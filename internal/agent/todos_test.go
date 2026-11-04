package agent_test

import (
	"context"
	"strings"
	"testing"

	"github.com/incurl/flint/internal/agent"
)

// TestTodosToolStoresTheList verifies that the tool keeps the list the
// model sent.
func TestTodosToolStoresTheList(t *testing.T) {
	registry := agent.NewRegistry()
	runner := agent.NewRunner(agent.Options{Registry: registry})

	agent.RegisterTodosTool(registry, runner)

	tool, err := registry.Get(agent.ToolTodos)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	ctx := agent.WithSessionID(context.Background(), "s1")

	input := `{"todos":[
		{"content":"Find the records with no label","status":"in_progress","active_form":"Finding the records"},
		{"content":"Propose a label for each one","status":"pending","active_form":""}
	]}`

	out, err := tool.Run(ctx, input)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out, "Find the records with no label") {
		t.Errorf("the output does not hold the list:\n%s", out)
	}

	todos := runner.Todos("s1")
	if len(todos) != 2 {
		t.Fatalf("the list has %d tasks, want 2", len(todos))
	}

	if todos[0].Status != agent.TodoInProgress {
		t.Errorf("the first task is %q, want in_progress", todos[0].Status)
	}
}

// TestTodosToolIsPerConversation verifies that two conversations do
// not share a list.
func TestTodosToolIsPerConversation(t *testing.T) {
	registry := agent.NewRegistry()
	runner := agent.NewRunner(agent.Options{Registry: registry})

	agent.RegisterTodosTool(registry, runner)

	tool, _ := registry.Get(agent.ToolTodos)

	input := `{"todos":[{"content":"Do it","status":"pending"}]}`

	if _, err := tool.Run(agent.WithSessionID(context.Background(), "s1"), input); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(runner.Todos("s1")) != 1 {
		t.Error("the first conversation has no list")
	}

	if len(runner.Todos("s2")) != 0 {
		t.Error("the second conversation has the list of the first one")
	}
}

// TestTodosToolNeedsAConversation verifies that the tool refuses to
// work without a conversation.
//
// A list that belongs to nobody is a list that is lost.
func TestTodosToolNeedsAConversation(t *testing.T) {
	registry := agent.NewRegistry()
	runner := agent.NewRunner(agent.Options{Registry: registry})

	agent.RegisterTodosTool(registry, runner)

	tool, _ := registry.Get(agent.ToolTodos)

	_, err := tool.Run(context.Background(), `{"todos":[{"content":"Do it","status":"pending"}]}`)
	if err == nil {
		t.Error("the tool accepted a call without a conversation")
	}
}

// TestTodosToolNormalisesTheList verifies that the list the model sent
// is cleaned up.
//
// A task without a status is pending, and a task without an active form
// gets one: the list is always renderable.
func TestTodosToolNormalisesTheList(t *testing.T) {
	registry := agent.NewRegistry()
	runner := agent.NewRunner(agent.Options{Registry: registry})

	agent.RegisterTodosTool(registry, runner)

	tool, _ := registry.Get(agent.ToolTodos)

	ctx := agent.WithSessionID(context.Background(), "s1")

	input := `{"todos":[
		{"content":"Run the tests","status":"nonsense"},
		{"content":"","status":"pending"},
		{"content":"Check the labels","status":"pending"}
	]}`

	if _, err := tool.Run(ctx, input); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	todos := runner.Todos("s1")
	if len(todos) != 2 {
		t.Fatalf("the list has %d tasks, want 2: %+v", len(todos), todos)
	}

	if todos[0].Status != agent.TodoPending {
		t.Errorf("the status is %q, want pending", todos[0].Status)
	}

	if todos[0].ActiveForm != "Running the tests" {
		t.Errorf("the active form is %q, want 'Running the tests'", todos[0].ActiveForm)
	}
}

// TestTodosToolRejectsABadInput verifies that a malformed argument is
// an error the model can correct.
func TestTodosToolRejectsABadInput(t *testing.T) {
	registry := agent.NewRegistry()
	runner := agent.NewRunner(agent.Options{Registry: registry})

	agent.RegisterTodosTool(registry, runner)

	tool, _ := registry.Get(agent.ToolTodos)

	ctx := agent.WithSessionID(context.Background(), "s1")

	if _, err := tool.Run(ctx, "not json"); err == nil {
		t.Error("the tool accepted a malformed input")
	}
}

// TestTodosToolRunsInAPlan verifies that the task list is available in
// the planning mode.
//
// A plan is a list of steps: the tool that holds it is one of the
// read-only ones.
func TestTodosToolRunsInAPlan(t *testing.T) {
	registry := agent.NewRegistry()
	runner := agent.NewRunner(agent.Options{Registry: registry})

	agent.RegisterTodosTool(registry, runner)

	names := registry.ToolNamesFor(agent.ModePlan)

	for _, name := range names {
		if name == agent.ToolTodos {
			return
		}
	}

	t.Errorf("the planning mode does not offer the task list: %v", names)
}

// TestTodosToolIsNotGated verifies that the task list is offered in the
// acting mode too.
//
// It changes nothing of the data, so it does not need the permission
// gate.
func TestTodosToolIsNotGated(t *testing.T) {
	registry := agent.NewRegistry()
	runner := agent.NewRunner(agent.Options{Registry: registry})

	agent.RegisterTodosTool(registry, runner)

	names := registry.ToolNamesFor(agent.ModeAct)

	for _, name := range names {
		if name == agent.ToolTodos {
			return
		}
	}

	t.Errorf("the acting mode does not offer the task list: %v", names)
}

// TestSortTodos verifies the order the list is shown in.
//
// The finished ones go first, then the one in progress, then the ones
// that are waiting.
func TestSortTodos(t *testing.T) {
	todos := []agent.Todo{
		{Content: "pending", Status: agent.TodoPending},
		{Content: "done", Status: agent.TodoCompleted},
		{Content: "running", Status: agent.TodoInProgress},
	}

	sorted := agent.SortTodos(todos)

	if sorted[0].Content != "done" {
		t.Errorf("the first task is %q, want done", sorted[0].Content)
	}

	if sorted[1].Content != "running" {
		t.Errorf("the second task is %q, want running", sorted[1].Content)
	}

	if sorted[2].Content != "pending" {
		t.Errorf("the third task is %q, want pending", sorted[2].Content)
	}

	// The original is not touched.
	if todos[0].Content != "pending" {
		t.Error("the list was sorted in place")
	}
}

// TestTodoProgress verifies the counters of the progress line.
func TestTodoProgress(t *testing.T) {
	todos := []agent.Todo{
		{Status: agent.TodoCompleted},
		{Status: agent.TodoCompleted},
		{Status: agent.TodoInProgress},
		{Status: agent.TodoPending},
	}

	completed, total := agent.TodoProgress(todos)

	if completed != 2 {
		t.Errorf("completed = %d, want 2", completed)
	}

	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
}

// TestSessionIDInTheContext verifies the plumbing of the session id.
//
// The tool needs to know which conversation it belongs to, and the
// signature of a tool does not carry it.
func TestSessionIDInTheContext(t *testing.T) {
	ctx := agent.WithSessionID(context.Background(), "s1")

	if got := agent.SessionIDFrom(ctx); got != "s1" {
		t.Errorf("SessionIDFrom() = %q, want s1", got)
	}

	// A context without one carries none.
	if got := agent.SessionIDFrom(context.Background()); got != "" {
		t.Errorf("SessionIDFrom() = %q, want an empty string", got)
	}
}

// TestTheTodosToolIsWrappedForTheModel verifies that the tool can be
// used by the model library.
func TestTheTodosToolIsWrappedForTheModel(t *testing.T) {
	registry := agent.NewRegistry()
	runner := agent.NewRunner(agent.Options{Registry: registry})

	agent.RegisterTodosTool(registry, runner)

	tools := registry.AgentToolsFor(agent.ModeAct)

	for _, tool := range tools {
		if tool.Info().Name == agent.ToolTodos {
			return
		}
	}

	t.Error("the task list is not in the tool list of the model")
}

// TestTheTodosToolHasASchema verifies that the model knows what to
// send.
//
// The schema is written by hand here, because the tool is not a store
// tool: the arguments are a list, not a record.
func TestTheTodosToolHasASchema(t *testing.T) {
	registry := agent.NewRegistry()
	runner := agent.NewRunner(agent.Options{Registry: registry})

	agent.RegisterTodosTool(registry, runner)

	tool, _ := registry.Get(agent.ToolTodos)

	schemer, ok := tool.(interface {
		Schema() (map[string]any, []string)
	})
	if !ok {
		t.Fatal("the tool does not declare a schema")
	}

	properties, required := schemer.Schema()

	if _, exists := properties["todos"]; !exists {
		t.Errorf("the schema has no 'todos' property: %v", properties)
	}

	if len(required) != 1 || required[0] != "todos" {
		t.Errorf("the required arguments are %v, want [todos]", required)
	}
}
