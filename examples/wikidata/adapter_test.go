package wikidata_test

import (
	"testing"

	"github.com/incurl/flint/examples/wikidata"
	"github.com/incurl/flint/internal/pbstore"
)

// TestBuildFilter verifies the human query to native filter translation.
func TestBuildFilter(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{name: "empty", query: "", want: ""},
		{name: "whitespace", query: "   ", want: ""},
		{name: "qid", query: "q42", want: `qid = "Q42"`},
		{name: "uppercase qid", query: "Q31", want: `qid = "Q31"`},
		{name: "label", query: "human", want: `label ~ "human"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wikidata.BuildFilter(tt.query); got != tt.want {
				t.Errorf("BuildFilter(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}

// TestRenderer verifies the Wikidata record renderer.
func TestRenderer(t *testing.T) {
	r := wikidata.NewRenderer("en")

	rec := &pbstore.Record{
		ID: "rec1",
		Fields: map[string]any{
			"qid":          "Q42",
			"labels":       map[string]any{"en": map[string]any{"value": "Douglas Adams"}},
			"descriptions": map[string]any{"en": map[string]any{"value": "author"}},
			"claims":       map[string]any{"P31": []any{}},
		},
	}

	if got := r.Title(rec); got != "Douglas Adams" {
		t.Errorf("Title() = %q, want %q", got, "Douglas Adams")
	}
	if got := r.Summary(rec); got != "Q42 — author" {
		t.Errorf("Summary() = %q, want %q", got, "Q42 — author")
	}

	rows := r.Fields(rec)
	if len(rows) != 4 {
		t.Fatalf("Fields() returned %d rows, want 4", len(rows))
	}
	if rows[0].Name != "QID" || rows[0].Value != "Q42" {
		t.Errorf("first row = %+v, want the QID", rows[0])
	}

	// A record without a label falls back to the QID.
	empty := &pbstore.Record{Fields: map[string]any{"qid": "Q1"}}
	if got := r.Title(empty); got != "Q1" {
		t.Errorf("Title() = %q, want %q", got, "Q1")
	}
}
