package agent_test

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/session"
)

// longConversation fills a conversation with turns that are long
// enough to pass the compaction threshold.
func longConversation(service *session.Service, sessionID string) error {
	for i := range 20 {
		_, err := service.AppendMessage(context.Background(), sessionID, session.Message{
			Role: session.RoleUser,
			Turn: i + 1,
			Text: strings.Repeat("filler ", 1000),
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// summaries returns the summaries of a conversation.
func summaries(t *testing.T, service *session.Service, sessionID string) []session.Message {
	t.Helper()

	messages, err := service.Messages(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	var out []session.Message

	for _, message := range messages {
		if message.Role == session.RoleSummary {
			out = append(out, message)
		}
	}

	return out
}

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

	if err := longConversation(service, created.ID); err != nil {
		t.Fatalf("longConversation() error = %v", err)
	}

	model := &fakeModel{turns: [][]fantasy.StreamPart{textTurn("The summary of the conversation.")}}

	runner := agent.NewRunner(agent.Options{
		Model:   model,
		History: agent.NewHistory(service, ""),
	})

	if err := runner.Compact(ctx, created.ID); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	found := summaries(t, service, created.ID)
	if len(found) != 1 {
		t.Fatalf("the conversation holds %d summaries, want 1", len(found))
	}

	if !strings.Contains(found[0].Text, "The summary of the conversation.") {
		t.Errorf("the summary does not hold the text: %q", found[0].Text)
	}

	// The model sees the summary and the turns that were kept.
	loaded := agent.NewHistory(service, created.ID).Load(ctx)

	if len(loaded) > agent.CompactKeep+1 {
		t.Errorf("the model sees %d messages, want at most %d", len(loaded), agent.CompactKeep+1)
	}

	if !strings.Contains(messageText(loaded[0]), "The summary of the conversation.") {
		t.Errorf("the first message is not the summary: %q", messageText(loaded[0]))
	}
}

// TestCompactTwice verifies that a second compaction works.
//
// The second summary covers the turns the first one covered as well, so
// the model sees one summary and not a pile of them.
func TestCompactTwice(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if err := longConversation(service, created.ID); err != nil {
		t.Fatalf("longConversation() error = %v", err)
	}

	first := &fakeModel{turns: [][]fantasy.StreamPart{textTurn("THE FIRST SUMMARY")}}

	runner := agent.NewRunner(agent.Options{
		Model:   first,
		History: agent.NewHistory(service, ""),
	})

	if err := runner.Compact(ctx, created.ID); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	// More turns, and a second compaction.
	if err := longConversation(service, created.ID); err != nil {
		t.Fatalf("longConversation() error = %v", err)
	}

	second := &fakeModel{turns: [][]fantasy.StreamPart{textTurn("THE SECOND SUMMARY")}}

	runner2 := agent.NewRunner(agent.Options{
		Model:   second,
		History: agent.NewHistory(service, ""),
	})

	if err := runner2.Compact(ctx, created.ID); err != nil {
		t.Fatalf("the second Compact() error = %v", err)
	}

	found := summaries(t, service, created.ID)
	if len(found) != 2 {
		t.Fatalf("the conversation holds %d summaries, want 2", len(found))
	}

	// The model sees the latest summary and the turns after it.
	loaded := agent.NewHistory(service, created.ID).Load(ctx)

	if !strings.Contains(messageText(loaded[0]), "THE SECOND SUMMARY") {
		t.Errorf("the first message is not the latest summary: %q", messageText(loaded[0]))
	}

	// The first summary is not on the screen any more.
	for _, message := range loaded {
		if strings.Contains(messageText(message), "THE FIRST SUMMARY") {
			t.Error("the model still sees the first summary")
		}
	}
}

// TestCompactKeepsTheSummaryOfTheEarlierTurns verifies that the
// summary of an earlier compaction is part of the conversation that the
// next summary is written from.
//
// Leaving it out would lose what happened before the turns that are
// summarised.
func TestCompactKeepsTheSummaryOfTheEarlierTurns(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if err := longConversation(service, created.ID); err != nil {
		t.Fatalf("longConversation() error = %v", err)
	}

	first := &fakeModel{turns: [][]fantasy.StreamPart{textTurn("THE FIRST SUMMARY")}}

	runner := agent.NewRunner(agent.Options{
		Model:   first,
		History: agent.NewHistory(service, ""),
	})

	if err := runner.Compact(ctx, created.ID); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	if err := longConversation(service, created.ID); err != nil {
		t.Fatalf("longConversation() error = %v", err)
	}

	// The second compaction is asked to summarise a conversation that
	// holds a summary: the prompt has to carry it.
	second := &recordingModel{}

	runner2 := agent.NewRunner(agent.Options{
		Model:   second,
		History: agent.NewHistory(service, ""),
	})

	if err := runner2.Compact(ctx, created.ID); err != nil {
		t.Fatalf("the second Compact() error = %v", err)
	}

	// The prompt the model received holds the first summary.
	second.mu.Lock()
	defer second.mu.Unlock()

	if len(second.calls) == 0 {
		t.Fatal("the model was not called")
	}

	found := false

	for _, message := range second.calls[0] {
		for _, part := range message.Content {
			if text, ok := part.(fantasy.TextPart); ok && strings.Contains(text.Text, "THE FIRST SUMMARY") {
				found = true
			}
		}
	}

	if !found {
		t.Error("the prompt of the second summary does not carry the first one")
	}
}

// TestTheRunSummarises verifies the whole flow: a run that finds a
// conversation that grew too long summarises it, and the model sees
// the summary.
func TestTheRunSummarises(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if err := longConversation(service, created.ID); err != nil {
		t.Fatalf("longConversation() error = %v", err)
	}

	// The model summarises first, then answers.
	model := &fakeModel{turns: [][]fantasy.StreamPart{
		textTurn("THE SUMMARY OF THE CONVERSATION"),
		textTurn("the answer"),
	}}

	runner := agent.NewRunner(agent.Options{
		Model:   model,
		History: agent.NewHistory(service, ""),
	})

	text, err := runner.Run(ctx, agent.RunRequest{
		RunID:     "r1",
		Prompt:    "and what do you think?",
		SessionID: created.ID,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if text != "the answer" {
		t.Errorf("Run() = %q, want the answer", text)
	}

	found := summaries(t, service, created.ID)
	if len(found) != 1 {
		t.Errorf("the conversation holds %d summaries, want 1", len(found))
	}

	// The model sees the summary, not the whole conversation.
	loaded := agent.NewHistory(service, created.ID).Load(ctx)

	if len(loaded) > agent.CompactKeep+1 {
		t.Errorf("the model sees %d messages, want at most %d", len(loaded), agent.CompactKeep+1)
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
		Model:   model,
		History: agent.NewHistory(service, ""),
	})

	if err := runner.Compact(ctx, created.ID); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	if model.calls != 0 {
		t.Errorf("the model was called %d times, want 0", model.calls)
	}

	if found := summaries(t, service, created.ID); len(found) != 0 {
		t.Errorf("a short conversation was summarised")
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

// TestALargeContextWindowIsNotCompactedEarly verifies that a
// conversation that fits in a large context window is left alone.
//
// A window of a million tokens holds a lot of conversation, and
// summarising it at six thousand tokens would cost a model call for
// every few turns.
func TestALargeContextWindowIsNotCompactedEarly(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// A conversation that is over the character threshold but far
	// under a million tokens.
	if err := longConversation(service, created.ID); err != nil {
		t.Fatalf("longConversation() error = %v", err)
	}

	model := &fakeModel{}

	runner := agent.NewRunner(agent.Options{
		Model:         model,
		History:       agent.NewHistory(service, ""),
		ContextWindow: 1_000_000,
	})

	if err := runner.Compact(ctx, created.ID); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	if model.calls != 0 {
		t.Errorf("the model was called %d times, want 0", model.calls)
	}
}

// TestANearlyFullContextWindowIsCompacted verifies that a conversation
// that is nearly full is summarised.
//
// The measure is what the provider reported for the last turn: it is
// the only measure that knows the tokenizer of the model.
func TestANearlyFullContextWindowIsCompacted(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	// A short conversation: the character estimate is far under the
	// window, so only the reported tokens can trigger the compaction.
	for i := range 10 {
		_, err := service.AppendMessage(ctx, created.ID, session.Message{
			Role: session.RoleUser,
			Turn: i + 1,
			Text: "a short question",
		})
		if err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	// The turn reports a usage that nearly fills the window.
	model := &fakeModel{turns: [][]fantasy.StreamPart{
		textTurnWithUsage("ok", fantasy.Usage{InputTokens: 29_000, OutputTokens: 1_000}),
		textTurn("THE SUMMARY"),
	}}

	runner := agent.NewRunner(agent.Options{
		Model:         model,
		History:       agent.NewHistory(service, ""),
		ContextWindow: 32_000,
	})

	// The turn that reports the usage has to go first -- it is the turn
	// the compaction is measured after.
	if _, err := runner.Run(ctx, agent.RunRequest{
		RunID:     "r0",
		Prompt:    "a question",
		SessionID: created.ID,
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// The count before the compaction: the run itself made a call.
	before := model.calls

	if err := runner.Compact(ctx, created.ID); err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	if model.calls != before+1 {
		t.Errorf("the compaction made %d model calls, want 1", model.calls-before)
	}
}
