// Package agent provides the agentic layer of the template: a tool
// registry, a run lifecycle and the bridge between a language model and
// the PocketBase data layer.
//
// The package is deliberately free of any UI dependency. The agent
// knows how to run a turn and what the tools do; it does not know how
// they are drawn. The page layer subscribes to the events it publishes,
// which keeps the same separation the rest of the template follows.
//
// The model itself comes from [charm.land/fantasy], so the loop — the
// model call, the tool dispatch and the iteration between them — is the
// library's. What this package adds is the part that is specific to
// Flint: the tools that speak to PocketBase, the permission gate in
// front of the mutating ones, and the run bookkeeping the UI needs.
package agent

import (
	"context"
	"errors"
	"fmt"

	"charm.land/fantasy"
)

// Errors returned by the package.
var (
	// ErrToolNotFound is returned when the model calls a tool that is
	// not registered.
	ErrToolNotFound = errors.New("agent: tool not found")
	// ErrNoModel is returned when a run is attempted without a model.
	ErrNoModel = errors.New("agent: no model configured")
	// ErrInvalidToolInput is returned when the arguments of a tool call
	// do not match the schema of the tool.
	ErrInvalidToolInput = errors.New("agent: invalid tool input")
)

// Tool is a capability the model can invoke.
//
// A tool is described to the model by its name and its description, and
// it receives the arguments as a JSON object. The implementation is
// responsible for the validation of the arguments: the schema is
// generated from the parameter type, but the values still have to be
// checked before they reach the store.
//
// Implementations must be safe for concurrent use: the model library
// may run the tools of a single step in parallel.
type Tool interface {
	// Name returns the identifier of the tool.
	//
	// It is the name the model uses to call it, so it must be stable
	// and unique in a registry.
	Name() string
	// Description returns the prose the model reads to decide when to
	// call the tool.
	//
	// It is the single most important part of the tool: a vague
	// description produces a model that calls the wrong tool.
	Description() string
	// Run executes the tool.
	//
	// It must not panic on a malformed input, and it must return the
	// error rather than the panic, so that a failing tool does not
	// take the whole run down.
	Run(ctx context.Context, input string) (string, error)
}

// Schemer is a tool that declares the schema of its arguments.
//
// The schema is what the model reads to call the tool, so a tool that
// implements it gets a better description of what it expects. The
// schema is generated from the parameter struct of the tool, which
// carries the description of every argument in its tags.
type Schemer interface {
	// Schema returns the properties of the arguments and the names of
	// the mandatory ones.
	Schema() (map[string]any, []string)
}

// WrappedTool adapts a [Tool] to the model library's tool interface.
//
// The library owns the JSON schema of the tool and the decoding of the
// arguments, so the adapter is a thin translation between the two
// worlds: the library calls [Tool.Run] with the raw JSON, and the
// result is returned as a text response.
type WrappedTool struct {
	// tool is the wrapped implementation.
	tool Tool
	// parameters is the schema of the arguments.
	parameters map[string]any
	// required holds the names of the mandatory arguments.
	required []string
}

// NewWrappedTool wraps a tool for the model library.
//
// The schema of the arguments is taken from the tool when it declares
// one, so the model receives the description of every argument.
func NewWrappedTool(tool Tool) *WrappedTool {
	wrapped := &WrappedTool{tool: tool}

	if schemer, ok := tool.(Schemer); ok {
		wrapped.parameters, wrapped.required = schemer.Schema()
	}

	return wrapped
}

// Info implements [fantasy.AgentTool].
func (a *WrappedTool) Info() fantasy.ToolInfo {
	return fantasy.ToolInfo{
		Name:        a.tool.Name(),
		Description: a.tool.Description(),
		Parameters:  a.parameters,
		Required:    a.required,
	}
}

// Run implements [fantasy.AgentTool].
//
// The arguments are handed to the tool as the raw JSON the model
// produced. A tool that rejects its input returns an error response
// rather than an error, so that the model sees the failure and can
// correct itself instead of the run ending.
func (a *WrappedTool) Run(ctx context.Context, params fantasy.ToolCall) (fantasy.ToolResponse, error) {
	var input string

	if params.Input != "" {
		input = params.Input
	}

	out, err := a.tool.Run(ctx, input)
	if err != nil {
		// A tool failure is a result, not a crash: the model is told
		// about it and gets a chance to try something else.
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}

	return fantasy.NewTextResponse(out), nil
}

// ProviderOptions implements [fantasy.AgentTool].
func (a *WrappedTool) ProviderOptions() fantasy.ProviderOptions {
	return fantasy.ProviderOptions{}
}

// SetProviderOptions implements [fantasy.AgentTool].
func (a *WrappedTool) SetProviderOptions(opts fantasy.ProviderOptions) {}

// Registry holds the tools the model may call.
//
// The registry is the single place where the capabilities of the agent
// are declared, so the tool list that reaches the model and the list
// the permission gate checks cannot drift apart.
type Registry struct {
	// tools holds the registered tools, keyed by name.
	tools map[string]Tool
	// order holds the tool names in registration order.
	//
	// The order is stable so that the tool list the model receives does
	// not change between runs, which keeps the prompt cache usable.
	order []string
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register adds a tool to the registry.
//
// A tool whose name is already registered replaces the previous one, so
// that a downstream project can override a tool of the template.
func (r *Registry) Register(tool Tool) {
	if tool == nil {
		return
	}

	if _, exists := r.tools[tool.Name()]; !exists {
		r.order = append(r.order, tool.Name())
	}

	r.tools[tool.Name()] = tool
}

// Tools returns the registered tools, in registration order.
func (r *Registry) Tools() []Tool {
	out := make([]Tool, 0, len(r.order))

	for _, name := range r.order {
		out = append(out, r.tools[name])
	}

	return out
}

// Get returns the tool with the given name.
func (r *Registry) Get(name string) (Tool, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}

	return tool, nil
}

// Names returns the names of the registered tools, in registration
// order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)

	return out
}

// AgentTools returns the registered tools wrapped for the model
// library.
func (r *Registry) AgentTools() []fantasy.AgentTool {
	tools := r.Tools()
	out := make([]fantasy.AgentTool, 0, len(tools))

	for _, tool := range tools {
		out = append(out, NewWrappedTool(tool))
	}

	return out
}

// AgentToolsFor returns the tools of a mode, wrapped for the model
// library.
//
// In [ModePlan] the tools that change the data are left out: the model
// does not see them, so it cannot call them. The restriction is
// structural, not a request in the prompt.
//
// A tool that the template does not know is treated as mutating, so a
// downstream project that adds a tool is safe by default: it is
// withheld from the planning mode unless it says otherwise.
func (r *Registry) AgentToolsFor(mode Mode) []fantasy.AgentTool {
	tools := r.Tools()
	out := make([]fantasy.AgentTool, 0, len(tools))

	for _, tool := range tools {
		if mode.IsPlan() && r.withheldInPlan(tool.Name()) {
			continue
		}

		out = append(out, NewWrappedTool(tool))
	}

	return out
}

// withheldInPlan reports whether a tool is withheld from the planning
// mode.
//
// The tools the template knows about are classified explicitly. An
// unknown tool is withheld: a planning mode that let an unknown tool
// through would be a planning mode that could change the data.
func (r *Registry) withheldInPlan(name string) bool {
	for _, tool := range readOnlyTools {
		if tool == name {
			return false
		}
	}

	return true
}

// ToolNamesFor returns the names of the tools of a mode.
//
// It is what the planning prompt advertises: the prompt lists the
// tools the run actually has, so it can never promise a tool the model
// cannot call.
func (r *Registry) ToolNamesFor(mode Mode) []string {
	names := r.Names()
	out := make([]string, 0, len(names))

	for _, name := range names {
		if mode.IsPlan() && r.withheldInPlan(name) {
			continue
		}

		out = append(out, name)
	}

	return out
}
