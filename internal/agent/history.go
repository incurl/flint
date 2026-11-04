package agent

import (
	"context"
	"strings"

	"charm.land/fantasy"

	"github.com/incurl/flint/internal/session"
)

// History is the conversation a run continues.
//
// It is the bridge between the stored conversations and the model: the
// messages are read from the store, turned into the messages the model
// library understands, and the turns of the run are written back.
type History struct {
	// service reads and writes the conversations.
	service *session.Service
	// sessionID names the conversation.
	sessionID string
}

// NewHistory returns the history of a conversation.
//
// A nil service means that the conversation is not persisted: the run
// then works on the history it is given and nothing else.
//
// The session id may be empty: the history is then a template that the
// run replaces with the conversation it names. That is what lets the
// page create the conversation on its first turn.
func NewHistory(service *session.Service, sessionID string) *History {
	if service == nil {
		return nil
	}

	return &History{service: service, sessionID: sessionID}
}

// SessionID returns the conversation the history belongs to.
func (h *History) SessionID() string {
	if h == nil {
		return ""
	}

	return h.sessionID
}

// Load returns the messages of the conversation, in the form the model
// library expects.
//
// A conversation that cannot be read is not fatal: the run continues
// with an empty history rather than failing to start.
func (h *History) Load(ctx context.Context) []fantasy.Message {
	if h == nil {
		return nil
	}

	// The conversation knows the message it starts from: the summary
	// of the earlier turns, when it was compacted.
	summaryID := ""

	if current, err := h.service.Session(ctx, h.sessionID); err == nil && current != nil {
		summaryID = current.Summary
	}

	messages, err := h.service.MessagesFromSummary(ctx, h.sessionID, summaryID)
	if err != nil {
		return nil
	}

	// A compacted conversation is stored as the turns the summary
	// replaces, then the summary, then the turns that were kept. The
	// model sees the summary and the kept turns: the summary carries
	// the past, the kept turns carry the present.
	out := make([]fantasy.Message, 0, len(messages))

	for _, message := range messages {
		out = append(out, historyMessage(message))
	}

	return out
}

// AppendUser records the prompt of the user.
func (h *History) AppendUser(ctx context.Context, turn int, text string) {
	h.append(ctx, turn, session.RoleUser, text, "", "", "")
}

// AppendAssistant records the answer of the model.
func (h *History) AppendAssistant(ctx context.Context, turn int, text string) {
	h.append(ctx, turn, session.RoleAssistant, text, "", "", "")
}

// AppendTool records a tool call and its result.
func (h *History) AppendTool(ctx context.Context, turn int, name, input, output string) {
	h.append(ctx, turn, session.RoleTool, "", name, input, output)
}

// AppendSummary records the summary of a compacted conversation.
//
// The summary is a message of the conversation like any other, so it
// survives a restart. The turns it replaces are left in the store:
// they are the history of the conversation, and deleting them would
// lose what the user did.
//
// The conversation is pointed at the summary, so that the loader reads
// the summary and the turns after it -- and not the turns the summary
// stands for.
//
// It returns the identifier of the summary message.
func (h *History) AppendSummary(ctx context.Context, turn int, text string) (string, error) {
	if h == nil {
		return "", nil
	}

	written, err := h.service.AppendMessage(ctx, h.sessionID, session.Message{
		Role: session.RoleSummary,
		Turn: turn,
		Text: text,
	})
	if err != nil {
		return "", err
	}

	if written == nil {
		return "", nil
	}

	if err := h.service.SetSummary(ctx, h.sessionID, written.ID); err != nil {
		return "", err
	}

	return written.ID, nil
}

// append writes a message to the conversation.
//
// A message that cannot be written is not fatal: the conversation is a
// convenience, and a failed write must not take the run down.
func (h *History) append(ctx context.Context, turn int, role, text, name, input, output string) {
	if h == nil {
		return
	}

	_, _ = h.service.AppendMessage(ctx, h.sessionID, session.Message{
		Role:       role,
		Turn:       turn,
		Text:       text,
		ToolName:   name,
		ToolInput:  input,
		ToolOutput: output,
	})
}

// historyMessage converts a stored message into a model message.
func historyMessage(message session.Message) fantasy.Message {
	switch message.Role {
	case session.RoleUser:
		return fantasy.NewUserMessage(message.Text)

	case session.RoleSummary:
		// The summary is given to the model as a user message: it is
		// the context of the conversation, not a question.
		return fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: strings.TrimSpace(message.Text)}},
		}

	case session.RoleAssistant:
		return fantasy.Message{
			Role:    fantasy.MessageRoleAssistant,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: message.Text}},
		}

	case session.RoleTool:
		// The tool call and its result are two parts of one message:
		// the model has to see both to continue the conversation.
		return fantasy.Message{
			Role: fantasy.MessageRoleTool,
			Content: []fantasy.MessagePart{
				fantasy.ToolCallPart{
					ToolCallID: message.ID,
					ToolName:   message.ToolName,
					Input:      message.ToolInput,
				},
				fantasy.ToolResultPart{
					ToolCallID: message.ID,
					Output:     fantasy.ToolResultOutputContentText{Text: message.ToolOutput},
				},
			},
		}

	default:
		return fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: message.Text}},
		}
	}
}
