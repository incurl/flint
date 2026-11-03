package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"charm.land/fantasy"
)

// Event is what a run publishes while it works.
//
// The events are the only way the UI learns about the run: the agent
// does not draw anything, and the page does not poll it. They are
// plain values, so they can be sent through the Bubble Tea message
// channel without any locking on the page side.
type Event struct {
	// RunID identifies the run the event belongs to.
	//
	// A page that shows several runs at once uses it to discard the
	// events of the runs it is no longer interested in.
	RunID string
	// Kind tells what happened.
	Kind EventKind
	// Text holds the text of a text event, or the summary of a tool
	// call.
	Text string
	// ToolName holds the name of the tool a tool event refers to.
	ToolName string
	// ToolInput holds the arguments of a tool call.
	ToolInput string
	// Err holds the error of a failed event.
	Err error
}

// EventKind identifies the type of an [Event].
type EventKind string

const (
	// EventStarted is published when a run begins.
	EventStarted EventKind = "started"
	// EventTextDelta is published for every chunk of streamed text.
	EventTextDelta EventKind = "text_delta"
	// EventTextDone is published when the assistant finished a block of
	// text.
	EventTextDone EventKind = "text_done"
	// EventToolCall is published when the model calls a tool.
	EventToolCall EventKind = "tool_call"
	// EventToolResult is published when a tool returned.
	EventToolResult EventKind = "tool_result"
	// EventFinished is published when a run ends, successfully or not.
	EventFinished EventKind = "finished"
)

// ErrRunCancelled is returned when a run is cancelled by the user.
var ErrRunCancelled = errors.New("agent: run cancelled")

// Publisher receives the events of a run.
//
// The implementation must be safe for concurrent use: the events are
// published from the goroutine that runs the model.
type Publisher interface {
	Publish(event Event)
}

// PublisherFunc adapts a function to the [Publisher] interface.
type PublisherFunc func(event Event)

// Publish implements [Publisher].
func (f PublisherFunc) Publish(event Event) { f(event) }

// RunRequest describes a single turn.
type RunRequest struct {
	// RunID identifies the run.
	//
	// It is supplied by the caller so that a page can correlate the
	// events with the run it started, and discard the events of a run
	// it has left.
	RunID string
	// Prompt is the user request.
	Prompt string
	// History holds the messages of the previous turns.
	History []fantasy.Message
}

// Options configures a [Runner].
type Options struct {
	// Model is the language model that drives the run.
	//
	// It is required: a run without a model is an error, not an empty
	// turn.
	Model fantasy.LanguageModel
	// Registry holds the tools the model may call.
	Registry *Registry
	// SystemPrompt is the instruction given to the model.
	SystemPrompt string
	// Publisher receives the events of the run.
	//
	// It may be nil, in which case the events are discarded.
	Publisher Publisher
	// MaxSteps bounds the number of model calls of a single run.
	//
	// A run without a bound can loop forever: a model that keeps
	// calling a tool that returns the same result will never stop on
	// its own. Zero uses [DefaultMaxSteps].
	MaxSteps int
	// Timeout bounds the wall-clock duration of a run.
	//
	// Zero uses [DefaultRunTimeout].
	Timeout time.Duration
}

// DefaultMaxSteps is the number of model calls a run makes when the
// options do not say otherwise.
const DefaultMaxSteps = 12

// DefaultRunTimeout is the wall-clock duration a run is allowed to take
// when the options do not say otherwise.
const DefaultRunTimeout = 5 * time.Minute

// Runner runs a turn against a model.
//
// The runner owns the lifecycle of a run and nothing else: the loop
// between the model and the tools belongs to the model library, and the
// runner adds the parts that are specific to the template — the events
// the UI consumes, the bound on the number of steps, and the
// cancellation.
type Runner struct {
	// options are the settings of the runner.
	options Options
	// mu guards the active runs.
	mu sync.Mutex
	// active holds the cancel function of the running runs, keyed by
	// run id.
	active map[string]context.CancelFunc
}

// NewRunner returns a runner with the given options.
func NewRunner(options Options) *Runner {
	if options.Registry == nil {
		options.Registry = NewRegistry()
	}

	if options.MaxSteps <= 0 {
		options.MaxSteps = DefaultMaxSteps
	}

	if options.Timeout <= 0 {
		options.Timeout = DefaultRunTimeout
	}

	return &Runner{
		options: options,
		active:  map[string]context.CancelFunc{},
	}
}

// Cancel stops the run with the given id.
//
// It returns false when no run with that id is active, so that the
// caller can tell a cancellation from a no-op.
func (r *Runner) Cancel(runID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	cancel, ok := r.active[runID]
	if !ok {
		return false
	}

	cancel()

	return true
}

// CancelAll stops every active run.
func (r *Runner) CancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, cancel := range r.active {
		cancel()
	}
}

// Busy reports whether a run is active.
func (r *Runner) Busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.active) > 0
}

// publish sends an event to the publisher, if there is one.
func (r *Runner) publish(event Event) {
	if r.options.Publisher == nil {
		return
	}

	r.options.Publisher.Publish(event)
}

// Run executes a turn and returns the final text of the assistant.
//
// It blocks for the duration of the run, so it must be called from a
// command and never from the update loop.
func (r *Runner) Run(ctx context.Context, request RunRequest) (string, error) {
	if r.options.Model == nil {
		return "", ErrNoModel
	}

	if request.Prompt == "" {
		return "", fmt.Errorf("agent: empty prompt")
	}

	// The run context carries the timeout and the cancellation, so that
	// both stop the model call and the tools.
	runCtx, cancel := context.WithTimeout(ctx, r.options.Timeout)
	defer cancel()

	r.mu.Lock()
	r.active[request.RunID] = cancel
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.active, request.RunID)
		r.mu.Unlock()
	}()

	r.publish(Event{RunID: request.RunID, Kind: EventStarted})

	opts := []fantasy.AgentOption{
		fantasy.WithTools(r.options.Registry.AgentTools()...),
	}

	if r.options.SystemPrompt != "" {
		opts = append(opts, fantasy.WithSystemPrompt(r.options.SystemPrompt))
	}

	// The step bound is what stops a model that keeps calling a tool
	// in a loop.
	opts = append(opts, fantasy.WithStopConditions(func(steps []fantasy.StepResult) bool {
		return len(steps) >= r.options.MaxSteps
	}))

	agent := fantasy.NewAgent(r.options.Model, opts...)

	var text string

	result, err := agent.Stream(runCtx, fantasy.AgentStreamCall{
		Prompt:   request.Prompt,
		Messages: request.History,
		OnTextDelta: func(id, delta string) error {
			text += delta
			r.publish(Event{RunID: request.RunID, Kind: EventTextDelta, Text: delta})

			return nil
		},
		OnToolCall: func(call fantasy.ToolCallContent) error {
			r.publish(Event{
				RunID:     request.RunID,
				Kind:      EventToolCall,
				ToolName:  call.ToolName,
				ToolInput: call.Input,
			})

			return nil
		},
	})
	if err != nil {
		if runCtx.Err() != nil {
			r.publish(Event{RunID: request.RunID, Kind: EventFinished, Err: ErrRunCancelled})

			return text, fmt.Errorf("%w: %w", ErrRunCancelled, err)
		}

		r.publish(Event{RunID: request.RunID, Kind: EventFinished, Err: err})

		return text, err
	}

	r.publish(Event{RunID: request.RunID, Kind: EventFinished})

	return resultText(result, text), nil
}

// resultText returns the text of the final response, falling back to
// the streamed text when the response carries none.
func resultText(result *fantasy.AgentResult, streamed string) string {
	if result == nil {
		return streamed
	}

	for _, part := range result.Response.Content {
		if text, ok := part.(fantasy.TextContent); ok && text.Text != "" {
			return text.Text
		}
	}

	return streamed
}
