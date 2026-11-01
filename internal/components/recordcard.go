package components

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/theme"
)

// RecordRenderer renders a record into a human readable summary.
//
// Downstream projects inject their own renderer to display dictionary
// entries, Wikidata items, reference metadata, ... without touching the
// core packages.
type RecordRenderer interface {
	// Title returns the short title of the record.
	Title(rec *pbstore.Record) string
	// Summary returns the one line description of the record.
	Summary(rec *pbstore.Record) string
	// Fields returns the ordered list of the displayed fields.
	//
	// Each entry is a "name: value" pair.
	Fields(rec *pbstore.Record) []FieldRow
}

// FieldRow is a single "name: value" line of a record card.
type FieldRow struct {
	// Name is the field label.
	Name string
	// Value is the field value.
	Value string
}

// DefaultRecordRenderer is the generic renderer used when no domain
// specific renderer is registered.
//
// It renders the record id and the first few non-system fields, which is
// enough for an arbitrary schema.
type DefaultRecordRenderer struct {
	// TitleFields lists the field names preferred for the title, in order.
	//
	// When empty the renderer falls back to a generic heuristic.
	TitleFields []string
	// MaxFields is the maximum number of fields rendered in the details.
	//
	// Zero means 8.
	MaxFields int
}

// Compile-time check that DefaultRecordRenderer satisfies RecordRenderer.
var _ RecordRenderer = (*DefaultRecordRenderer)(nil)

// systemFields are the PocketBase bookkeeping fields that are never shown
// as regular content.
var systemFields = map[string]bool{
	"id":             true,
	"collectionId":   true,
	"collectionName": true,
	"created":        true,
	"updated":        true,
	"expand":         true,
}

// Title implements [RecordRenderer].
func (r *DefaultRecordRenderer) Title(rec *pbstore.Record) string {
	if rec == nil {
		return ""
	}

	for _, name := range r.TitleFields {
		if v := rec.StringField(name); v != "" {
			return v
		}
	}

	// Fall back to the best non-system string field.
	//
	// The preferred names are the usual display fields; when none of them
	// is present the shortest string wins, because a title is usually a
	// short label rather than a long description.
	preferred := []string{"label", "name", "title", "term", "key"}
	for _, name := range preferred {
		value, ok := rec.Field(name)
		if !ok || value == nil {
			continue
		}
		text, ok := value.(string)
		if !ok || text == "" {
			continue
		}
		return text
	}

	var best string
	for _, name := range r.sortedFieldNames(rec) {
		if systemFields[name] {
			continue
		}
		// Only a genuine string makes a good title: a number or a
		// boolean would render as an unhelpful value.
		value, ok := rec.Field(name)
		if !ok || value == nil {
			continue
		}
		text, ok := value.(string)
		if !ok || text == "" {
			continue
		}
		if best == "" || len(text) < len(best) {
			best = text
		}
	}
	if best != "" {
		return best
	}

	return rec.ID
}

// Summary implements [RecordRenderer].
func (r *DefaultRecordRenderer) Summary(rec *pbstore.Record) string {
	if rec == nil {
		return ""
	}
	return fmt.Sprintf("%s · %s", rec.CollectionName, rec.ID)
}

// Fields implements [RecordRenderer].
func (r *DefaultRecordRenderer) Fields(rec *pbstore.Record) []FieldRow {
	if rec == nil {
		return nil
	}

	max := r.MaxFields
	if max <= 0 {
		max = 8
	}

	rows := make([]FieldRow, 0, max)
	for _, name := range r.sortedFieldNames(rec) {
		if systemFields[name] {
			continue
		}
		if len(rows) >= max {
			break
		}
		rows = append(rows, FieldRow{Name: name, Value: formatValue(rec.Fields[name])})
	}
	return rows
}

// sortedFieldNames returns the record field names in a stable order.
func (r *DefaultRecordRenderer) sortedFieldNames(rec *pbstore.Record) []string {
	names := make([]string, 0, len(rec.Fields))
	for name := range rec.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RecordCard renders the detail view of a single record.
//
// It is deliberately generic: the actual content comes from the injected
// [RecordRenderer], so a downstream project can display any domain
// specific layout without modifying this package.
type RecordCard struct {
	// theme is the shared style palette.
	theme *theme.Theme
	// renderer formats the record content.
	renderer RecordRenderer
	// record is the displayed record.
	record *pbstore.Record
	// width is the available rendering width.
	width int
}

// NewRecordCard creates a record card component.
//
// A nil renderer falls back to [DefaultRecordRenderer].
func NewRecordCard(t *theme.Theme, renderer RecordRenderer) *RecordCard {
	if t == nil {
		t = theme.Default()
	}
	if renderer == nil {
		renderer = &DefaultRecordRenderer{}
	}
	return &RecordCard{theme: t, renderer: renderer}
}

// SetRenderer replaces the renderer used to format the record.
func (c *RecordCard) SetRenderer(renderer RecordRenderer) {
	if renderer == nil {
		renderer = &DefaultRecordRenderer{}
	}
	c.renderer = renderer
}

// SetRecord replaces the displayed record.
func (c *RecordCard) SetRecord(rec *pbstore.Record) { c.record = rec }

// Record returns the displayed record.
func (c *RecordCard) Record() *pbstore.Record { return c.record }

// SetWidth sets the available rendering width.
func (c *RecordCard) SetWidth(width int) { c.width = width }

// View renders the card.
//
// The layout adapts to the available width: the field name and value are
// stacked when the terminal is too narrow.
func (c *RecordCard) View() string {
	if c.record == nil {
		return c.theme.Muted.Render("no record selected")
	}

	var b strings.Builder

	b.WriteString(c.theme.Title.Render(c.renderer.Title(c.record)))
	b.WriteString("\n")
	b.WriteString(c.theme.Muted.Render(c.renderer.Summary(c.record)))
	b.WriteString("\n\n")

	rows := c.renderer.Fields(c.record)
	stacked := c.width > 0 && c.width < 48

	for _, row := range rows {
		if stacked {
			b.WriteString(c.theme.Label.Render(row.Name))
			b.WriteString("\n")
			b.WriteString("  " + c.theme.Value.Render(row.Value))
			b.WriteString("\n")
			continue
		}

		label := c.theme.Label.Render(padRight(row.Name, 14))
		b.WriteString(label + c.theme.Value.Render(row.Value))
		b.WriteString("\n")
	}

	return b.String()
}

// padRight pads a string with spaces up to the requested width.
//
// The padding is applied on the visible characters only, so ANSI escape
// sequences are not counted.
func padRight(s string, width int) string {
	visible := lipgloss.Width(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

// formatValue renders an arbitrary field value as a single line string.
func formatValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.ReplaceAll(v, "\n", " ")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, formatValue(item))
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		return fmt.Sprintf("{%d keys}", len(v))
	default:
		return fmt.Sprintf("%v", v)
	}
}
