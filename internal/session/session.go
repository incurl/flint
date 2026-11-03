// Package session persists the conversations of the agent.
//
// The conversations live in PocketBase, like every other piece of data
// of the template. That is what makes them survive a restart, and what
// makes them travel to a remote backend with the rest of the data: the
// agent does not need a storage of its own.
//
// Two collections back a conversation:
//
//   - a session holds the metadata of a conversation;
//   - a message holds one turn of it.
//
// The messages are stored as JSON, because a turn is not a flat record:
// it carries the text of the assistant, the tool calls it made and the
// results it got. The JSON column keeps the schema of a turn out of the
// collection schema, so a new kind of turn does not need a migration.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/incurl/flint/internal/pbstore"
)

// The names of the collections that hold the conversations.
const (
	// SessionsCollection holds the sessions.
	SessionsCollection = "_flint_sessions"
	// MessagesCollection holds the messages of the sessions.
	MessagesCollection = "_flint_messages"
)

// The names of the fields of the collections.
//
// They are exported because a downstream project that writes its own
// migrations needs them.
const (
	// FieldTitle is the title of a session.
	FieldTitle = "title"
	// FieldSessionID links a message to its session.
	FieldSessionID = "session"
	// FieldRole is the role of the author of a message.
	FieldRole = "role"
	// FieldContent is the JSON payload of a message.
	FieldContent = "content"
	// FieldTurn is the number of the turn the message belongs to.
	FieldTurn = "turn"
)

// ErrNotFound is returned when a session does not exist.
var ErrNotFound = errors.New("session: not found")

// The roles a message can have.
const (
	// RoleUser marks what the user wrote.
	RoleUser = "user"
	// RoleAssistant marks what the model answered.
	RoleAssistant = "assistant"
	// RoleTool marks a tool call and its result.
	RoleTool = "tool"
)

// Message is one turn of a conversation.
//
// The content is kept as a JSON document, so a turn can carry whatever
// the model produced without a migration.
type Message struct {
	// ID is the identifier of the message.
	ID string
	// SessionID is the session the message belongs to.
	SessionID string
	// Role is who produced the message.
	Role string
	// Turn is the number of the turn, starting at 1.
	Turn int
	// Text is the text of the message.
	Text string
	// ToolName is the name of the tool, for a tool message.
	ToolName string
	// ToolInput holds the arguments of a tool call.
	ToolInput string
	// ToolOutput holds the result of a tool call.
	ToolOutput string
	// Created is the creation timestamp.
	Created time.Time
}

// payload is the JSON form of a message.
//
// It is what the collection holds. The tool fields are omitted when
// they are empty, so a plain message stays small.
type payload struct {
	Role       string `json:"role"`
	Turn       int    `json:"turn"`
	Text       string `json:"text,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolInput  string `json:"tool_input,omitempty"`
	ToolOutput string `json:"tool_output,omitempty"`
}

// Service reads and writes the conversations.
//
// It is safe for concurrent use: the store underneath is.
type Service struct {
	// store is the data layer.
	store pbstore.Store
}

// NewService returns a service backed by the given store.
func NewService(store pbstore.Store) *Service {
	return &Service{store: store}
}

// Store returns the store the service writes to.
//
// It is the door the setup uses to create the collections.
func (s *Service) Store() pbstore.Store { return s.store }

// Session is the metadata of a conversation.
type Session struct {
	// ID is the identifier of the session.
	ID string
	// Title is the name of the session.
	Title string
	// Created is the creation timestamp.
	Created time.Time
	// Updated is the last update timestamp.
	Updated time.Time
}

// CreateSession creates a session and returns it.
func (s *Service) CreateSession(ctx context.Context, title string) (*Session, error) {
	if s.store == nil {
		return nil, errors.New("session: no store configured")
	}

	title = strings.TrimSpace(title)
	if title == "" {
		title = "New session"
	}

	record, err := s.store.Create(ctx, SessionsCollection, map[string]any{
		FieldTitle: title,
	})
	if err != nil {
		return nil, fmt.Errorf("session: create the session: %w", err)
	}

	return sessionFromRecord(record), nil
}

// Session returns the session with the given id.
func (s *Service) Session(ctx context.Context, id string) (*Session, error) {
	if s.store == nil {
		return nil, errors.New("session: no store configured")
	}

	record, err := s.store.Get(ctx, SessionsCollection, id)
	if err != nil {
		if errors.Is(err, pbstore.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}

		return nil, err
	}

	return sessionFromRecord(record), nil
}

// Sessions returns the sessions, most recent first.
func (s *Service) Sessions(ctx context.Context, limit int) ([]Session, error) {
	if s.store == nil {
		return nil, errors.New("session: no store configured")
	}

	if limit <= 0 {
		limit = 20
	}

	result, err := s.store.List(ctx, pbstore.Query{
		Collection: SessionsCollection,
		Sort:       "-created",
		Page:       1,
		PerPage:    limit,
	})
	if err != nil {
		return nil, err
	}

	out := make([]Session, 0, len(result.Records))

	for _, record := range result.Records {
		out = append(out, *sessionFromRecord(record))
	}

	return out, nil
}

// DeleteSession removes a session and its messages.
func (s *Service) DeleteSession(ctx context.Context, id string) error {
	if s.store == nil {
		return errors.New("session: no store configured")
	}

	messages, err := s.Messages(ctx, id, 0)
	if err != nil {
		return err
	}

	for _, message := range messages {
		if err := s.store.Delete(ctx, MessagesCollection, message.ID); err != nil {
			return err
		}
	}

	if err := s.store.Delete(ctx, SessionsCollection, id); err != nil {
		return err
	}

	return nil
}

// AppendMessage adds a message to a session.
func (s *Service) AppendMessage(ctx context.Context, sessionID string, message Message) (*Message, error) {
	if s.store == nil {
		return nil, errors.New("session: no store configured")
	}

	content, err := json.Marshal(payload{
		Role:       message.Role,
		Turn:       message.Turn,
		Text:       message.Text,
		ToolName:   message.ToolName,
		ToolInput:  message.ToolInput,
		ToolOutput: message.ToolOutput,
	})
	if err != nil {
		return nil, fmt.Errorf("session: encode the message: %w", err)
	}

	record, err := s.store.Create(ctx, MessagesCollection, map[string]any{
		FieldSessionID: sessionID,
		FieldRole:      message.Role,
		FieldTurn:      message.Turn,
		FieldContent:   string(content),
	})
	if err != nil {
		return nil, fmt.Errorf("session: append the message: %w", err)
	}

	out := message
	out.ID = record.ID
	out.SessionID = sessionID

	return &out, nil
}

// Messages returns the messages of a session, oldest first.
//
// A limit of 0 returns every message.
func (s *Service) Messages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	if s.store == nil {
		return nil, errors.New("session: no store configured")
	}

	query := pbstore.Query{
		Collection: MessagesCollection,
		Filter:     FieldSessionID + " = '" + sessionID + "'",
		Sort:       "created",
		Page:       1,
		PerPage:    100,
	}

	if limit > 0 && limit < query.PerPage {
		query.PerPage = limit
	}

	result, err := s.store.List(ctx, query)
	if err != nil {
		return nil, err
	}

	out := make([]Message, 0, len(result.Records))

	for _, record := range result.Records {
		out = append(out, *messageFromRecord(record))
	}

	return out, nil
}

// sessionFromRecord converts a stored record into a session.
func sessionFromRecord(record *pbstore.Record) *Session {
	if record == nil {
		return nil
	}

	out := &Session{
		ID:    record.ID,
		Title: record.StringField(FieldTitle),
	}

	if created, err := record.TimeField("created"); err == nil {
		out.Created = created
	}

	if updated, err := record.TimeField("updated"); err == nil {
		out.Updated = updated
	}

	return out
}

// messageFromRecord converts a stored record into a message.
func messageFromRecord(record *pbstore.Record) *Message {
	if record == nil {
		return nil
	}

	out := &Message{
		ID:        record.ID,
		SessionID: record.StringField(FieldSessionID),
		Role:      record.StringField(FieldRole),
	}

	if turn, ok := record.Field(FieldTurn); ok {
		out.Turn = int(toFloat(turn))
	}

	if created, err := record.TimeField("created"); err == nil {
		out.Created = created
	}

	// A message whose payload cannot be decoded is still worth showing:
	// it keeps its role and its turn, and its text says what happened.
	raw := record.StringField(FieldContent)
	if raw == "" {
		return out
	}

	var content payload
	if err := json.Unmarshal([]byte(raw), &content); err != nil {
		out.Text = raw

		return out
	}

	if content.Role != "" {
		out.Role = content.Role
	}

	if content.Turn > 0 {
		out.Turn = content.Turn
	}

	out.Text = content.Text
	out.ToolName = content.ToolName
	out.ToolInput = content.ToolInput
	out.ToolOutput = content.ToolOutput

	return out
}

// toFloat converts a numeric field to a float64.
func toFloat(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}
