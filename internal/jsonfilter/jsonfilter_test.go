package jsonfilter_test

import (
	"strings"
	"testing"

	"github.com/incurl/flint/internal/jsonfilter"
)

const document = `{
  "meta": {
    "lang": "go",
    "level": 3,
    "active": true,
    "tags": ["cli", "fast", "tui"],
    "items": [
      {"name": "alpha", "qty": 1},
      {"name": "beta", "qty": 7}
    ],
    "nested": {"deep": {"value": "x"}}
  }
}`

func TestMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		expr string
		want bool
	}{
		{"equality", `meta.lang = "go"`, true},
		{"equality no match", `meta.lang = "rust"`, false},
		{"inequality", `meta.lang != "rust"`, true},
		{"numeric greater", `meta.level > 2`, true},
		{"numeric greater no match", `meta.level > 5`, false},
		{"numeric greater or equal", `meta.level >= 3`, true},
		{"numeric less", `meta.level < 5`, true},
		{"numeric less or equal", `meta.level <= 3`, true},
		{"numeric equal", `meta.level = 3`, true},
		{"contains", `meta.lang ~ "o"`, true},
		{"contains no match", `meta.lang ~ "z"`, false},
		{"conjunction", `meta.lang = "go" && meta.level > 2`, true},
		{"conjunction no match", `meta.lang = "go" && meta.level > 9`, false},
		{"disjunction", `meta.lang = "rust" || meta.level > 2`, true},
		{"disjunction no match", `meta.lang = "rust" || meta.level > 9`, false},
		{"nested object", `meta.nested.deep.value = "x"`, true},
		{"array contains", `meta.tags.#(=="cli")`, true},
		{"array contains no match", `meta.tags.#(=="nope")`, false},
		{"array index", `meta.items.0.name = "alpha"`, true},
		{"array index no match", `meta.items.1.name = "alpha"`, false},
		{"array numeric across elements", `meta.items.#.qty > 5`, true},
		{"array numeric across elements no match", `meta.items.#.qty > 99`, false},
		{"missing path does not match", `meta.absent = "x"`, false},
		{"boolean field", `meta.active = true`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filter, err := jsonfilter.Parse(tt.expr)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", tt.expr, err)
			}

			if got := filter.Match(document); got != tt.want {
				t.Errorf("Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"   ",
		`meta.lang`,
		`= "go"`,
		`meta.lang = "unterminated`,
	}

	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			t.Parallel()

			if _, err := jsonfilter.Parse(expr); err == nil {
				t.Errorf("Parse(%q) returned no error", expr)
			}
		})
	}
}

func TestMatchInvalidDocument(t *testing.T) {
	t.Parallel()

	filter, err := jsonfilter.Parse(`meta.lang = "go"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if filter.Match("{not json") {
		t.Error("Match() = true for an invalid document, want false")
	}
}

func TestNilFilter(t *testing.T) {
	t.Parallel()

	var filter *jsonfilter.Filter

	if !filter.Match(document) {
		t.Error("a nil filter must match everything")
	}

	if filter.Expr() != "" {
		t.Errorf("Expr() = %q, want an empty string", filter.Expr())
	}
}

func TestExpr(t *testing.T) {
	t.Parallel()

	filter, err := jsonfilter.Parse(`meta.lang = "go"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if filter.Expr() != `meta.lang = "go"` {
		t.Errorf("Expr() = %q, want %q", filter.Expr(), `meta.lang = "go"`)
	}
}

func TestNeedsLocal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		expr string
		want bool
	}{
		// The native grammar handles these.
		{`meta.lang = "go"`, false},
		{`meta.level > 2`, false},
		{`meta.lang ~ "g"`, false},
		{`meta.lang = "go" && meta.level > 2`, false},
		{`meta.nested.deep.value = "x"`, false},
		{`title = "hello"`, false},
		{`meta.level >= 2 && meta.level <= 9`, false},

		// The native grammar cannot handle these.
		{`meta.tags.#(=="cli")`, true},
		{`meta.tags[0] = "cli"`, true},
		{`meta.items.0.name = "alpha"`, true},
		{`meta.items.#.qty > 5`, true},
		{`not (meta.lang = "go")`, true},
		{`meta.lang = "go" && meta.tags.#(=="cli")`, true},
		{`meta.lang = "go" || meta.items.0.name = "x"`, true},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			t.Parallel()

			if got := jsonfilter.NeedsLocal(tt.expr); got != tt.want {
				t.Errorf("NeedsLocal(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestNeedsLocalIgnoresBracketsInStrings(t *testing.T) {
	t.Parallel()

	// A bracket inside a quoted value is a literal, not an array index.
	expr := `meta.label = "a[0]b"`

	if jsonfilter.NeedsLocal(expr) {
		t.Errorf("NeedsLocal(%q) = true, want false", expr)
	}
}

func TestParseAndMatchAgreeWithNeedsLocal(t *testing.T) {
	t.Parallel()

	// Every expression that NeedsLocal must also be parseable, so that
	// the caller never has to handle a parse failure of a locally
	// evaluated expression.
	expressions := []string{
		`meta.tags.#(=="cli")`,
		`meta.items.0.name = "alpha"`,
		`meta.items.#.qty > 5`,
		`not (meta.lang = "go")`,
	}

	for _, expr := range expressions {
		if !jsonfilter.NeedsLocal(expr) {
			continue
		}

		if _, err := jsonfilter.Parse(expr); err != nil {
			t.Errorf("Parse(%q) error = %v", expr, err)
		}
	}
}

func TestMatchIsCaseSensitive(t *testing.T) {
	t.Parallel()

	filter, err := jsonfilter.Parse(`meta.lang = "GO"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if filter.Match(document) {
		t.Error("Match() = true for a different case, want false")
	}
}

func TestMatchDoesNotMutateTheFilter(t *testing.T) {
	t.Parallel()

	filter, err := jsonfilter.Parse(`meta.tags.#(=="cli")`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	for i := 0; i < 3; i++ {
		if !filter.Match(document) {
			t.Fatalf("Match() = false on run %d", i)
		}
	}
}

func TestErrorMessagesMentionTheExpression(t *testing.T) {
	t.Parallel()

	_, err := jsonfilter.Parse(`meta.lang`)
	if err == nil {
		t.Fatal("Parse() returned no error")
	}

	if !strings.Contains(err.Error(), "meta.lang") {
		t.Errorf("error %q does not mention the expression", err)
	}
}
