package components

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/incurl/flint/internal/pbstore"
)

// ScriptTimeout bounds a single script evaluation.
//
// The script runs off the render loop, but it must not hang forever:
// a script that waits on something that never happens would leave the
// record without a title.
const ScriptTimeout = 2 * time.Second

// ScriptRenderer renders a record with a JavaScript script, falling back
// to a Go renderer.
//
// It **overrides** the Go renderer: when the script returns a value,
// that value is used. When the script is missing, fails or times out,
// the Go renderer renders the record instead.
//
// The script runs in its own JavaScript runtime, not in the PocketBase
// one: the two have different lifetimes and different globals, and the
// script must not be able to reach the database bindings.
//
// The evaluation is synchronous, so it must be called from a command
// and never from a render function. The [RecordRenderer] interface does
// not allow that, so the caching is the caller's responsibility: see
// the script renderer in the application package.
type ScriptRenderer struct {
	// fallback renders the record when the script does not.
	fallback RecordRenderer
	// script is the evaluated script.
	script string
	// timeout bounds a single evaluation.
	timeout time.Duration

	// mu guards the runtime: the script may be evaluated from several
	// goroutines, and a JavaScript runtime is not safe for concurrent
	// use.
	mu sync.Mutex
	// interruptMu guards the runtime pointer for the interrupt
	// goroutine, which reads it while the evaluation holds mu.
	interruptMu sync.Mutex
	// runtime is the JavaScript runtime.
	runtime *goja.Runtime
	// ready reports whether the runtime was initialised.
	ready bool
}

// NewScriptRenderer creates a renderer that evaluates the script.
//
// The fallback is used when the script is empty or fails.
func NewScriptRenderer(script string, fallback RecordRenderer) *ScriptRenderer {
	if fallback == nil {
		fallback = &DefaultRecordRenderer{}
	}

	return &ScriptRenderer{
		fallback: fallback,
		script:   script,
		timeout:  ScriptTimeout,
	}
}

// ErrNoScript is returned when there is no script to evaluate.
var ErrNoScript = errors.New("components: no script to evaluate")

// evaluate runs the script against the record and returns the rendered
// result.
//
// It returns [ErrNoScript] when there is no script, and an error when
// the evaluation fails.
func (r *ScriptRenderer) evaluate(rec *pbstore.Record) (ScriptResult, error) {
	if r.script == "" {
		return ScriptResult{}, ErrNoScript
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.ensureRuntime(); err != nil {
		return ScriptResult{}, err
	}

	// The record is bound on every call: the runtime is reused, so a
	// stale binding would render the first record for every record.
	if err := r.runtime.Set("record", recordToMap(rec)); err != nil {
		return ScriptResult{}, fmt.Errorf("components: bind the record: %w", err)
	}

	value, err := r.runtime.RunString("flintRender(record)")
	if err != nil {
		return ScriptResult{}, fmt.Errorf("components: run the script: %w", err)
	}

	return scriptResultFromValue(value)
}

// ensureRuntime creates the runtime and compiles the script.
//
// The caller must hold mu.
func (r *ScriptRenderer) ensureRuntime() error {
	if r.ready {
		return nil
	}

	if r.script == "" {
		return ErrNoScript
	}

	vm := goja.New()

	// The script is evaluated once: it only defines the function, the
	// record is bound on every call.
	if _, err := vm.RunString(r.script); err != nil {
		return fmt.Errorf("components: evaluate the script: %w", err)
	}

	if _, ok := goja.AssertFunction(vm.Get("flintRender")); !ok {
		return errors.New("components: the script does not define a flintRender function")
	}

	r.runtime = vm
	r.ready = true

	return nil
}

// Validate compiles the script and checks that it defines flintRender.
//
// It does not render anything: it only reports whether the script is
// usable. The application calls it at startup, so that a broken script
// is reported once instead of silently falling back on every record.
func (r *ScriptRenderer) Validate() error {
	if r.script == "" {
		return ErrNoScript
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	return r.ensureRuntime()
}

// evaluateWithTimeout evaluates the script, interrupting it when it runs
// longer than the timeout.
//
// The interrupt is what makes the timeout real: a goroutine that merely
// stops waiting would leave the runtime mid-execution, holding the lock
// forever.
func (r *ScriptRenderer) evaluateWithTimeout(rec *pbstore.Record) (ScriptResult, error) {
	timer := time.AfterFunc(r.timeout, func() {
		if vm := r.runtimeForInterrupt(); vm != nil {
			vm.Interrupt("components: the script timed out")
		}
	})
	defer timer.Stop()

	result, err := r.evaluate(rec)

	// The interrupt is sticky: it must be cleared before the next run.
	if vm := r.runtimeForInterrupt(); vm != nil {
		vm.ClearInterrupt()
	}

	if err != nil {
		var interrupted *goja.InterruptedError
		if errors.As(err, &interrupted) {
			return ScriptResult{}, fmt.Errorf("components: the script timed out after %s: %w", r.timeout, err)
		}

		return ScriptResult{}, err
	}

	return result, nil
}

// runtimeForInterrupt returns the runtime for the interrupt goroutine.
//
// It only reads the pointer: the runtime is created once, before any
// evaluation, so a nil result means the script never ran.
func (r *ScriptRenderer) runtimeForInterrupt() *goja.Runtime {
	r.interruptMu.Lock()
	defer r.interruptMu.Unlock()

	return r.runtime
}

// ScriptResult is the outcome of a script evaluation.
//
// It mirrors the three methods of [RecordRenderer], so that the result
// can be used without a second call.
type ScriptResult struct {
	// Title is the short title of the record.
	Title string
	// Summary is the one line description of the record.
	Summary string
	// Fields holds the ordered list of the displayed fields.
	Fields []FieldRow
}

// scriptResultFromValue converts the value returned by the script into
// a [ScriptResult].
//
// The script must return an object with the "title", "summary" and
// "fields" properties. The "fields" property is an array of
// "name: value" pairs.
func scriptResultFromValue(value goja.Value) (ScriptResult, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return ScriptResult{}, errors.New("components: the script returned nothing")
	}

	obj := value.ToObject(nil)
	if obj == nil {
		return ScriptResult{}, errors.New("components: the script did not return an object")
	}

	result := ScriptResult{}

	if title := obj.Get("title"); title != nil && !goja.IsUndefined(title) && !goja.IsNull(title) {
		result.Title = title.String()
	}

	if summary := obj.Get("summary"); summary != nil && !goja.IsUndefined(summary) && !goja.IsNull(summary) {
		result.Summary = summary.String()
	}

	fields := obj.Get("fields")
	if fields != nil && !goja.IsUndefined(fields) && !goja.IsNull(fields) {
		rows, err := scriptFieldsFromValue(fields)
		if err != nil {
			return ScriptResult{}, err
		}

		result.Fields = rows
	}

	return result, nil
}

// scriptFieldsFromValue converts the "fields" property into the field
// rows.
//
// The value is an array of "name: value" strings, or an array of
// objects with a "name" and a "value" property.
func scriptFieldsFromValue(value goja.Value) ([]FieldRow, error) {
	obj := value.ToObject(nil)
	if obj == nil {
		return nil, errors.New("components: the script fields are not an object")
	}

	length := int(obj.Get("length").ToInteger())
	rows := make([]FieldRow, 0, length)

	for i := 0; i < length; i++ {
		item := obj.Get(strconv.Itoa(i))
		if item == nil || goja.IsUndefined(item) || goja.IsNull(item) {
			continue
		}

		// An object with a name and a value.
		if itemObj := item.ToObject(nil); itemObj != nil {
			name := itemObj.Get("name")
			val := itemObj.Get("value")

			if name != nil && val != nil &&
				!goja.IsUndefined(name) && !goja.IsUndefined(val) {
				rows = append(rows, FieldRow{
					Name:  name.String(),
					Value: val.String(),
				})

				continue
			}
		}

		// A "name: value" string.
		text := item.String()

		name, val, found := strings.Cut(text, ":")
		if !found {
			rows = append(rows, FieldRow{Name: text})

			continue
		}

		rows = append(rows, FieldRow{
			Name:  strings.TrimSpace(name),
			Value: strings.TrimSpace(val),
		})
	}

	return rows, nil
}

// recordToMap converts a record into the plain map passed to the
// script.
//
// The system fields are kept: the script may want the id or the
// collection name.
func recordToMap(rec *pbstore.Record) map[string]any {
	if rec == nil {
		return map[string]any{}
	}

	out := make(map[string]any, len(rec.Fields)+4)

	for k, v := range rec.Fields {
		out[k] = v
	}

	out["id"] = rec.ID
	out["collectionId"] = rec.CollectionID
	out["collectionName"] = rec.CollectionName
	out["created"] = rec.Created
	out["updated"] = rec.Updated

	return out
}
