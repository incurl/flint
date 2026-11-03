package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Errors returned by the permission package.
var (
	// ErrPermissionDenied is returned when the user refused a request.
	ErrPermissionDenied = errors.New("agent: permission denied")
	// ErrNoPermissionRequester is returned when a tool asks for a
	// permission but no requester was configured.
	ErrNoPermissionRequester = errors.New("agent: no permission requester")
)

// PermissionMode tells how the mutating tools are gated.
type PermissionMode int

const (
	// PermissionModeAsk asks the user before every mutating tool.
	//
	// It is the default: the agent writes to the data of the user, so
	// the user decides.
	PermissionModeAsk PermissionMode = iota
	// PermissionModeAllow approves every mutating tool.
	//
	// It is meant for an unattended run, where nobody can answer.
	PermissionModeAllow
)

// String implements [fmt.Stringer].
func (m PermissionMode) String() string {
	switch m {
	case PermissionModeAllow:
		return "allow"
	default:
		return "ask"
	}
}

// PermissionRequest asks the user to approve a tool call.
//
// The request is a value, so it can travel through the message channel
// of the UI without any shared state.
type PermissionRequest struct {
	// RunID identifies the run the request belongs to.
	RunID string
	// ToolName is the name of the tool that wants to run.
	ToolName string
	// Arguments holds the raw arguments of the call.
	Arguments string
	// Description explains what the call would do.
	Description string
}

// PermissionDecision is the answer of the user.
type PermissionDecision int

const (
	// PermissionAllow lets the tool run.
	PermissionAllow PermissionDecision = iota
	// PermissionDeny refuses the tool.
	PermissionDeny
)

// PermissionRequester asks the user to approve a tool call.
//
// The implementation blocks until the user answers, so it must be
// called from a command and never from the update loop.
//
// A requester that returns [PermissionDeny] makes the tool fail with
// [ErrPermissionDenied], which the model sees as the result of the
// call: it can then explain the refusal or try something else.
type PermissionRequester interface {
	// RequestPermission asks for the approval of a tool call.
	RequestPermission(ctx context.Context, request PermissionRequest) (PermissionDecision, error)
}

// PermissionRequesterFunc adapts a function to the
// [PermissionRequester] interface.
type PermissionRequesterFunc func(ctx context.Context, request PermissionRequest) (PermissionDecision, error)

// RequestPermission implements [PermissionRequester].
func (f PermissionRequesterFunc) RequestPermission(
	ctx context.Context,
	request PermissionRequest,
) (PermissionDecision, error) {
	return f(ctx, request)
}

// Gate decides whether a tool may run.
//
// The gate is consulted by the mutating tools only: a read never needs
// an approval, and asking for one on every read would make the agent
// unusable.
type Gate struct {
	// mu guards the mode and the requester.
	mu sync.RWMutex
	// mode tells how the calls are gated.
	mode PermissionMode
	// requester asks the user.
	requester PermissionRequester
}

// NewGate returns a gate in the given mode.
func NewGate(mode PermissionMode, requester PermissionRequester) *Gate {
	return &Gate{mode: mode, requester: requester}
}

// Mode returns the mode of the gate.
func (g *Gate) Mode() PermissionMode {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.mode
}

// SetMode changes the mode of the gate.
func (g *Gate) SetMode(mode PermissionMode) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.mode = mode
}

// Check asks for the approval of a tool call.
//
// In [PermissionModeAllow] it approves without asking. In
// [PermissionModeAsk] it defers to the requester, and a missing
// requester is an error rather than a silent approval: an agent that
// writes to the data of the user must not do it by default.
func (g *Gate) Check(ctx context.Context, request PermissionRequest) error {
	g.mu.RLock()
	mode, requester := g.mode, g.requester
	g.mu.RUnlock()

	if mode == PermissionModeAllow {
		return nil
	}

	if requester == nil {
		return fmt.Errorf("%w: %s", ErrNoPermissionRequester, request.ToolName)
	}

	decision, err := requester.RequestPermission(ctx, request)
	if err != nil {
		return err
	}

	if decision != PermissionAllow {
		return fmt.Errorf("%w: %s", ErrPermissionDenied, request.ToolName)
	}

	return nil
}

// GuardedTool is a tool that asks for the approval before it runs.
//
// It is the wrapper the mutating tools use: the read-only tools run
// without a gate, and the write ones do not.
type GuardedTool struct {
	// tool is the wrapped implementation.
	tool Tool
	// gate decides whether the tool may run.
	gate *Gate
	// description explains what the tool would do, for the request.
	description string
}

// NewGuardedTool wraps a tool with a permission gate.
//
// The description is what the user reads when they are asked to
// approve, so it must say what the call would do.
func NewGuardedTool(tool Tool, gate *Gate, description string) *GuardedTool {
	return &GuardedTool{tool: tool, gate: gate, description: description}
}

// Name implements [Tool].
func (g *GuardedTool) Name() string { return g.tool.Name() }

// Description implements [Tool].
func (g *GuardedTool) Description() string { return g.tool.Description() }

// Schema implements [Schemer].
func (g *GuardedTool) Schema() (map[string]any, []string) {
	schemer, ok := g.tool.(Schemer)
	if !ok {
		return nil, nil
	}

	return schemer.Schema()
}

// Run implements [Tool].
//
// The gate is consulted first: a refused call never reaches the tool,
// so no write happens.
func (g *GuardedTool) Run(ctx context.Context, input string) (string, error) {
	if g.gate == nil {
		return g.tool.Run(ctx, input)
	}

	request := PermissionRequest{
		ToolName:    g.tool.Name(),
		Arguments:   input,
		Description: g.description,
	}

	if err := g.gate.Check(ctx, request); err != nil {
		return "", err
	}

	return g.tool.Run(ctx, input)
}
