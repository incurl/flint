package agent

import (
	"context"
	"fmt"
	"strings"

	"charm.land/fantasy"

	"github.com/incurl/flint/internal/session"
)

// The bounds of the compaction.
//
// The threshold is how many characters the conversation may hold before
// it is summarised, and the keep is how many of the last turns survive
// the summary untouched.
const (
	// CompactThreshold is the number of characters the conversation may
	// hold before it is summarised.
	//
	// It is a rough measure, and a rough measure is the right one: the
	// exact number of tokens depends on the model and on the
	// tokenizer, and a template cannot know either.
	CompactThreshold = 24_000

	// CompactKeep is the number of turns that are kept as they are.
	//
	// The last turns are the ones the conversation is about, and the
	// model needs them verbatim: a summary of the last exchange loses
	// the details the model is about to act on.
	CompactKeep = 6
)

// The summary prompt.
//
// The summary is written for the model that continues the conversation,
// so it says what matters to a model and not to a person: the state of
// the work, the decisions, and the names.
const summaryPrompt = `You are summarising a conversation between a user and an assistant that reads and writes PocketBase records.

Write a summary that lets the assistant continue the conversation without seeing it. Include:
- what the user asked for, and what was done about it
- the collections, the filters and the fields that were used, exactly
- the decisions that were made, and the reasons given
- what is still open, and what the next step is

Do not invent anything. Keep the names of the collections, the fields and the identifiers exact: the assistant uses them to work with the data.

Write the summary as plain prose and lists. No preamble.`

// Compact summarises a conversation that grew too long.
//
// It returns the messages the model should see instead of the whole
// history: a summary of the older turns, and the last turns as they
// are.
//
// The summary is produced by the model itself, with the same model
// that runs the conversation: a second, cheaper model would need a
// configuration the template does not have.
//
// A conversation that cannot be summarised is not fatal: the run
// continues with the whole history, which is what happened before.
func (r *Runner) Compact(ctx context.Context, sessionID string) error {
	if sessionID == "" || r.options.History == nil {
		return nil
	}

	conversation := NewHistory(r.options.History.service, sessionID)

	messages, err := conversation.service.Messages(ctx, sessionID, 0)
	if err != nil {
		return fmt.Errorf("agent: read the conversation: %w", err)
	}

	if len(messages) == 0 {
		return nil
	}

	// The turns that are kept are not summarised, so the history is
	// only compacted when there is something to compact.
	if len(messages) <= CompactKeep {
		return nil
	}

	if conversationSize(messages) <= CompactThreshold {
		return nil
	}

	// The last turns are kept as they are, and the older ones are the
	// ones the summary replaces.
	older := messages[:len(messages)-CompactKeep]

	model := r.Model()
	if model == nil {
		return ErrNoModel
	}

	summary, err := r.summarise(ctx, model, older)
	if err != nil {
		return err
	}

	if summary == "" {
		return nil
	}

	// The summary is stored as a message of the conversation, so that
	// it survives a restart and the next turn sees it.
	conversation.AppendSummary(ctx, 0, len(older), summary)

	return nil
}

// summarise asks the model for a summary of the turns.
func (r *Runner) summarise(ctx context.Context, model fantasy.LanguageModel, messages []session.Message) (string, error) {
	prompt := summarisePrompt(messages)

	agent := fantasy.NewAgent(model, fantasy.WithSystemPrompt(summaryPrompt))

	var text string

	result, err := agent.Stream(ctx, fantasy.AgentStreamCall{
		Prompt: prompt,
		OnTextDelta: func(_ string, delta string) error {
			text += delta

			return nil
		},
	})
	if err != nil {
		return "", fmt.Errorf("agent: summarise the conversation: %w", err)
	}

	if result != nil {
		if final := resultText(result, text); final != "" {
			return final, nil
		}
	}

	return text, nil
}

// summarisePrompt builds the prompt that asks for the summary.
//
// The turns are rendered as text, because the model reads text better
// than a transcript, and because the tool calls of the older turns are
// noise: what matters is what was decided and done.
func summarisePrompt(messages []session.Message) string {
	var b strings.Builder

	b.WriteString("Summarise this conversation.\n\n")

	for _, message := range messages {
		switch message.Role {
		case session.RoleUser:
			b.WriteString("user: ")
			b.WriteString(message.Text)
			b.WriteString("\n\n")

		case session.RoleAssistant:
			b.WriteString("assistant: ")
			b.WriteString(message.Text)
			b.WriteString("\n\n")

		case session.RoleTool:
			// The tool calls are summarised in one line: the model
			// needs to know what was touched, not the arguments.
			b.WriteString("tool: ")
			b.WriteString(message.ToolName)
			b.WriteString("\n\n")
		}
	}

	return b.String()
}

// conversationSize returns the rough size of a conversation.
//
// It counts the characters of the text and a fixed amount for every
// tool call, because a tool call costs tokens in its arguments and in
// its result.
func conversationSize(messages []session.Message) int {
	size := 0

	for _, message := range messages {
		size += len(message.Text)
		size += len(message.ToolInput)
		size += len(message.ToolOutput)

		if message.ToolName != "" {
			size += 64
		}
	}

	return size
}
