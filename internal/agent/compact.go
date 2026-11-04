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
	// hold before it is summarised, when the context window of the model
	// is unknown.
	//
	// It is a rough measure, and a rough measure is the right one for a
	// window nobody knows: the exact number of tokens depends on the
	// model and on the tokenizer.
	CompactThreshold = 24_000

	// CompactKeep is the number of turns that are kept as they are.
	//
	// The last turns are the ones the conversation is about, and the
	// model needs them verbatim: a summary of the last exchange loses
	// the details the model is about to act on.
	CompactKeep = 6

	// compactLargeWindow is the context window above which the reserve
	// is a fixed number of tokens rather than a share of the window.
	//
	// A window of a million tokens does not need a fifth of itself kept
	// free: a fixed reserve is what a conversation actually needs.
	compactLargeWindow = 100_000

	// compactLargeReserve is the room left free in a large window.
	compactLargeReserve = 16_000

	// compactSmallRatio is the share of a small window kept free.
	//
	// A fifth of the window is room for one answer and the tool calls
	// that go with it.
	compactSmallRatio = 0.2
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

	if !r.needsCompaction(sessionID, messages) {
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
	//
	// The conversation is pointed at it, so that the loader reads the
	// summary and the turns after it.
	//
	// The summaries of the earlier compactions are left in the store:
	// they are the history of the conversation, and deleting them would
	// lose what the user did. The conversation points at the latest one,
	// so the model sees one summary and not a pile of them.
	if _, err := conversation.AppendSummary(ctx, 0, summary); err != nil {
		return fmt.Errorf("agent: store the summary: %w", err)
	}

	return nil
}

// needsCompaction reports whether a conversation is full.
//
// The measure is the context window of the model, when it is known: a
// conversation is summarised when the room left in the window is down
// to the reserve, and not before. A window of a million tokens holds a
// lot of conversation, and summarising it at six thousand tokens would
// cost a model call for every few turns.
//
// When the window is unknown the character count decides, which is a
// rough measure and the right one for a model that does not say.
func (r *Runner) needsCompaction(sessionID string, messages []session.Message) bool {
	window := r.options.ContextWindow

	if window <= 0 {
		return conversationSize(messages) > CompactThreshold
	}

	// The tokens the conversation holds: what the provider reported for
	// the last turn, plus what the turns it has not seen yet will cost.
	used := r.usage(sessionID)
	if used <= 0 {
		// Nothing has been reported yet: the characters are the only
		// thing there is to go on.
		used = int(float64(conversationSize(messages)) * tokensPerCharacter)
	}

	reserve := compactLargeReserve
	if window <= compactLargeWindow {
		reserve = int(float64(window) * compactSmallRatio)
	}

	return window-used <= reserve
}

// tokensPerCharacter is the rough number of tokens a character is worth.
//
// It is a quarter, the usual figure for English prose: four characters
// to a token. It is only used when the provider has not reported a
// usage yet.
const tokensPerCharacter = 0.25

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

		case session.RoleSummary:
			// The summary of an earlier compaction is part of the
			// conversation: it carries what happened before the turns
			// that are summarised here. Leaving it out would lose it.
			b.WriteString("summary of the earlier turns: ")
			b.WriteString(strings.TrimSpace(message.Text))
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
