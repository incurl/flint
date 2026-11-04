package session_test

import (
	"context"
	"strings"
	"testing"

	"github.com/incurl/flint/internal/session"
)

// TestMessagesReadsEveryPage verifies that a conversation longer than a
// page is read whole.
//
// A single page holds a hundred messages. A conversation that grew past
// that was truncated: the model saw the first hundred turns and never
// the ones after them, and the compaction measured a conversation that
// was not the one the user had.
func TestMessagesReadsEveryPage(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	const count = 150

	for i := range count {
		_, err := service.AppendMessage(ctx, created.ID, session.Message{
			Role: session.RoleUser,
			Turn: i + 1,
			Text: strings.Repeat("a message ", 10),
		})
		if err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	messages, err := service.Messages(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	if len(messages) != count {
		t.Errorf("the query read %d messages, want %d", len(messages), count)
	}

	// The messages are in the order they were written.
	if messages[0].Text != strings.Repeat("a message ", 10) {
		t.Error("the first message is not the first one that was written")
	}
}

// TestMessagesHonoursTheLimit verifies that the limit still works.
func TestMessagesHonoursTheLimit(t *testing.T) {
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
			Text: "a message",
		})
		if err != nil {
			t.Fatalf("AppendMessage() error = %v", err)
		}
	}

	messages, err := service.Messages(ctx, created.ID, 5)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	if len(messages) != 5 {
		t.Errorf("the query read %d messages, want 5", len(messages))
	}
}
