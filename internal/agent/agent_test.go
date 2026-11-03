package agent_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"charm.land/fantasy"

	"github.com/incurl/flint/internal/agent"
)

// fakeModel is a scripted language model.
//
// The script is a list of turns: each turn is the stream of parts the
// model produces for one model call. The agent calls the model again
// after a tool call, so a script of two turns exercises the loop.
type fakeModel struct {
	mu sync.Mutex
	// turns holds the scripted streams, one per model call.
	turns [][]fantasy.StreamPart
	// calls counts the model calls.
	calls int
}

func (m *fakeModel) Provider() string { return "fake" }

func (m *fakeModel) Model() string { return "fake-model" }

func (m *fakeModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("agent_test: Generate is not used")
}

func (m *fakeModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("agent_test: GenerateObject is not used")
}

func (m *fakeModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("agent_test: StreamObject is not used")
}

// Stream returns the next scripted turn.
//
// A script that runs out returns a single stop turn, so a model that
// asks for a tool twice does not panic the test.
func (m *fakeModel) Stream(context.Context, fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// A script that runs out returns a single stop turn, so a model
	// that asks for a tool twice does not panic the test.
	turn := []fantasy.StreamPart{{
		Type:         fantasy.StreamPartTypeFinish,
		FinishReason: fantasy.FinishReasonStop,
	}}

	if m.calls < len(m.turns) {
		turn = m.turns[m.calls]
	}

	m.calls++

	return func(yield func(fantasy.StreamPart) bool) {
		for _, part := range turn {
			if !yield(part) {
				return
			}
		}
	}, nil
}

// textTurn returns a scripted turn that streams text and stops.
func textTurn(text string) []fantasy.StreamPart {
	return []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeTextStart, ID: "t1"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "t1", Delta: text},
		{Type: fantasy.StreamPartTypeTextEnd, ID: "t1"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop},
	}
}

// toolTurn returns a scripted turn that calls a tool.
func toolTurn(name, input string) []fantasy.StreamPart {
	return []fantasy.StreamPart{
		{
			Type:          fantasy.StreamPartTypeToolCall,
			ID:            "call-1",
			ToolCallName:  name,
			ToolCallInput: input,
		},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls},
	}
}

// recordingTool is a tool that records its calls.
type recordingTool struct {
	// name is the tool name.
	name string
	// out is the text the tool returns.
	out string
	// err is the error the tool returns.
	err error
	// mu guards the calls.
	mu sync.Mutex
	// calls holds the inputs the tool received.
	calls []string
}

func (t *recordingTool) Name() string { return t.name }

func (t *recordingTool) Description() string { return "a test tool" }

func (t *recordingTool) Run(_ context.Context, input string) (string, error) {
	t.mu.Lock()
	t.calls = append(t.calls, input)
	t.mu.Unlock()

	if t.err != nil {
		return "", t.err
	}

	return t.out, nil
}

// collector records the published events.
type collector struct {
	mu sync.Mutex
	// events holds the published events.
	events []agent.Event
}

func (c *collector) Publish(event agent.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, event)
}

// count returns how many events of a kind were published.
func (c *collector) count(kind agent.EventKind) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	n := 0

	for _, event := range c.events {
		if event.Kind == kind {
			n++
		}
	}

	return n
}

// TestRunnerStreamsText verifies that a run returns the text of the
// assistant and publishes the events the UI needs.
func TestRunnerStreamsText(t *testing.T) {
	model := &fakeModel{turns: [][]fantasy.StreamPart{textTurn("hello there")}}
	events := &collector{}

	runner := agent.NewRunner(agent.Options{
		Model:     model,
		Publisher: events,
	})

	text, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "hi"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(text, "hello there") {
		t.Errorf("Run() = %q, want it to contain the streamed text", text)
	}

	if events.count(agent.EventStarted) != 1 {
		t.Error("the run did not publish a start event")
	}

	if events.count(agent.EventTextDelta) == 0 {
		t.Error("the run did not publish the text deltas")
	}

	if events.count(agent.EventFinished) != 1 {
		t.Error("the run did not publish exactly one finish event")
	}
}

// TestRunnerCallsTool verifies that the model can call a tool and that
// the answer reaches the model.
func TestRunnerCallsTool(t *testing.T) {
	tool := &recordingTool{name: "count_records", out: "42"}

	model := &fakeModel{turns: [][]fantasy.StreamPart{
		toolTurn("count_records", `{"collection":"vocab"}`),
		textTurn("there are 42 records"),
	}}

	events := &collector{}

	registry := agent.NewRegistry()
	registry.Register(tool)

	runner := agent.NewRunner(agent.Options{
		Model:     model,
		Registry:  registry,
		Publisher: events,
	})

	text, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "how many?"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(text, "42 records") {
		t.Errorf("Run() = %q, want the answer that followed the tool call", text)
	}

	if len(tool.calls) != 1 {
		t.Fatalf("the tool was called %d times, want 1", len(tool.calls))
	}

	if tool.calls[0] != `{"collection":"vocab"}` {
		t.Errorf("the tool received %q, want the raw arguments", tool.calls[0])
	}

	if events.count(agent.EventToolCall) != 1 {
		t.Error("the run did not publish the tool call event")
	}
}

// TestRunnerToolFailureIsAResult verifies that a failing tool does not
// end the run: the model is told about the failure and continues.
func TestRunnerToolFailureIsAResult(t *testing.T) {
	tool := &recordingTool{name: "count_records", err: errors.New("the store is down")}

	model := &fakeModel{turns: [][]fantasy.StreamPart{
		toolTurn("count_records", `{}`),
		textTurn("I could not count them"),
	}}

	registry := agent.NewRegistry()
	registry.Register(tool)

	runner := agent.NewRunner(agent.Options{
		Model:    model,
		Registry: registry,
	})

	text, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "how many?"})
	if err != nil {
		t.Fatalf("Run() error = %v, want the run to survive a tool failure", err)
	}

	if !strings.Contains(text, "could not count") {
		t.Errorf("Run() = %q, want the answer after the failure", text)
	}
}

// TestRunnerRequiresModel verifies that a run without a model is an
// error rather than an empty turn.
func TestRunnerRequiresModel(t *testing.T) {
	runner := agent.NewRunner(agent.Options{})

	_, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "hi"})
	if !errors.Is(err, agent.ErrNoModel) {
		t.Errorf("Run() error = %v, want ErrNoModel", err)
	}
}

// TestRunnerCancel verifies that a running turn can be stopped.
func TestRunnerCancel(t *testing.T) {
	runner := agent.NewRunner(agent.Options{Model: &blockingModel{}})

	done := make(chan error, 1)

	go func() {
		_, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "hi"})
		done <- err
	}()

	// Wait for the run to register itself.
	for !runner.Busy() {
	}

	if !runner.Cancel("r1") {
		t.Fatal("Cancel() = false, want the run to be cancelled")
	}

	err := <-done
	if !errors.Is(err, agent.ErrRunCancelled) {
		t.Errorf("Run() error = %v, want ErrRunCancelled", err)
	}
}

// TestRunnerCancelUnknownRun verifies that cancelling a run that is not
// active is reported as such.
func TestRunnerCancelUnknownRun(t *testing.T) {
	runner := agent.NewRunner(agent.Options{})

	if runner.Cancel("nope") {
		t.Error("Cancel() = true, want false for an unknown run")
	}
}

// blockingModel blocks until its context is canceled.
type blockingModel struct{}

func (m *blockingModel) Provider() string { return "fake" }

func (m *blockingModel) Model() string { return "blocking" }

func (m *blockingModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("not used")
}

func (m *blockingModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not used")
}

func (m *blockingModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not used")
}

func (m *blockingModel) Stream(ctx context.Context, _ fantasy.Call) (fantasy.StreamResponse, error) {
	return func(yield func(fantasy.StreamPart) bool) {
		<-ctx.Done()
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeError, Error: ctx.Err()})
	}, nil
}

// TestRegistryOrder verifies that the registry keeps the registration
// order and that a duplicate name replaces the tool.
func TestRegistryOrder(t *testing.T) {
	registry := agent.NewRegistry()

	registry.Register(&recordingTool{name: "a"})
	registry.Register(&recordingTool{name: "b"})

	if got := registry.Names(); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("Names() = %v, want [a b]", got)
	}

	// A duplicate replaces the tool without changing the order.
	registry.Register(&recordingTool{name: "a"})

	if got := registry.Names(); len(got) != 2 {
		t.Errorf("Names() = %v, want the order to stay the same", got)
	}
}

// TestRegistryGetUnknown verifies that an unknown tool is reported.
func TestRegistryGetUnknown(t *testing.T) {
	registry := agent.NewRegistry()

	_, err := registry.Get("nope")
	if !errors.Is(err, agent.ErrToolNotFound) {
		t.Errorf("Get() error = %v, want ErrToolNotFound", err)
	}
}

// TestAgentToolReportsFailure verifies that a failing tool becomes an
// error response, so the model can correct itself.
func TestAgentToolReportsFailure(t *testing.T) {
	tool := &recordingTool{name: "boom", err: errors.New("the store is down")}

	resp, err := agent.NewWrappedTool(tool).Run(context.Background(), fantasy.ToolCall{Input: "{}"})
	if err != nil {
		t.Fatalf("Run() error = %v, want the failure to be a response", err)
	}

	if !resp.IsError {
		t.Error("the response is not an error response")
	}

	if !strings.Contains(resp.Content, "the store is down") {
		t.Errorf("Content = %q, want the failure text", resp.Content)
	}
}

// TestAgentToolInfo verifies that the tool metadata reaches the model.
func TestAgentToolInfo(t *testing.T) {
	info := agent.NewWrappedTool(&recordingTool{name: "count_records"}).Info()

	if info.Name != "count_records" {
		t.Errorf("Info().Name = %q, want count_records", info.Name)
	}

	if info.Description == "" {
		t.Error("Info().Description is empty")
	}
}

// TestRunnerIteratesAfterToolCall verifies that the loop calls the
// model again after a tool returned, which is what makes the agent
// agentic rather than a single call.
//
// The scripted model produces a tool call and then an answer, so the
// second model call can only happen if the tool result was fed back.
func TestRunnerIteratesAfterToolCall(t *testing.T) {
	model := &fakeModel{turns: [][]fantasy.StreamPart{
		toolTurn("count_records", `{}`),
		textTurn("done"),
	}}

	registry := agent.NewRegistry()
	registry.Register(&recordingTool{name: "count_records", out: "7"})

	runner := agent.NewRunner(agent.Options{Model: model, Registry: registry})

	if _, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "count"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	model.mu.Lock()
	calls := model.calls
	model.mu.Unlock()

	if calls != 2 {
		t.Errorf("the model was called %d times, want 2: once for the tool call and once for the answer", calls)
	}
}
