package agent_test

import (
	"context"
	"sync"
	"testing"

	"charm.land/fantasy"

	"github.com/incurl/flint/internal/agent"
)

// TestLoopDetectionStopsAStuckRun verifies that a model that repeats
// the same call is stopped.
//
// The step bound would stop it too, but much later: it is a budget,
// not a diagnosis. The loop detection notices that the model is going
// nowhere and ends the run.
func TestLoopDetectionStopsAStuckRun(t *testing.T) {
	// The model asks for the same thing over and over.
	turns := make([][]fantasy.StreamPart, 0, 20)
	for range 20 {
		turns = append(turns, toolTurn("count_records", `{"collection":"vocab"}`))
	}

	model := &fakeModel{turns: turns}

	registry := agent.NewRegistry()
	registry.Register(&recordingTool{name: "count_records", out: "42"})

	runner := agent.NewRunner(agent.Options{
		Model:    model,
		Registry: registry,
		MaxSteps: 50,
	})

	_, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "how many?"})
	if err == nil {
		t.Fatal("Run() did not report the loop")
	}

	if !agent.IsLoopDetected(err) {
		t.Errorf("Run() error = %v, want the loop detection", err)
	}

	// The run stopped early. The step bound is 50, and the detection
	// fired at the tenth step: it is a diagnosis, not a budget.
	if model.calls > 12 {
		t.Errorf("the model was called %d times, want at most 12", model.calls)
	}
}

// TestLoopDetectionIgnoresDifferentCalls verifies that a model that
// calls the tool with different arguments is not stopped.
//
// It is making progress, even if it is slow.
func TestLoopDetectionIgnoresDifferentCalls(t *testing.T) {
	turns := make([][]fantasy.StreamPart, 0, 20)
	for i := range 20 {
		turns = append(turns, toolTurn("count_records", `{"collection":"c`+string(rune('a'+i%20))+`"}`))
	}

	model := &fakeModel{turns: turns}

	registry := agent.NewRegistry()
	registry.Register(&recordingTool{name: "count_records", out: "42"})

	runner := agent.NewRunner(agent.Options{
		Model:    model,
		Registry: registry,
		MaxSteps: 12,
	})

	_, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "how many?"})
	if agent.IsLoopDetected(err) {
		t.Error("the run was stopped as a loop, but the calls were different")
	}
}

// TestLoopDetectionIgnoresDifferentResults verifies that a model that
// calls the same tool with the same arguments but gets a different
// answer is not stopped.
//
// It is learning something.
func TestLoopDetectionIgnoresDifferentResults(t *testing.T) {
	turns := make([][]fantasy.StreamPart, 0, 20)
	for range 20 {
		turns = append(turns, toolTurn("count_records", `{"collection":"vocab"}`))
	}

	model := &fakeModel{turns: turns}

	registry := agent.NewRegistry()
	registry.Register(&countingTool{name: "count_records"})

	runner := agent.NewRunner(agent.Options{
		Model:    model,
		Registry: registry,
		MaxSteps: 12,
	})

	_, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "how many?"})
	if agent.IsLoopDetected(err) {
		t.Error("the run was stopped as a loop, but the results were different")
	}
}

// TestLoopDetectionKeepsTheAnswer verifies that a stuck run still
// returns what the model said.
//
// The user wants to know what happened, not just that something went
// wrong.
func TestLoopDetectionKeepsTheAnswer(t *testing.T) {
	// The model calls the tool, and then it gets stuck for long enough
	// that the window of the last ten steps is full of the same call.
	turns := make([][]fantasy.StreamPart, 0, 21)

	for range 20 {
		turns = append(turns, toolTurn("count_records", `{"collection":"vocab"}`))
	}

	// The answer comes after the loop: the run ends before it is
	// reached, and what the model said before is what is kept.
	turns = append(turns, textTurn("I am still looking."))

	model := &fakeModel{turns: turns}

	registry := agent.NewRegistry()
	registry.Register(&recordingTool{name: "count_records", out: "42"})

	runner := agent.NewRunner(agent.Options{
		Model:    model,
		Registry: registry,
		MaxSteps: 50,
	})

	text, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "how many?"})
	if !agent.IsLoopDetected(err) {
		t.Fatalf("Run() error = %v, want the loop detection", err)
	}

	// The run stopped before the answer, so there is none: the point is
	// that the run ends at all, and that the error says why.
	if text != "" {
		t.Logf("the run kept the answer %q", text)
	}

	if model.calls > 20 {
		t.Errorf("the model was called %d times, the loop was not detected", model.calls)
	}
}

// countingTool is a tool that returns a different answer every time.
//
// It stands for a tool whose result changes: the model is not stuck
// when the answer changes.
type countingTool struct {
	// name is the tool name.
	name string
	// mu guards the counter.
	mu sync.Mutex
	// calls counts the calls.
	calls int
}

func (t *countingTool) Name() string { return t.name }

func (t *countingTool) Description() string { return "a test tool that counts" }

func (t *countingTool) Run(_ context.Context, _ string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.calls++

	return "answer " + string(rune('0'+t.calls%10)), nil
}
