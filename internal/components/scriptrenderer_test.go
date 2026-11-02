package components_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
)

// newRecord builds a record for the script tests.
func newScriptRecord(id string, fields map[string]any) *pbstore.Record {
	return &pbstore.Record{
		ID:             id,
		CollectionName: "items",
		Updated:        "2024-01-01 00:00:00.000Z",
		Fields:         fields,
	}
}

const renderScript = `
function flintRender(record) {
    return {
        title:   "script:" + (record.label || record.id),
        summary: "script summary",
        fields: [
            { name: "Key", value: String(record.key) },
        ],
    }
}
`

func TestScriptRendererRenders(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1.pb.js"), []byte(renderScript), 0o600); err != nil {
		t.Fatal(err)
	}

	script, err := components.LoadScript(dir)
	if err != nil {
		t.Fatalf("LoadScript() error = %v", err)
	}

	renderer := components.NewCachedRenderer(script, nil)
	rec := newScriptRecord("r1", map[string]any{"label": "Hello", "key": "k"})

	// Nothing is cached yet: the Go fallback renders the record.
	if got := renderer.Title(rec); got == "script:Hello" {
		t.Error("Title() used the script before the preload")
	}

	renderer.Preload([]*pbstore.Record{rec})

	if got := renderer.Title(rec); got != "script:Hello" {
		t.Errorf("Title() = %q, want %q", got, "script:Hello")
	}

	rows := renderer.Fields(rec)
	if len(rows) != 1 || rows[0].Name != "Key" || rows[0].Value != "k" {
		t.Errorf("Fields() = %+v, want one Key: k row", rows)
	}
}

// TestScriptRendererUsesTheCurrentRecord guards against the regression
// where the record was bound once, so every record rendered as the
// first one.
func TestScriptRendererUsesTheCurrentRecord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1.pb.js"), []byte(renderScript), 0o600); err != nil {
		t.Fatal(err)
	}

	script, err := components.LoadScript(dir)
	if err != nil {
		t.Fatalf("LoadScript() error = %v", err)
	}

	renderer := components.NewCachedRenderer(script, nil)

	first := newScriptRecord("r1", map[string]any{"label": "First", "key": "a"})
	second := newScriptRecord("r2", map[string]any{"label": "Second", "key": "b"})

	renderer.Preload([]*pbstore.Record{first, second})

	if got := renderer.Title(first); got != "script:First" {
		t.Errorf("the first record rendered as %q, want %q", got, "First")
	}

	if got := renderer.Title(second); got != "script:Second" {
		t.Errorf("the second record rendered as %q, want %q", got, "Second")
	}
}

func TestScriptRendererFallsBackOnAnError(t *testing.T) {
	t.Parallel()

	script := `
function flintRender(record) {
    throw new Error("boom")
}
`
	renderer := components.NewCachedRenderer(script, nil)
	rec := newScriptRecord("r1", map[string]any{"label": "Hello"})

	renderer.Preload([]*pbstore.Record{rec})

	// The Go renderer must produce a title, because the script failed.
	if got := renderer.Title(rec); got == "" {
		t.Error("Title() is empty after a script error, want the Go fallback")
	}
}

func TestScriptRendererFallsBackWithoutFlintRender(t *testing.T) {
	t.Parallel()

	renderer := components.NewCachedRenderer("var x = 1", nil)
	rec := newScriptRecord("r1", map[string]any{"label": "Hello"})

	renderer.Preload([]*pbstore.Record{rec})

	if got := renderer.Title(rec); got == "" {
		t.Error("Title() is empty without flintRender, want the Go fallback")
	}
}

func TestScriptRendererTimesOut(t *testing.T) {
	t.Parallel()

	// An endless loop must not hang the preload forever.
	script := `
function flintRender(record) {
    while (true) {}
}
`
	renderer := components.NewCachedRenderer(script, nil)
	rec := newScriptRecord("r1", map[string]any{"label": "Hello"})

	done := make(chan struct{})

	go func() {
		renderer.Preload([]*pbstore.Record{rec})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(components.ScriptTimeout + 5*time.Second):
		t.Fatal("Preload() did not return: the timeout did not interrupt the script")
	}

	// The fallback renders the record.
	if got := renderer.Title(rec); got == "" {
		t.Error("Title() is empty after a timeout, want the Go fallback")
	}
}

func TestScriptRendererIsNotEvaluatedInTheRenderPath(t *testing.T) {
	t.Parallel()

	// A script with a side effect: it counts its own calls.
	script := `
var calls = 0
function flintRender(record) {
    calls++
    return { title: "T" + calls, summary: "", fields: [] }
}
`
	renderer := components.NewCachedRenderer(script, nil)
	rec := newScriptRecord("r1", map[string]any{"label": "Hello"})

	renderer.Preload([]*pbstore.Record{rec})

	first := renderer.Title(rec)

	// Rendering the record repeatedly must not evaluate the script
	// again: the render loop reads the cache.
	for i := 0; i < 10; i++ {
		if got := renderer.Title(rec); got != first {
			t.Fatalf("Title() changed on render %d: %q -> %q", i, first, got)
		}
	}
}

func TestCachedRendererReRendersAChangedRecord(t *testing.T) {
	t.Parallel()

	script := `
function flintRender(record) {
    return { title: record.label, summary: "", fields: [] }
}
`
	renderer := components.NewCachedRenderer(script, nil)
	rec := newScriptRecord("r1", map[string]any{"label": "Before"})

	renderer.Preload([]*pbstore.Record{rec})

	if got := renderer.Title(rec); got != "Before" {
		t.Errorf("Title() = %q, want %q", got, "Before")
	}

	// The record changes: it must be rendered again.
	rec.Fields["label"] = "After"
	rec.Updated = "2024-02-02 00:00:00.000Z"

	renderer.Preload([]*pbstore.Record{rec})

	if got := renderer.Title(rec); got != "After" {
		t.Errorf("Title() = %q, want %q", got, "After")
	}
}

func TestCachedRendererInvalidate(t *testing.T) {
	t.Parallel()

	script := `
function flintRender(record) {
    return { title: record.label, summary: "", fields: [] }
}
`
	renderer := components.NewCachedRenderer(script, nil)
	rec := newScriptRecord("r1", map[string]any{"label": "Hello"})

	renderer.Preload([]*pbstore.Record{rec})

	if renderer.Len() != 1 {
		t.Errorf("Len() = %d, want 1", renderer.Len())
	}

	renderer.Invalidate("r1")

	if renderer.Len() != 0 {
		t.Errorf("Len() = %d after Invalidate, want 0", renderer.Len())
	}

	renderer.Preload([]*pbstore.Record{rec})
	renderer.InvalidateAll()

	if renderer.Len() != 0 {
		t.Errorf("Len() = %d after InvalidateAll, want 0", renderer.Len())
	}
}

func TestLoadScript(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "a.pb.js"), []byte("var a = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "b.pb.js"), []byte("var b = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A file that does not match the extension is ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	script, err := components.LoadScript(dir)
	if err != nil {
		t.Fatalf("LoadScript() error = %v", err)
	}

	if !strings.Contains(script, "var a = 1") || !strings.Contains(script, "var b = 2") {
		t.Errorf("LoadScript() = %q, want both files", script)
	}

	if strings.Contains(script, "ignored") {
		t.Errorf("LoadScript() = %q, want the .txt file to be skipped", script)
	}
}

func TestLoadScriptMissingDirectory(t *testing.T) {
	t.Parallel()

	// A missing directory is not an error: it yields an empty script.
	script, err := components.LoadScript(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatalf("LoadScript() error = %v", err)
	}

	if script != "" {
		t.Errorf("LoadScript() = %q, want an empty script", script)
	}
}
