package agent_test

import (
	"context"
	"errors"
	"testing"

	"github.com/incurl/flint/internal/agent"
)

// TestGateAsksForApproval verifies that the gate consults the user.
func TestGateAsksForApproval(t *testing.T) {
	var asked []agent.PermissionRequest

	gate := agent.NewGate(agent.PermissionModeAsk, agent.PermissionRequesterFunc(
		func(_ context.Context, request agent.PermissionRequest) (agent.PermissionDecision, error) {
			asked = append(asked, request)

			return agent.PermissionAllow, nil
		},
	))

	err := gate.Check(context.Background(), agent.PermissionRequest{
		ToolName:    "delete_record",
		Arguments:   `{"id":"abc"}`,
		Description: "Delete a record.",
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if len(asked) != 1 {
		t.Fatalf("the user was asked %d times, want 1", len(asked))
	}

	if asked[0].ToolName != "delete_record" {
		t.Errorf("the request names %q, want delete_record", asked[0].ToolName)
	}

	if asked[0].Description == "" {
		t.Error("the request carries no description")
	}
}

// TestGateDenies verifies that a refusal is an error.
func TestGateDenies(t *testing.T) {
	gate := agent.NewGate(agent.PermissionModeAsk, agent.PermissionRequesterFunc(
		func(context.Context, agent.PermissionRequest) (agent.PermissionDecision, error) {
			return agent.PermissionDeny, nil
		},
	))

	err := gate.Check(context.Background(), agent.PermissionRequest{ToolName: "delete_record"})
	if !errors.Is(err, agent.ErrPermissionDenied) {
		t.Errorf("Check() error = %v, want ErrPermissionDenied", err)
	}
}

// TestGateWithoutRequesterFails verifies that a gate that cannot ask
// refuses rather than approving.
//
// An agent that writes to the data of the user must not do it by
// default: the absence of a requester is an error, not a silent yes.
func TestGateWithoutRequesterFails(t *testing.T) {
	gate := agent.NewGate(agent.PermissionModeAsk, nil)

	err := gate.Check(context.Background(), agent.PermissionRequest{ToolName: "delete_record"})
	if !errors.Is(err, agent.ErrNoPermissionRequester) {
		t.Errorf("Check() error = %v, want ErrNoPermissionRequester", err)
	}
}

// TestGateAllowModeSkipsTheUser verifies that the allow mode approves
// without asking.
func TestGateAllowModeSkipsTheUser(t *testing.T) {
	asked := false

	gate := agent.NewGate(agent.PermissionModeAllow, agent.PermissionRequesterFunc(
		func(context.Context, agent.PermissionRequest) (agent.PermissionDecision, error) {
			asked = true

			return agent.PermissionDeny, nil
		},
	))

	if err := gate.Check(context.Background(), agent.PermissionRequest{ToolName: "delete_record"}); err != nil {
		t.Fatalf("Check() error = %v", err)
	}

	if asked {
		t.Error("the user was asked in the allow mode")
	}
}

// TestGateModeCanChange verifies that the mode can be changed at
// runtime.
func TestGateModeCanChange(t *testing.T) {
	gate := agent.NewGate(agent.PermissionModeAsk, nil)

	if gate.Mode() != agent.PermissionModeAsk {
		t.Errorf("Mode() = %v, want ask", gate.Mode())
	}

	gate.SetMode(agent.PermissionModeAllow)

	if gate.Mode() != agent.PermissionModeAllow {
		t.Errorf("Mode() = %v, want allow", gate.Mode())
	}
}

// TestGuardedToolRefusesToWrite verifies that a refused call never
// reaches the store.
//
// This is the property that matters: the gate is not a warning, it
// stops the write.
func TestGuardedToolRefusesToWrite(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	gate := agent.NewGate(agent.PermissionModeAsk, agent.PermissionRequesterFunc(
		func(context.Context, agent.PermissionRequest) (agent.PermissionDecision, error) {
			return agent.PermissionDeny, nil
		},
	))

	tool := agent.NewGuardedTool(agent.NewCreateRecordTool(store), gate, "Create a record.")

	_, err := tool.Run(context.Background(), `{"collection":"vocab","fields":{"key":"delta"}}`)
	if !errors.Is(err, agent.ErrPermissionDenied) {
		t.Fatalf("Run() error = %v, want ErrPermissionDenied", err)
	}

	count, err := store.Count(context.Background(), "vocab", "")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if count != 3 {
		t.Errorf("the collection holds %d records, want 3: the refused write happened", count)
	}
}

// TestGuardedToolWritesWhenAllowed verifies that an approved call runs.
func TestGuardedToolWritesWhenAllowed(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	gate := agent.NewGate(agent.PermissionModeAsk, agent.PermissionRequesterFunc(
		func(context.Context, agent.PermissionRequest) (agent.PermissionDecision, error) {
			return agent.PermissionAllow, nil
		},
	))

	tool := agent.NewGuardedTool(agent.NewCreateRecordTool(store), gate, "Create a record.")

	if _, err := tool.Run(context.Background(),
		`{"collection":"vocab","fields":{"key":"delta"}}`); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	count, err := store.Count(context.Background(), "vocab", "")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if count != 4 {
		t.Errorf("the collection holds %d records, want 4", count)
	}
}

// TestGuardedToolKeepsTheSchema verifies that the wrapper still tells
// the model what the tool expects.
func TestGuardedToolKeepsTheSchema(t *testing.T) {
	store := newEmbeddedStore(t)

	tool := agent.NewGuardedTool(agent.NewListRecordsTool(store), nil, "List the records.")

	info := agent.NewWrappedTool(tool).Info()

	if len(info.Parameters) == 0 {
		t.Fatal("the guarded tool exposes no parameters")
	}

	if len(info.Required) != 1 || info.Required[0] != "collection" {
		t.Errorf("Required = %v, want [collection]", info.Required)
	}
}
