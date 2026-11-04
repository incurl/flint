package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"

	"charm.land/fantasy"
)

// The bounds of the loop detection.
//
// The window is how many recent steps are looked at, and the repeats
// are how many times the same call may appear in it before the run is
// stopped.
const (
	// loopWindowSize is the number of recent steps the detection looks
	// at.
	loopWindowSize = 10

	// loopMaxRepeats is how many times the same tool call may appear
	// in the window before the run is stopped.
	loopMaxRepeats = 5
)

// ErrLoopDetected is returned when the model gets stuck.
//
// It is not a failure of the model or of the tools: it is the agent
// repeating the same call with the same arguments and getting the same
// answer, over and over. Continuing would burn the token budget of the
// user for nothing.
var ErrLoopDetected = errLoopDetected{}

// errLoopDetected is the error of a stuck run.
//
// It is a type and not a sentinel variable so that the message can
// carry the name of the call that was repeated.
type errLoopDetected struct{}

// Error implements the error interface.
func (errLoopDetected) Error() string {
	return "agent: the model repeated the same call and stopped making progress"
}

// IsLoopDetected reports whether an error is the loop detection.
//
// It is the check a caller makes to tell a stuck run from a failed
// one: a stuck run produced a valid answer, and the agent can report
// what it was doing.
//
// The error is looked for with [errors.As], so a wrapped one is found
// too: the run wraps the errors it returns.
func IsLoopDetected(err error) bool {
	var detected errLoopDetected

	return errors.As(err, &detected)
}

// hasLoop reports whether the recent steps repeat the same call.
//
// It looks at the last [loopWindowSize] steps and counts the
// signatures. A signature is the hash of a call and its result, so two
// calls are the same when they do the same thing and get the same
// answer.
//
// A step without a tool call has no signature and is skipped: a model
// that only writes text is not stuck.
func hasLoop(steps []fantasy.StepResult) bool {
	if len(steps) < loopWindowSize {
		return false
	}

	window := steps[len(steps)-loopWindowSize:]
	counts := make(map[string]int, len(window))

	for _, step := range window {
		signature := stepSignature(step)

		if signature == "" {
			continue
		}

		counts[signature]++
		if counts[signature] > loopMaxRepeats {
			return true
		}
	}

	return false
}

// stepSignature returns the signature of the tool calls of a step.
//
// It pairs every call with its result, so that a model that calls the
// same tool with the same arguments but gets a *different* answer is
// not counted as stuck: it is making progress.
func stepSignature(step fantasy.StepResult) string {
	calls := step.Content.ToolCalls()
	if len(calls) == 0 {
		return ""
	}

	results := make(map[string]fantasy.ToolResultContent, len(calls))
	for _, result := range step.Content.ToolResults() {
		results[result.ToolCallID] = result
	}

	hash := sha256.New()

	for _, call := range calls {
		output := ""

		if result, ok := results[call.ToolCallID]; ok {
			output = toolResultText(result.Result)
		}

		_, _ = io.WriteString(hash, call.ToolName)
		_, _ = io.WriteString(hash, "\x00")
		_, _ = io.WriteString(hash, call.Input)
		_, _ = io.WriteString(hash, "\x00")
		_, _ = io.WriteString(hash, output)
		_, _ = io.WriteString(hash, "\x00")
	}

	return hex.EncodeToString(hash.Sum(nil))
}

// toolResultText returns a stable text for the output of a tool.
//
// The output can be a text, an error or a blob of media, and the
// signature only has to tell two outputs apart.
func toolResultText(result fantasy.ToolResultOutputContent) string {
	if result == nil {
		return ""
	}

	if text, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result); ok {
		return text.Text
	}

	if failure, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result); ok {
		if failure.Error != nil {
			return failure.Error.Error()
		}

		return ""
	}

	if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result); ok {
		return media.Data
	}

	return ""
}
