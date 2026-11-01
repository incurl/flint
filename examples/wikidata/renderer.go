package wikidata

import (
	"fmt"
	"strings"

	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
)

// Renderer renders a Wikidata item.
//
// It implements [components.RecordRenderer], so it plugs into the record
// card, the record table and the search results without touching the core
// packages.
type Renderer struct {
	// language is the preferred label language.
	language string
}

// Compile-time check that Renderer satisfies the interface.
var _ components.RecordRenderer = (*Renderer)(nil)

// NewRenderer creates a renderer for the given label language.
func NewRenderer(language string) *Renderer {
	if language == "" {
		language = "en"
	}

	return &Renderer{language: language}
}

// Title returns the label of the item, falling back to the QID.
func (r *Renderer) Title(rec *pbstore.Record) string {
	if rec == nil {
		return ""
	}

	if label := r.label(rec); label != "" {
		return label
	}

	return rec.StringField("qid")
}

// Summary returns the description of the item.
func (r *Renderer) Summary(rec *pbstore.Record) string {
	if rec == nil {
		return ""
	}

	description := r.description(rec)
	qid := rec.StringField("qid")

	if description == "" {
		return qid
	}

	return fmt.Sprintf("%s — %s", qid, description)
}

// Fields returns the displayed rows.
func (r *Renderer) Fields(rec *pbstore.Record) []components.FieldRow {
	if rec == nil {
		return nil
	}

	rows := []components.FieldRow{
		{Name: "QID", Value: rec.StringField("qid")},
		{Name: "Label", Value: r.label(rec)},
		{Name: "Description", Value: r.description(rec)},
	}

	if claims := rec.StringField("claims"); claims != "" {
		rows = append(rows, components.FieldRow{
			Name:  "Claims",
			Value: fmt.Sprintf("%d bytes of JSON", len(claims)),
		})
	}

	return rows
}

// label extracts the label for the preferred language.
//
// The "labels" field holds a JSON object shaped like
// {"en": {"language": "en", "value": "Douglas Adams"}, ...}.
func (r *Renderer) label(rec *pbstore.Record) string {
	byLang, ok := rec.Field("labels")
	if !ok {
		return ""
	}

	entries, ok := byLang.(map[string]any)
	if !ok {
		return ""
	}

	entry, ok := entries[r.language].(map[string]any)
	if !ok {
		return ""
	}

	value, _ := entry["value"].(string)

	return value
}

// description extracts the description for the preferred language.
func (r *Renderer) description(rec *pbstore.Record) string {
	byLang, ok := rec.Field("descriptions")
	if !ok {
		return ""
	}

	entries, ok := byLang.(map[string]any)
	if !ok {
		return ""
	}

	entry, ok := entries[r.language].(map[string]any)
	if !ok {
		return ""
	}

	value, _ := entry["value"].(string)

	return strings.TrimSpace(value)
}
