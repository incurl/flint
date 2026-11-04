package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// ToolTodos is the name of the task list tool.
//
// It is the identifier the model uses to call it, so it is part of the
// contract with the model.
const ToolTodos = "todos"

// The task statuses.
const (
	// TodoPending is a task that has not been started.
	TodoPending = "pending"
	// TodoInProgress is the task that is being worked on.
	TodoInProgress = "in_progress"
	// TodoCompleted is a task that is done.
	TodoCompleted = "completed"
)

// sessionIDKey is the key of the session id in the context.
//
// It is a private type, so that no other package can collide with it.
type sessionIDKey struct{}

// WithSessionID returns a context that carries the session id.
//
// The tools that belong to a conversation need to know which one, and
// the signature of a tool does not carry it: the context is the
// natural place for it.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	if sessionID == "" {
		return ctx
	}

	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}

// SessionIDFrom returns the session id a context carries.
//
// It is empty when the context carries none, which happens when the
// conversation is not persisted.
func SessionIDFrom(ctx context.Context) string {
	sessionID, _ := ctx.Value(sessionIDKey{}).(string)

	return sessionID
}

// Todo is one task of the list.
//
// The active form is the same task written in the present continuous —
// "Running the tests" rather than "Run the tests" — because it is the
// one shown while the task is in progress.
type Todo struct {
	// Content is what the task is, in the imperative.
	Content string `json:"content" description:"What needs to be done, in the imperative form"`
	// Status is the state of the task: "pending", "in_progress" or
	// "completed".
	Status string `json:"status" description:"The state of the task: pending, in_progress or completed"`
	// ActiveForm is the task in the present continuous.
	ActiveForm string `json:"active_form" description:"The task in the present continuous, e.g. 'Running the tests'"`
}

// todosParams are the arguments of the tool.
//
// The tool replaces the whole list: the model sends the list it wants,
// and the tool stores it. It is simpler than a diff, and the model is
// good at it.
type todosParams struct {
	// Todos is the new list of the tasks.
	Todos []Todo `json:"todos" description:"The updated list of the tasks"`
}

// The description the model reads.
//
// It says when *not* to use the tool: a task list for a single lookup
// is noise, and the model that writes one for everything stops using
// it.
const todosDescription = `Manage a structured task list for multi-step work. Each task has a pending, in_progress or completed state. Keep exactly one task in_progress at a time. Skip this tool for simple or single-step tasks.`

// todoStore holds the task list of the conversations.
//
// It is in memory and not in the database: the task list is a way for
// the model to organise a turn, not something the user needs to keep.
// A conversation that is not persisted has no task list either.
type todoStore struct {
	// mu guards the lists.
	mu sync.Mutex
	// lists holds the tasks of the sessions, keyed by session id.
	lists map[string][]Todo
}

// newTodoStore returns an empty store.
func newTodoStore() *todoStore {
	return &todoStore{lists: map[string][]Todo{}}
}

// set replaces the list of a session.
func (s *todoStore) set(sessionID string, todos []Todo) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lists[sessionID] = todos
}

// get returns the list of a session.
func (s *todoStore) get(sessionID string) []Todo {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.lists[sessionID]
}

// NewTodosTool returns the tool that manages the task list.
//
// The list belongs to the conversation of the run, which the tool
// finds in the context.
//
// The tool is not gated: it changes nothing of the data. It only
// changes what the model is doing, and the user sees it.
func NewTodosTool(store *todoStore) Tool {
	return &todosTool{store: store}
}

// todosTool is the task list tool.
type todosTool struct {
	// store holds the lists.
	store *todoStore
}

// Name implements [Tool].
func (t *todosTool) Name() string { return ToolTodos }

// Description implements [Tool].
func (t *todosTool) Description() string { return todosDescription }

// Schema implements [Schemer].
func (t *todosTool) Schema() (map[string]any, []string) {
	properties := map[string]any{
		"todos": map[string]any{
			"type":        "array",
			"description": "The updated list of the tasks.",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "What needs to be done, in the imperative form.",
					},
					"status": map[string]any{
						"type":        "string",
						"enum":        []string{TodoPending, TodoInProgress, TodoCompleted},
						"description": "The state of the task: pending, in_progress or completed.",
					},
					"active_form": map[string]any{
						"type":        "string",
						"description": "The task in the present continuous, e.g. 'Running the tests'.",
					},
				},
				"required": []string{"content", "status"},
			},
		},
	}

	return properties, []string{"todos"}
}

// Run implements [Tool].
func (t *todosTool) Run(ctx context.Context, input string) (string, error) {
	var params todosParams

	if input != "" {
		if err := json.Unmarshal([]byte(input), &params); err != nil {
			return "", fmt.Errorf("%w: %w", ErrInvalidToolInput, err)
		}
	}

	sessionID := SessionIDFrom(ctx)
	if sessionID == "" {
		return "", fmt.Errorf("agent: the task list needs a conversation")
	}

	todos := normalizeTodos(params.Todos)
	t.store.set(sessionID, todos)

	return formatTodos(todos), nil
}

// normalizeTodos cleans up the list the model sent.
//
// A task without a status is pending, and a task without an active form
// gets one derived from its content: the list is always renderable.
func normalizeTodos(todos []Todo) []Todo {
	out := make([]Todo, 0, len(todos))

	for _, todo := range todos {
		if todo.Content == "" {
			continue
		}

		switch todo.Status {
		case TodoPending, TodoInProgress, TodoCompleted:
		default:
			todo.Status = TodoPending
		}

		if todo.ActiveForm == "" {
			todo.ActiveForm = activeForm(todo.Content)
		}

		out = append(out, todo)
	}

	return out
}

// activeForm returns the present continuous of a task.
//
// It is a rough conversion: it turns the imperative into the form the
// list shows while the task runs. A model that wants it exact sends
// the active form itself.
func activeForm(content string) string {
	switch {
	case len(content) > 3 && content[:3] == "Run":
		return "Running" + content[3:]
	case len(content) > 3 && content[:3] == "Add":
		return "Adding" + content[3:]
	case len(content) > 5 && content[:5] == "Check":
		return "Checking" + content[5:]
	case len(content) > 7 && content[:7] == "Update ":
		return "Updating" + content[7:]
	case len(content) > 7 && content[:7] == "Delete ":
		return "Deleting" + content[7:]
	case len(content) > 7 && content[:7] == "Create ":
		return "Creating" + content[7:]
	default:
		return content
	}
}

// formatTodos returns the list as the model reads it.
//
// The model needs to know what is done and what is next, not a pretty
// list: the order is the order the model gave.
func formatTodos(todos []Todo) string {
	if len(todos) == 0 {
		return "The task list is empty."
	}

	completed := 0

	for _, todo := range todos {
		if todo.Status == TodoCompleted {
			completed++
		}
	}

	return fmt.Sprintf("The task list has %d tasks, %d completed:\n%s", len(todos), completed, renderTodos(todos))
}

// renderTodos returns the list as text.
func renderTodos(todos []Todo) string {
	out := ""

	for _, todo := range todos {
		out += fmt.Sprintf("- [%s] %s\n", todo.Status, todo.Content)
	}

	return out
}

// SortTodos returns the tasks in the order they are shown.
//
// The finished ones go first, then the one in progress, then the ones
// that are waiting. The order the model gave is kept inside a group.
func SortTodos(todos []Todo) []Todo {
	out := make([]Todo, len(todos))
	copy(out, todos)

	sort.SliceStable(out, func(i, j int) bool {
		return todoStatusOrder(out[i].Status) < todoStatusOrder(out[j].Status)
	})

	return out
}

// todoStatusOrder returns the position of a status in the display
// order.
func todoStatusOrder(status string) int {
	switch status {
	case TodoCompleted:
		return 0
	case TodoInProgress:
		return 1
	default:
		return 2
	}
}

// TodoProgress returns how many tasks are done and how many there are.
//
// It is what the progress line of the page shows.
func TodoProgress(todos []Todo) (completed, total int) {
	for _, todo := range todos {
		if todo.Status == TodoCompleted {
			completed++
		}
	}

	return completed, len(todos)
}
