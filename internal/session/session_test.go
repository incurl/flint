package session_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"

	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/session"
)

// newStore creates an embedded store backed by a temporary PocketBase
// data directory, with the session collections in place.
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

// TestEnsureCollections verifies that the collections are created and
// that the call is idempotent.
func TestEnsureCollections(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()

	collections, err := store.Collections(ctx)
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	found := map[string]bool{}

	for _, collection := range collections {
		found[collection.Name] = true
	}

	for _, want := range []string{session.SessionsCollection, session.MessagesCollection} {
		if !found[want] {
			t.Errorf("the collection %q was not created", want)
		}
	}

	// The call is idempotent: a second run does not fail.
	if err := session.EnsureCollections(ctx, store); err != nil {
		t.Errorf("EnsureCollections() error = %v on the second call", err)
	}
}

// TestCreateSession verifies that a session is stored and read back.
func TestCreateSession(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "  My chat  ")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if created.Title != "My chat" {
		t.Errorf("the title is %q, want %q", created.Title, "My chat")
	}

	found, err := service.Session(ctx, created.ID)
	if err != nil {
		t.Fatalf("Session() error = %v", err)
	}

	if found.ID != created.ID {
		t.Errorf("the session id is %q, want %q", found.ID, created.ID)
	}

	if found.Title != "My chat" {
		t.Errorf("the title is %q, want %q", found.Title, "My chat")
	}
}

// TestCreateSessionWithoutTitle verifies that a session gets a default
// title.
func TestCreateSessionWithoutTitle(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)

	created, err := service.CreateSession(context.Background(), "")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if created.Title == "" {
		t.Error("the session has no title")
	}
}

// TestAppendAndReadMessages verifies that the messages of a session
// survive a round trip through the store.
func TestAppendAndReadMessages(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, err := service.CreateSession(ctx, "chat")
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := service.AppendMessage(ctx, created.ID, session.Message{
		Role: session.RoleUser,
		Turn: 1,
		Text: "how many records?",
	}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	if _, err := service.AppendMessage(ctx, created.ID, session.Message{
		Role:       session.RoleTool,
		Turn:       1,
		ToolName:   "count_records",
		ToolInput:  `{"collection":"vocab"}`,
		ToolOutput: "42 records",
	}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	if _, err := service.AppendMessage(ctx, created.ID, session.Message{
		Role: session.RoleAssistant,
		Turn: 1,
		Text: "there are 42 records",
	}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	messages, err := service.Messages(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	if len(messages) != 3 {
		t.Fatalf("the session holds %d messages, want 3", len(messages))
	}

	if messages[0].Text != "how many records?" {
		t.Errorf("the first message is %q, want the prompt", messages[0].Text)
	}

	if messages[1].ToolName != "count_records" {
		t.Errorf("the tool message names %q, want count_records", messages[1].ToolName)
	}

	if messages[1].ToolOutput != "42 records" {
		t.Errorf("the tool output is %q, want %q", messages[1].ToolOutput, "42 records")
	}

	if messages[2].Text != "there are 42 records" {
		t.Errorf("the last message is %q, want the answer", messages[2].Text)
	}

	// The messages belong to the session.
	for _, message := range messages {
		if message.SessionID != created.ID {
			t.Errorf("the message belongs to %q, want %q", message.SessionID, created.ID)
		}
	}
}

// TestMessagesAreScopedToTheSession verifies that the messages of one
// session do not leak into another.
func TestMessagesAreScopedToTheSession(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	first, _ := service.CreateSession(ctx, "first")
	second, _ := service.CreateSession(ctx, "second")

	if _, err := service.AppendMessage(ctx, first.ID, session.Message{
		Role: session.RoleUser,
		Text: "only in the first",
	}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	messages, err := service.Messages(ctx, second.ID, 0)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	if len(messages) != 0 {
		t.Errorf("the second session holds %d messages, want 0", len(messages))
	}
}

// TestSessionsAreListed verifies that the sessions are listed.
func TestSessionsAreListed(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	if _, err := service.CreateSession(ctx, "first"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := service.CreateSession(ctx, "second"); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	sessions, err := service.Sessions(ctx, 10)
	if err != nil {
		t.Fatalf("Sessions() error = %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("the store holds %d sessions, want 2", len(sessions))
	}
}

// TestDeleteSession verifies that deleting a session removes its
// messages too.
func TestDeleteSession(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, _ := service.CreateSession(ctx, "chat")

	if _, err := service.AppendMessage(ctx, created.ID, session.Message{
		Role: session.RoleUser,
		Text: "hello",
	}); err != nil {
		t.Fatalf("AppendMessage() error = %v", err)
	}

	if err := service.DeleteSession(ctx, created.ID); err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}

	if _, err := service.Session(ctx, created.ID); !errors.Is(err, session.ErrNotFound) {
		t.Errorf("Session() error = %v, want ErrNotFound", err)
	}

	messages, err := service.Messages(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	if len(messages) != 0 {
		t.Errorf("the deleted session still holds %d messages", len(messages))
	}
}

// TestMigrationsCreateTheCollections verifies that the server side
// migration creates the same collections as the embedded backend.
//
// The two paths have to agree: a deployment that switches backends must
// not lose its conversations.
func TestMigrationsCreateTheCollections(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pb_data")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	// The migration saves a collection, which needs a database.
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}

	t.Cleanup(func() { _ = app.ClearBootstrap() })

	migrations := session.Migrations()
	if len(migrations) == 0 {
		t.Fatal("Migrations() = no migrations")
	}

	for _, migration := range migrations {
		if err := migration.Up(app); err != nil {
			t.Fatalf("the migration failed: %v", err)
		}
	}

	collections, err := app.FindAllCollections()
	if err != nil {
		t.Fatalf("FindAllCollections() error = %v", err)
	}

	found := map[string]bool{}

	for _, collection := range collections {
		found[collection.Name] = true
	}

	for _, want := range []string{session.SessionsCollection, session.MessagesCollection} {
		if !found[want] {
			t.Errorf("the migration did not create the collection %q", want)
		}
	}
}

// TestServiceWithoutStore verifies that the service reports the missing
// store instead of panicking.
func TestServiceWithoutStore(t *testing.T) {
	service := session.NewService(nil)
	ctx := context.Background()

	if _, err := service.CreateSession(ctx, "chat"); err == nil {
		t.Error("CreateSession() returned no error without a store")
	}

	if _, err := service.Messages(ctx, "x", 0); err == nil {
		t.Error("Messages() returned no error without a store")
	}
}

// TestBrokenPayloadIsStillReadable verifies that a message whose payload
// cannot be decoded is still returned.
//
// A payload written by a newer version must not make the whole
// conversation unreadable.
func TestBrokenPayloadIsStillReadable(t *testing.T) {
	store := newStore(t)
	service := session.NewService(store)
	ctx := context.Background()

	created, _ := service.CreateSession(ctx, "chat")

	if _, err := store.Create(ctx, session.MessagesCollection, map[string]any{
		session.FieldSessionID: created.ID,
		session.FieldRole:      session.RoleAssistant,
		session.FieldContent:   "{not json",
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	messages, err := service.Messages(ctx, created.ID, 0)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("the session holds %d messages, want 1", len(messages))
	}

	if !strings.Contains(messages[0].Text, "{not json") {
		t.Errorf("the message is %q, want the raw payload", messages[0].Text)
	}
}
