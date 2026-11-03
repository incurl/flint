package agent_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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

// recordingPublisher records the published requests.
type recordingPublisher struct {
	mu sync.Mutex
	// requests holds the published requests.
	requests []agent.PermissionRequest
}

// PublishPermission implements [agent.RequestPublisher].
func (p *recordingPublisher) PublishPermission(request agent.PermissionRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.requests = append(p.requests, request)
}

// TestDecisionBrokerCarriesTheAnswer verifies that the answer of the
// user reaches the run that asked for it.
//
// This is the bridge the whole permission flow depends on: the run
// blocks on the answer, and the answer comes from the UI.
func TestDecisionBrokerCarriesTheAnswer(t *testing.T) {
	publisher := &recordingPublisher{}
	broker := agent.NewDecisionBroker(publisher)

	done := make(chan agent.PermissionDecision, 1)

	go func() {
		decision, err := broker.RequestPermission(context.Background(), agent.PermissionRequest{
			ToolName:    "delete_record",
			Description: "Delete a record.",
		})
		if err != nil {
			done <- agent.PermissionDeny

			return
		}

		done <- decision
	}()

	// Wait for the request to be published.
	for {
		publisher.mu.Lock()
		published := len(publisher.requests)
		publisher.mu.Unlock()

		if published > 0 {
			break
		}
	}

	if !broker.Resolve(publisher.requests[0].ID, agent.PermissionAllow) {
		t.Fatal("Resolve() = false, want the pending request to be answered")
	}

	select {
	case decision := <-done:
		if decision != agent.PermissionAllow {
			t.Errorf("the decision is %v, want allow", decision)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the run did not receive the decision")
	}
}

// TestDecisionBrokerUnknownRequest verifies that answering a request
// that is not pending is reported as such.
func TestDecisionBrokerUnknownRequest(t *testing.T) {
	broker := agent.NewDecisionBroker(&recordingPublisher{})

	if broker.Resolve("nope", agent.PermissionAllow) {
		t.Error("Resolve() = true, want false for an unknown request")
	}
}

// TestDecisionBrokerWithoutPublisher verifies that a broker that cannot
// ask refuses rather than approving.
func TestDecisionBrokerWithoutPublisher(t *testing.T) {
	broker := agent.NewDecisionBroker(nil)

	_, err := broker.RequestPermission(context.Background(), agent.PermissionRequest{
		ToolName: "delete_record",
	})
	if !errors.Is(err, agent.ErrNoPermissionRequester) {
		t.Errorf("RequestPermission() error = %v, want ErrNoPermissionRequester", err)
	}
}

// TestDecisionBrokerCancelEndsTheWait verifies that a cancelled run does
// not leave the requester blocked forever.
func TestDecisionBrokerCancelEndsTheWait(t *testing.T) {
	broker := agent.NewDecisionBroker(&recordingPublisher{})

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		_, err := broker.RequestPermission(ctx, agent.PermissionRequest{ToolName: "delete_record"})
		done <- err
	}()

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("RequestPermission() error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the requester stayed blocked after the cancellation")
	}
}

// TestGatedToolRunsAfterTheApproval verifies the whole flow: a gated
// tool waits for the answer and only then writes.
//
// It is the property the permission gate exists for: the write happens
// because the user allowed it, not before.
func TestGatedToolRunsAfterTheApproval(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	publisher := &recordingPublisher{}
	broker := agent.NewDecisionBroker(publisher)

	tool := agent.NewGuardedTool(agent.NewCreateRecordTool(store), agent.NewGate(
		agent.PermissionModeAsk,
		broker,
	), "Create a record.")

	done := make(chan error, 1)

	go func() {
		_, err := tool.Run(context.Background(), `{"collection":"vocab","fields":{"key":"delta"}}`)
		done <- err
	}()

	// Wait for the request, then allow it.
	for {
		publisher.mu.Lock()
		published := len(publisher.requests)
		publisher.mu.Unlock()

		if published > 0 {
			break
		}
	}

	broker.Resolve(publisher.requests[0].ID, agent.PermissionAllow)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the tool did not run")
	}

	count, err := store.Count(context.Background(), "vocab", "")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if count != 4 {
		t.Errorf("the collection holds %d records, want 4: the approved write did not happen", count)
	}
}
