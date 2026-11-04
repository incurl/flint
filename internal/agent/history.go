package agent

import (
	"context"
	"fmt"
	"strconv"
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

	messages, err := h.service.Messages(ctx, h.sessionID, 0)
	if err != nil {
		return nil
	}

	// A compacted conversation is stored as the turns the summary
	// replaces, then the turns that were kept, then the summary. The
	// model sees the summary and the kept turns: the summary carries
	// the past, the kept turns carry the present.
	out := make([]fantasy.Message, 0, len(messages))

	for _, message := range compacted(messages) {
		out = append(out, historyMessage(message))
	}

	return out
}

// compacted returns the messages the model has to see.
//
// It is the summary of a compacted conversation, followed by the turns
// that were kept, and it is the whole of a conversation that was never
// compacted.
func compacted(messages []session.Message) []session.Message {
	if len(messages) == 0 {
		return messages
	}

	last := messages[len(messages)-1]
	if last.Role != session.RoleSummary {
		return messages
	}

	// The summary says how many turns it replaces. The turns it
	// replaces are the first ones of the conversation.
	replaces := summaryReplaces(last.Text)

	kept := len(messages) - 1 - replaces
	if kept < 0 {
		kept = 0
	}

	out := make([]session.Message, 0, kept+1)
	out = append(out, last)

	out = append(out, messages[len(messages)-1-kept:]...)

	return out
}

// summaryReplaces reads the number of turns a summary replaces.
//
// It is zero when the text does not carry the number, which is the
// case for a summary that was written by hand.
func summaryReplaces(text string) int {
	const prefix = "[summary of the earlier "

	if !strings.HasPrefix(text, prefix) {
		return 0
	}

	end := strings.Index(text, " turns]")
	if end < 0 {
		return 0
	}

	count, err := strconv.Atoi(text[len(prefix):end])
	if err != nil {
		return 0
	}

	return count
}

// summaryMarker opens the text of a summary.
//
// It carries the number of the turns the summary replaces, so that the
// loader knows where the summary sits: the summary is stored at the
// end of the conversation, and the turns it replaces are before it.
//
// The marker is hidden from the model: it is stripped before the
// message is given to it.
func summaryMarker(replaces int) string {
	return fmt.Sprintf("[summary of the earlier %d turns]", replaces)
}

// summaryText returns the text of a summary, without the marker.
func summaryText(text string) string {
	if index := strings.Index(text, "\n\n"); index >= 0 {
		return strings.TrimSpace(text[index+2:])
	}

	return strings.TrimSpace(text)
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
// The number of the turns it replaces is written in the summary, so
// that the loader knows where the summary sits among the turns.
func (h *History) AppendSummary(ctx context.Context, turn, replaces int, text string) {
	h.append(ctx, turn, session.RoleSummary, summaryMarker(replaces)+"\n\n"+text, "", "", "")
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
		// the context of the conversation, not a question. The marker
		// is stripped: it is for the application, not for the model.
		return fantasy.Message{
			Role:    fantasy.MessageRoleUser,
			Content: []fantasy.MessagePart{fantasy.TextPart{Text: summaryText(message.Text)}},
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
