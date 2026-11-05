package code

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// Diagnostic is a problem the language server found in a file.
//
// It is the flattened form of the LSP diagnostics: the severity is a
// name and not a number, and the line is counted from 1.
type Diagnostic struct {
	// Path is the absolute path of the file.
	Path string
	// Line is the line the problem is at, counted from 1.
	Line int
	// Column is the column the problem is at, counted from 1.
	Column int
	// Severity is the kind of the problem: "error", "warning",
	// "information" or "hint".
	Severity string
	// Message is what the server says.
	Message string
	// Source is the name of the tool that reported it.
	Source string
	// Code is the code of the problem, when the server gives one.
	Code string
}

// String returns the diagnostic as one line a model can read.
func (d Diagnostic) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s:%d:%d: %s: %s", d.Path, d.Line, d.Column, d.Severity, d.Message)

	if d.Code != "" {
		fmt.Fprintf(&b, " (%s)", d.Code)
	}

	return b.String()
}

// diagnostics holds the diagnostics the server published.
//
// The server publishes them when it finishes analysing a file, and
// again every time the file changes. The cache holds the last set for
// every file, so that a question about the problems of a file is
// answered with what the server knows now.
type diagnostics struct {
	// mu guards the cache below.
	mu sync.RWMutex
	// byFile holds the diagnostics of every file, keyed by path.
	byFile map[string][]Diagnostic
}

// newDiagnostics returns an empty cache.
func newDiagnostics() *diagnostics {
	return &diagnostics{byFile: map[string][]Diagnostic{}}
}

// publish stores the diagnostics of a file.
//
// An empty list clears them: the server publishes an empty list when
// a file has no problems left.
func (d *diagnostics) publish(path string, items []Diagnostic) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(items) == 0 {
		delete(d.byFile, path)

		return
	}

	d.byFile[path] = items
}

// of returns the diagnostics of a file.
func (d *diagnostics) of(path string) []Diagnostic {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.byFile[path]
}

// all returns the diagnostics of every file, in a stable order.
func (d *diagnostics) all() []Diagnostic {
	d.mu.RLock()
	defer d.mu.RUnlock()

	out := make([]Diagnostic, 0, len(d.byFile))

	for _, items := range d.byFile {
		out = append(out, items...)
	}

	// The map is unordered: the diagnostics are sorted so that the
	// model reads the same list twice.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}

		return out[i].Line < out[j].Line
	})

	return out
}

// diagnosticFrom converts an LSP diagnostic into a [Diagnostic].
func diagnosticFrom(item protocol.Diagnostic, path string) Diagnostic {
	return Diagnostic{
		Path:     path,
		Line:     int(item.Range.Start.Line) + 1,
		Column:   int(item.Range.Start.Character) + 1,
		Severity: severityName(item.Severity),
		Message:  item.Message,
		Source:   item.Source,
		Code:     diagnosticCode(item.Code),
	}
}

// severityName names a severity.
//
// The LSP numbers them; the model reads names.
func severityName(severity protocol.DiagnosticSeverity) string {
	switch severity {
	case protocol.SeverityError:
		return "error"
	case protocol.SeverityWarning:
		return "warning"
	case protocol.SeverityInformation:
		return "information"
	case protocol.SeverityHint:
		return "hint"
	default:
		return "error"
	}
}

// diagnosticCode renders the code of a diagnostic.
//
// The code is anything the server wants it to be: a number, a string,
// or nothing at all.
func diagnosticCode(code any) string {
	switch typed := code.(type) {
	case string:
		return typed
	case float64:
		return strings.TrimSuffix(fmt.Sprintf("%v", typed), ".0")
	case int:
		return fmt.Sprintf("%d", typed)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}
