package components

import (
	"fmt"

	"charm.land/bubbles/v2/paginator"
	tea "charm.land/bubbletea/v2"

	"github.com/incurl/flint/internal/theme"
)

// Pagination renders the page navigation control of the record list.
//
// It is driven by the native PocketBase pagination metadata (page,
// perPage, totalItems, totalPages), so it stays consistent with whatever
// the backend returned.
type Pagination struct {
	// theme is the shared style palette.
	theme *theme.Theme
	// paginator is the bubbles paginator model.
	paginator paginator.Model
	// totalItems is the total number of matching records.
	totalItems int
	// perPage is the page size.
	perPage int
}

// NewPagination creates a pagination component.
//
// The perPage value is normalised with [pbstore.NormalizePerPage] so that
// the component never disagrees with the backend.
func NewPagination(t *theme.Theme, perPage int) *Pagination {
	if t == nil {
		t = theme.Default()
	}

	if perPage <= 0 {
		perPage = 30
	}

	p := paginator.New()
	p.Type = paginator.Dots
	p.ActiveDot = t.Selected.Render("•")
	p.InactiveDot = t.Muted.Render("•")

	return &Pagination{
		theme:     t,
		paginator: p,
		perPage:   perPage,
	}
}

// SetTotal updates the total number of items and the current page.
//
// It returns the number of pages, which is 0 when there is no item.
func (p *Pagination) SetTotal(totalItems, page, perPage int) int {
	if perPage > 0 {
		p.perPage = perPage
	}

	p.totalItems = totalItems

	pages := 0
	if p.perPage > 0 {
		pages = (totalItems + p.perPage - 1) / p.perPage
	}

	p.paginator.TotalPages = pages

	p.paginator.Page = page
	if p.paginator.PerPage != p.perPage {
		p.paginator.PerPage = p.perPage
	}

	return pages
}

// Page returns the current page number.
func (p *Pagination) Page() int { return p.paginator.Page }

// SetPage sets the current page number.
// SetPage sets the current page number.
func (p *Pagination) SetPage(page int) { p.paginator.Page = page }

// TotalPages returns the number of pages.
func (p *Pagination) TotalPages() int { return p.paginator.TotalPages }

// PerPage returns the page size.
func (p *Pagination) PerPage() int { return p.perPage }

// TotalItems returns the total number of matching records.
func (p *Pagination) TotalItems() int { return p.totalItems }

// Init implements [tea.Model].
func (p *Pagination) Init() tea.Cmd { return nil }

// Update implements [tea.Model].
func (p *Pagination) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "left", "h", "p":
			p.paginator.PrevPage()
			return p, nil
		case "right", "l", "n":
			p.paginator.NextPage()
			return p, nil
		}
	}

	return p, nil
}

// View renders the pagination control.
func (p *Pagination) View() tea.View {
	if p.totalItems == 0 {
		return tea.NewView(p.theme.Muted.Render("no records"))
	}

	summary := p.theme.Muted.Render(fmt.Sprintf("%d records · page %d/%d",
		p.totalItems, p.paginator.Page, maxInt(p.paginator.TotalPages, 1)))

	return tea.NewView(summary + "  " + p.paginator.View())
}

// maxInt returns the larger of the two provided integers.
func maxInt(a, b int) int {
	if a > b {
		return a
	}

	return b
}
