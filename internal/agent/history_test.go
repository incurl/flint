package agent_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"charm.land/fantasy"
	"github.com/pocketbase/pocketbase"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/session"
)

// newStore creates an embedded store with the session collections.
func newStore(t *testing.T) *pbstore.Embedded {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "pb_data")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	if err := session.EnsureCollections(context.Background(), store); err != nil {
		t.Fatalf("EnsureCollections() error = %v", err)
	}

	return store
}

// TestHistoryRoundTrip verifies that a conversation survives the trip
// through the store.
//
// This is the whole point of the persistence: the messages are written
// by one run and read back by the next one.
func TestHistoryRoundTrip(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	history := agent.NewHistory(service, created.ID)
	if history == nil {
		t.Fatal("NewHistory() = nil, want a history")
	}

	history.AppendUser(ctx, 1, "how many records?")
	history.AppendTool(ctx, 1, "count_records", `{"collection":"vocab"}`, "42 records")
	history.AppendAssistant(ctx, 1, "there are 42 records")

	messages := history.Load(ctx)
	if len(messages) != 3 {
		t.Fatalf("the history holds %d messages, want 3", len(messages))
	}

	// The prompt is a user message.
	if messages[0].Role != "user" {
		t.Errorf("the first message has the role %q, want user", messages[0].Role)
	}

	// The tool message carries both the call and its result.
	if messages[1].Role != "tool" {
		t.Errorf("the second message has the role %q, want tool", messages[1].Role)
	}

	if len(messages[1].Content) != 2 {
		t.Fatalf("the tool message has %d parts, want 2", len(messages[1].Content))
	}

	// The answer is an assistant message.
	if messages[2].Role != "assistant" {
		t.Errorf("the last message has the role %q, want assistant", messages[2].Role)
	}
}

// TestHistoryWithoutService verifies that a history that has nowhere to
// write is inert.
func TestHistoryWithoutService(t *testing.T) {
	if agent.NewHistory(nil, "session") != nil {
		t.Error("NewHistory() is not nil without a service")
	}
}

// TestHistoryWithoutSession verifies that a history without a session is
// a template.
//
// The page creates the conversation on its first turn, so the runner
// needs a history it can point at the conversation later.
func TestHistoryWithoutSession(t *testing.T) {
	history := agent.NewHistory(session.NewService(nil), "")
	if history == nil {
		t.Fatal("NewHistory() = nil, want a template")
	}

	if history.SessionID() != "" {
		t.Errorf("SessionID() = %q, want an empty id", history.SessionID())
	}
}

// TestHistoryLoadWithoutStore verifies that a history that cannot be
// read returns nothing rather than failing.
//
// A run must start even when the conversation cannot be loaded: the
// conversation is a convenience, not a precondition.
func TestHistoryLoadWithoutStore(t *testing.T) {
	history := agent.NewHistory(session.NewService(nil), "session")
	if history == nil {
		t.Fatal("NewHistory() = nil, want a history")
	}

	if messages := history.Load(context.Background()); len(messages) != 0 {
		t.Errorf("Load() returned %d messages, want 0", len(messages))
	}
}

// TestRunnerPersistsTheConversation verifies that a run writes its turn
// to the conversation.
//
// Without this, the conversation would be written by the page alone and
// the history of the model would not match what the user sees.
func TestRunnerPersistsTheConversation(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	model := &fakeModel{turns: [][]fantasy.StreamPart{textTurn("there are 3 records")}}

	runner := agent.NewRunner(agent.Options{
		Model:   model,
		History: agent.NewHistory(service, created.ID),
	})

	if _, err := runner.Run(ctx, agent.RunRequest{RunID: "r1", Prompt: "how many?", SessionID: created.ID}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	messages, err := service.Messages(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	if len(messages) != 2 {
		t.Fatalf("the conversation holds %d messages, want 2", len(messages))
	}

	if messages[0].Role != session.RoleUser {
		t.Errorf("the first message has the role %q, want user", messages[0].Role)
	}

	if messages[0].Text != "how many?" {
		t.Errorf("the first message is %q, want the prompt", messages[0].Text)
	}

	if messages[1].Role != session.RoleAssistant {
		t.Errorf("the second message has the role %q, want assistant", messages[1].Role)
	}

	if !strings.Contains(messages[1].Text, "there are 3 records") {
		t.Errorf("the second message is %q, want the answer", messages[1].Text)
	}
}

// TestRunnerContinuesTheConversation verifies that the second turn of a
// session sees the first one.
//
// The model has to receive the history, otherwise the conversation is
// stored but never used.
func TestRunnerContinuesTheConversation(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	model := &recordingModel{}
	history := agent.NewHistory(service, created.ID)

	runner := agent.NewRunner(agent.Options{Model: model, History: history})

	if _, err := runner.Run(ctx, agent.RunRequest{RunID: "r1", Prompt: "first", SessionID: created.ID}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if _, err := runner.Run(ctx, agent.RunRequest{RunID: "r2", Prompt: "second", SessionID: created.ID}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// The second call carries the first turn.
	model.mu.Lock()
	defer model.mu.Unlock()

	if len(model.calls) != 2 {
		t.Fatalf("the model was called %d times, want 2", len(model.calls))
	}

	found := false

	for _, message := range model.calls[1] {
		for _, part := range message.Content {
			if text, ok := part.(fantasy.TextPart); ok && strings.Contains(text.Text, "first") {
				found = true
			}
		}
	}

	if !found {
		t.Error("the second call does not carry the first turn of the conversation")
	}
}

// recordingModel records the messages of every call.
type recordingModel struct {
	mu sync.Mutex
	// calls holds the prompt of every call.
	calls [][]fantasy.Message
}

func (m *recordingModel) Provider() string { return "fake" }

func (m *recordingModel) Model() string { return "fake" }

func (m *recordingModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("not used")
}

func (m *recordingModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not used")
}

func (m *recordingModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not used")
}

func (m *recordingModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	m.calls = append(m.calls, call.Prompt)
	m.mu.Unlock()

	return func(yield func(fantasy.StreamPart) bool) {
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextStart, ID: "t1"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextDelta, ID: "t1", Delta: "ok"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeTextEnd, ID: "t1"})
		yield(fantasy.StreamPart{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop})
	}, nil
}
