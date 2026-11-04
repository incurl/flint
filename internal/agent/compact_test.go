package agent_test

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/session"
)

// TestCompactReplacesTheOldTurns verifies that a compacted conversation
// holds the summary instead of the turns it summarises.
//
// This is the whole point: a model that sees a thousand messages of
// history does not have room for the answer.
func TestCompactReplacesTheOldTurns(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// A conversation that is too long: many turns, and a lot of text.
	for i := range 20 {
		_, err := service.AppendMessage(ctx, created.ID, session.Message{
			Role: session.RoleUser,
			Turn: i + 1,
			Text: strings.Repeat("a long question that fills the context window. ", 100),
		})
		if err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	model := &fakeModel{turns: [][]fantasy.StreamPart{textTurn("The summary of the conversation.")}}

	runner := agent.NewRunner(agent.Options{
		Model:    model,
		Registry: agent.NewRegistry(),
		History:  agent.NewHistory(service, ""),
	})

	if err := runner.Compact(ctx, created.ID); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	// The summary is stored.
	messages, err := service.Messages(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	summaries := 0

	for _, message := range messages {
		if message.Role == session.RoleSummary {
			summaries++

			if !strings.Contains(message.Text, "The summary of the conversation.") {
				t.Errorf("the summary does not hold the text: %q", message.Text)
			}
		}
	}

	if summaries != 1 {
		t.Fatalf("the conversation holds %d summaries, want 1", summaries)
	}

	// And the model sees the summary, not the twenty turns.
	history := agent.NewHistory(service, created.ID)
	loaded := history.Load(ctx)

	if len(loaded) == 0 {
		t.Fatal("the history is empty")
	}

	text := messageText(loaded[0])

	if !strings.Contains(text, "The summary of the conversation.") {
		t.Errorf("the first message is not the summary: %q", text)
	}
}

// TestCompactKeepsTheLastTurns verifies that the turns that came after
// the summary are kept.
//
// The last turns are the ones the conversation is about, and the model
// needs them verbatim.
func TestCompactKeepsTheLastTurns(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	for i := range 20 {
		_, err := service.AppendMessage(ctx, created.ID, session.Message{
			Role: session.RoleUser,
			Turn: i + 1,
			Text: strings.Repeat("filler ", 1000),
		})
		if err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	// The last turn is the one that matters.
	_, err = service.AppendMessage(ctx, created.ID, session.Message{
		Role: session.RoleUser,
		Turn: 21,
		Text: "the last question",
	})
	if err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	model := &fakeModel{turns: [][]fantasy.StreamPart{textTurn("the summary")}}

	runner := agent.NewRunner(agent.Options{
		Model:    model,
		Registry: agent.NewRegistry(),
		History:  agent.NewHistory(service, ""),
	})

	if err := runner.Compact(ctx, created.ID); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	history := agent.NewHistory(service, created.ID)
	loaded := history.Load(ctx)

	found := false

	for _, message := range loaded {
		if strings.Contains(messageText(message), "the last question") {
			found = true
		}
	}

	if !found {
		t.Error("the last turn was lost")
	}
}

// TestCompactLeavesAShortConversationAlone verifies that a
// conversation that fits is not summarised.
//
// Summarising every conversation would cost a call for nothing.
func TestCompactLeavesAShortConversationAlone(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	for i := range 3 {
		_, err := service.AppendMessage(ctx, created.ID, session.Message{
			Role: session.RoleUser,
			Turn: i + 1,
			Text: "a short question",
		})
		if err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	model := &fakeModel{}

	runner := agent.NewRunner(agent.Options{
		Model:    model,
		Registry: agent.NewRegistry(),
		History:  agent.NewHistory(service, ""),
	})

	if err := runner.Compact(ctx, created.ID); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	if model.calls != 0 {
		t.Errorf("the model was called %d times, want 0", model.calls)
	}

	messages, err := service.Messages(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	for _, message := range messages {
		if message.Role == session.RoleSummary {
			t.Error("a short conversation was summarised")
		}
	}
}

// TestCompactWithoutAConversation verifies that the compaction of
// nothing is nothing.
func TestCompactWithoutAConversation(t *testing.T) {
	runner := agent.NewRunner(agent.Options{Model: &fakeModel{}})

	if err := runner.Compact(context.Background(), ""); err != nil {
		t.Errorf("Compact() error = %v", err)
	}
}

// messageText returns the text of a model message.
func messageText(message fantasy.Message) string {
	text := ""

	for _, part := range message.Content {
		if value, ok := part.(fantasy.TextPart); ok {
			text += value.Text
		}
	}

	return text
}
