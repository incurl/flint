package agent_test

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/incurl/flint/internal/agent"
)

// TestPlanModeWithholdsTheMutatingTools verifies that the planning
// mode takes the tools that change the data out of the tool list.
//
// This is the point of the mode: the model does not see them, so it
// cannot call them. A prompt that asked the model not to write would
// be a request, and a request can be ignored.
func TestPlanModeWithholdsTheMutatingTools(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register(&recordingTool{name: agent.ToolListRecords})
	registry.Register(&recordingTool{name: agent.ToolGetRecord})
	registry.Register(&recordingTool{name: agent.ToolCreateRecord})
	registry.Register(&recordingTool{name: agent.ToolUpdateRecord})
	registry.Register(&recordingTool{name: agent.ToolDeleteRecord})

	plan := toolNames(registry.AgentToolsFor(agent.ModePlan))
	act := toolNames(registry.AgentToolsFor(agent.ModeAct))

	for _, name := range []string{agent.ToolCreateRecord, agent.ToolUpdateRecord, agent.ToolDeleteRecord} {
		if contains(plan, name) {
			t.Errorf("the planning mode offers %q", name)
		}

		if !contains(act, name) {
			t.Errorf("the acting mode does not offer %q", name)
		}
	}

	for _, name := range []string{agent.ToolListRecords, agent.ToolGetRecord} {
		if !contains(plan, name) {
			t.Errorf("the planning mode does not offer %q", name)
		}
	}
}

// TestPlanModeWithholdsAnUnknownTool verifies that a tool the template
// does not know is withheld from the planning mode.
//
// A downstream project that adds a tool is safe by default: the tool
// is withheld until it is classified as read-only.
func TestPlanModeWithholdsAnUnknownTool(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register(&recordingTool{name: "export_everything"})

	if contains(toolNames(registry.AgentToolsFor(agent.ModePlan)), "export_everything") {
		t.Error("the planning mode offers a tool it does not know")
	}
}

// TestPlanModeWithholdsTheToolsOfTheTemplate verifies the same thing
// for the tools the template ships.
func TestPlanModeWithholdsTheToolsOfTheTemplate(t *testing.T) {
	registry := agent.NewRegistry()

	for _, name := range []string{
		agent.ToolListRecords,
		agent.ToolGetRecord,
		agent.ToolSearchRecords,
		agent.ToolCountRecords,
		agent.ToolListCollections,
	} {
		registry.Register(&recordingTool{name: name})
	}

	for _, name := range []string{
		agent.ToolCreateRecord,
		agent.ToolUpdateRecord,
		agent.ToolDeleteRecord,
	} {
		registry.Register(&recordingTool{name: name})
	}

	plan := registry.ToolNamesFor(agent.ModePlan)

	if len(plan) != 5 {
		t.Errorf("the planning mode offers %d tools, want 5: %v", len(plan), plan)
	}

	for _, name := range plan {
		if agent.IsMutatingTool(name) {
			t.Errorf("the planning mode offers the mutating tool %q", name)
		}
	}
}

// TestEveryTemplateToolIsClassified verifies that every tool of the
// template is classified as read-only or as mutating.
//
// The classification lives in two lists, and a tool that is in
// neither is withheld from the planning mode. A read-only tool that
// was forgotten is withheld as well: the plan cannot use it, and the
// plan is exactly what needs it.
//
// The unknown tool stays withheld -- that is the safe default for a
// downstream project -- but the tools of the template have to be
// classified.
func TestEveryTemplateToolIsClassified(t *testing.T) {
	registry := agent.NewRegistry()

	for _, name := range templateToolNames {
		registry.Register(&recordingTool{name: name})
	}

	plan := registry.ToolNamesFor(agent.ModePlan)
	act := registry.ToolNamesFor(agent.ModeAct)

	if len(act) != len(templateToolNames) {
		t.Errorf("the acting mode offers %d tools, want %d", len(act), len(templateToolNames))
	}

	for _, name := range templateToolNames {
		mutating := agent.IsMutatingTool(name)

		inPlan := contains(plan, name)
		inAct := contains(act, name)

		if !inAct {
			t.Errorf("the acting mode does not offer %q", name)
		}

		if mutating && inPlan {
			t.Errorf("the planning mode offers the mutating tool %q", name)
		}

		if !mutating && !inPlan {
			t.Errorf("the read-only tool %q is withheld from the planning mode", name)
		}
	}
}

// templateToolNames are the tools the template registers.
//
// The list is the one of the registration functions, so that a tool
// that is added is classified in the same place.
var templateToolNames = []string{
	agent.ToolListRecords,
	agent.ToolGetRecord,
	agent.ToolSearchRecords,
	agent.ToolCountRecords,
	agent.ToolListCollections,
	agent.ToolCollectionSchema,
	agent.ToolCreateRecord,
	agent.ToolUpdateRecord,
	agent.ToolDeleteRecord,
	agent.ToolCreateCollection,
	agent.ToolTodos,
	agent.ToolCodeSymbols,
	agent.ToolCodeDefinition,
	agent.ToolCodeReferences,
	agent.ToolCodeDiagnostics,
	agent.ToolCodeCallHierarchy,
	agent.ToolCodeSearch,
	agent.ToolCodeFindReferences,
	agent.ToolCodeRename,
	agent.ToolCodeReplaceSymbol,
	agent.ToolCodeCreateFile,
	agent.ToolCodeMoveFile,
	agent.ToolCodeDeleteFile,
}

// TestPlanPromptListsTheToolsItHas verifies that the planning prompt
// advertises the tools the run actually has.
//
// The list comes from the registry, so the prompt can never promise a
// tool the model cannot call.
func TestPlanPromptListsTheToolsItHas(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register(&recordingTool{name: agent.ToolListRecords})
	registry.Register(&recordingTool{name: agent.ToolCreateRecord})

	prompt := agent.PlanPrompt(registry.ToolNamesFor(agent.ModePlan))

	if !strings.Contains(prompt, agent.ToolListRecords) {
		t.Errorf("the prompt does not list the read-only tool:\n%s", prompt)
	}

	if strings.Contains(prompt, agent.ToolCreateRecord) {
		t.Errorf("the prompt lists a tool the run does not have:\n%s", prompt)
	}
}

// TestPlanPromptNamesTheMarkers verifies that the prompt tells the
// model how to finish a plan.
//
// The markers are how the page recognises a finished plan, so the
// prompt has to name them exactly.
func TestPlanPromptNamesTheMarkers(t *testing.T) {
	prompt := agent.PlanPrompt([]string{agent.ToolListRecords})

	if !strings.Contains(prompt, agent.PlanStartMarker) {
		t.Error("the prompt does not name the start marker")
	}

	if !strings.Contains(prompt, agent.PlanReadyMarker) {
		t.Error("the prompt does not name the end marker")
	}
}

// TestPlanModeCannotChangeTheData verifies the restriction end to end.
//
// A model that asks to create a record in the planning mode gets a
// "tool not found" error, and the record is not created.
func TestPlanModeCannotChangeTheData(t *testing.T) {
	created := &recordingTool{name: agent.ToolCreateRecord, out: "created"}

	registry := agent.NewRegistry()
	registry.Register(&recordingTool{name: agent.ToolListRecords})
	registry.Register(created)

	model := &fakeModel{turns: [][]fantasy.StreamPart{
		toolTurn(agent.ToolCreateRecord, `{"collection":"notes"}`),
		textTurn("I cannot create records in planning mode."),
	}}

	runner := agent.NewRunner(agent.Options{
		Model:    model,
		Registry: registry,
	})
	runner.SetMode(agent.ModePlan)

	_, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "add a note"})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(created.calls) != 0 {
		t.Errorf("the tool was called %d times in the planning mode", len(created.calls))
	}
}

// TestActModeCanChangeTheData verifies the other half: the acting mode
// still has the tools.
//
// A planning mode that withheld everything would pass the test above
// without being useful.
func TestActModeCanChangeTheData(t *testing.T) {
	created := &recordingTool{name: agent.ToolCreateRecord, out: "created"}

	registry := agent.NewRegistry()
	registry.Register(created)

	model := &fakeModel{turns: [][]fantasy.StreamPart{
		toolTurn(agent.ToolCreateRecord, `{"collection":"notes"}`),
		textTurn("Done."),
	}}

	runner := agent.NewRunner(agent.Options{
		Model:    model,
		Registry: registry,
	})

	if _, err := runner.Run(context.Background(), agent.RunRequest{RunID: "r1", Prompt: "add a note"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(created.calls) != 1 {
		t.Errorf("the tool was called %d times, want 1", len(created.calls))
	}
}

// TestHasPlanReady verifies the recognition of a finished plan.
//
// The marker has to be on a line of its own: a sentence that mentions
// it does not finish a plan.
func TestHasPlanReady(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"empty", "", false},
		{"the marker alone", agent.PlanReadyMarker, true},
		{"the marker after the plan", "# Plan\n\nSteps.\n\n" + agent.PlanReadyMarker, true},
		{"the marker with spaces", "  " + agent.PlanReadyMarker + "  ", true},
		{"the marker in a sentence", "the marker is " + agent.PlanReadyMarker + " here", false},
		{"no marker", "# Plan\n\nSteps.", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := agent.HasPlanReady(test.text); got != test.want {
				t.Errorf("HasPlanReady() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestStripPlanMarkers verifies that the markers are removed from the
// plan the user reads.
func TestStripPlanMarkers(t *testing.T) {
	text := agent.PlanStartMarker + "\n# Plan\n\nDo the thing.\n" + agent.PlanReadyMarker

	got := agent.StripPlanMarkers(text)

	if strings.Contains(got, agent.PlanStartMarker) {
		t.Errorf("the start marker is still there:\n%s", got)
	}

	if strings.Contains(got, agent.PlanReadyMarker) {
		t.Errorf("the end marker is still there:\n%s", got)
	}

	if !strings.Contains(got, "# Plan") || !strings.Contains(got, "Do the thing.") {
		t.Errorf("the plan was lost:\n%s", got)
	}
}

// TestModeString verifies the names of the modes.
func TestModeString(t *testing.T) {
	if agent.ModeAct.String() != "act" {
		t.Errorf("ModeAct = %q, want act", agent.ModeAct.String())
	}

	if agent.ModePlan.String() != "plan" {
		t.Errorf("ModePlan = %q, want plan", agent.ModePlan.String())
	}

	if !agent.ModePlan.IsPlan() {
		t.Error("ModePlan.IsPlan() is false")
	}

	if agent.ModeAct.IsPlan() {
		t.Error("ModeAct.IsPlan() is true")
	}
}

// toolNames returns the names of a list of tools.
func toolNames(tools []fantasy.AgentTool) []string {
	out := make([]string, 0, len(tools))

	for _, tool := range tools {
		out = append(out, tool.Info().Name)
	}

	return out
}

// contains reports whether a list holds a value.
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}
