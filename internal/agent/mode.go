package agent

import (
	"fmt"
	"strings"
)

// Mode tells the agent what it is allowed to do.
//
// The mode is not a prompt: it decides which tools the model can even
// see. In [ModePlan] the tools that change the data are absent from
// the tool list, so a model that asks to create a record gets a
// "tool not found" error rather than a promise it may not keep.
//
// That is the whole point of the mode. A prompt that says "do not
// change anything" is a request, and a model may ignore it. A tool
// that is not registered cannot be called, whatever the model decides.
type Mode int

const (
	// ModeAct is the normal mode: the agent reads and writes the
	// records, and the permission gate asks the user before every
	// change.
	ModeAct Mode = iota

	// ModePlan is the planning mode: the agent reads the records and
	// produces a plan, and it cannot change anything.
	//
	// The plan is a proposal, not an action. The user reads it and
	// approves it, and the agent then carries it out in [ModeAct].
	ModePlan
)

// String returns the name of the mode.
func (m Mode) String() string {
	switch m {
	case ModePlan:
		return "plan"
	case ModeAct:
		return "act"
	default:
		return "unknown"
	}
}

// IsPlan reports whether the mode is [ModePlan].
func (m Mode) IsPlan() bool { return m == ModePlan }

// readOnlyTools are the tools a planning run may use.
//
// They are the tools that only read. The mutating ones are absent, so
// the model cannot call them — not even by accident, and not even if
// it is asked to.
var readOnlyTools = []string{
	ToolListRecords,
	ToolGetRecord,
	ToolSearchRecords,
	ToolCountRecords,
	ToolListCollections,
}

// mutatingTools are the tools that change the data.
//
// They are the ones the planning mode withholds. A downstream project
// that adds a tool has to decide whether it belongs here: a tool that
// writes belongs in this list, or the planning mode can call it.
var mutatingTools = []string{
	ToolCreateRecord,
	ToolUpdateRecord,
	ToolDeleteRecord,
}

// IsMutatingTool reports whether a tool changes the data.
//
// It is the classification the planning mode uses, and it is exported
// so that a downstream project can ask the same question about its own
// tools.
func IsMutatingTool(name string) bool {
	for _, tool := range mutatingTools {
		if tool == name {
			return true
		}
	}

	return false
}

// The plan markers.
//
// The model wraps its plan in them, and the page looks for the second
// one to know that the plan is ready to be approved. They are HTML
// comments so that they survive the markdown rendering as nothing at
// all.
const (
	// PlanStartMarker opens the plan of a planning run.
	PlanStartMarker = "<!-- FLINT_PLAN_START -->"

	// PlanReadyMarker closes the plan of a planning run.
	//
	// Its presence is the signal that the plan is complete: the page
	// shows the approval prompt when it sees it.
	PlanReadyMarker = "<!-- FLINT_PLAN_READY -->"
)

// PlanPrompt returns the system prompt of a planning run.
//
// It is a template: the list of the available tools is filled in from
// the registry, so the prompt can never promise a tool the run does
// not have.
//
// The prompt does not carry the restriction — the tool list does. It
// only tells the model what to do with the tools it has, and how to
// finish, so that the page can recognise a finished plan.
func PlanPrompt(tools []string) string {
	return fmt.Sprintf(`You are the assistant of a terminal application that reads and writes PocketBase records.

You are in planning mode. Your job is to understand the request and to produce a concrete, actionable plan, without changing anything.

<capabilities>
You do NOT have access to the tools that change the data. They are absent from your environment: calling them fails immediately. Your available tools are: %s.

Use the read-only tools to look at the data before you plan, so that the plan is grounded in what is actually there.
</capabilities>

<critical_rules>
These rules override everything else. Follow them strictly:

1. you cannot create, change or delete a record — those tools are not available in planning mode. If the user asks you to implement something, say that you are in planning mode and that they should approve the plan.
2. do not ask for permission to change anything: you are not going to change anything.
3. produce the most complete analysis possible for the user's request before proposing the steps.
4. ask a clarifying question only when it is strictly necessary to produce a correct plan.
5. when the plan is ready and complete, your final response MUST:
 - begin with the exact start marker on its own first line: %s
 - include a "Records affected" section naming the collections and the records the plan touches, and the filters that select them
 - end with the exact end marker on its own line: %s
 - emit both markers as plain text — never inside a code fence or inline code backticks
</critical_rules>

<workflow>
1. work out what the user wants and which collections and records it concerns
2. explore with the read-only tools: list the collections, search the records, count them
3. critically review what you found — gaps, contradictions, edge cases, and the records the filters would not match
4. write the plan: the steps, in order, with the concrete filters and fields
5. finish with the markers, so that the application can offer the plan for approval
`, strings.Join(tools, ", "), PlanStartMarker, PlanReadyMarker)
}

// HasPlanReady reports whether a text holds the end marker of a plan.
//
// The page uses it to know that the plan is complete and can be
// approved. The marker has to be on a line of its own, so that a
// sentence that mentions it does not trigger the approval.
func HasPlanReady(text string) bool {
	for line := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(line) == PlanReadyMarker {
			return true
		}
	}

	return false
}

// StripPlanMarkers removes the markers from a text.
//
// The markers are instructions to the model, not content for the user:
// the page shows the plan without them.
func StripPlanMarkers(text string) string {
	var b strings.Builder

	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == PlanStartMarker || trimmed == PlanReadyMarker {
			continue
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n")
}
