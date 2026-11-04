package pages_test

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/pages"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/state"
	"github.com/incurl/flint/internal/theme"
)

// TestThePagesFitTheTerminal verifies that no page renders more lines
// than the terminal has.
//
// A page that renders more is cut off by the terminal, and the lines
// at the end are the ones that are lost: the last message of a
// conversation, the last field of a record.
func TestThePagesFitTheTerminal(t *testing.T) {
	const height = 20

	// A record with many fields.
	fields := map[string]any{}
	for i := range 40 {
		fields[string(rune('a'+i%26))+string(rune('a'+i/26))] = "a value that is long enough to show"
	}

	record := &pbstore.Record{ID: "1", CollectionName: "vocab", Fields: fields}

	store := &stubStore{
		collections: []pbstore.Collection{{ID: "1", Name: "vocab", Type: "base"}},
		records:     []*pbstore.Record{record},
	}

	runtime := state.NewRuntime()
	runtime.Collections = store.collections

	detail := pages.NewDetailPage(store, runtime, theme.Default(), nil)

	home := pages.NewHomePage(store, runtime, theme.Default())

	search := pages.NewSearchPage(store, runtime, theme.Default(), nil)

	records := pages.NewRecordsPage(store, runtime, theme.Default(), nil)

	rows := make([][2]string, 0, 60)
	for range 60 {
		rows = append(rows, [2]string{"setting.name", "value"})
	}

	settings := pages.NewSettingsPage(runtime, theme.Default(), rows)

	pages := map[string]tea.Model{
		"detail":   detail,
		"home":     home,
		"search":   search,
		"records":  records,
		"settings": settings,
	}

	for name, page := range pages {
		updated, _ := page.Update(tea.WindowSizeMsg{Width: 80, Height: height})
		page = updated

		lines := strings.Split(page.View().Content, "\n")

		if len(lines) > height {
			t.Errorf("the %s page rendered %d lines for a %d line terminal", name, len(lines), height)
		}
	}
}

// TestTheDetailPageFillsTheTerminal verifies that the record view uses
// the whole page it is given.
//
// A page that renders one line too many loses the line at the bottom,
// and one that renders too few leaves the terminal half empty: the
// fields and the JSON tree share everything between the title and the
// help line, so the record view fills the page exactly.
func TestTheDetailPageFillsTheTerminal(t *testing.T) {
	fields := map[string]any{"label": "Go"}

	for i := range 20 {
		fields[fmt.Sprintf("field-%02d", i)] = "a value that is long enough to wrap on a narrow page"
	}

	record := &pbstore.Record{ID: "rec1", CollectionName: "vocab", Fields: fields}

	store := &stubStore{records: []*pbstore.Record{record}}

	for _, width := range []int{80, 120, 200} {
		for _, height := range []int{10, 20, 40} {
			runtime := newRuntime()
			runtime.SelectedID = "rec1"

			page := pages.NewDetailPage(store, runtime, theme.Default(), nil)

			updated, _ := page.Update(tea.WindowSizeMsg{Width: width, Height: height})
			page = updated.(*pages.DetailPage)

			updated, _ = page.Update(pages.DetailMsg{Record: record, ID: "rec1"})
			page = updated.(*pages.DetailPage)

			checkFills(t, "structured", page.View().Content, width, height)

			// Pressing `r` leaves the tree on its own.
			updated, _ = page.Update(tea.KeyPressMsg{Code: 'r', Text: "r"})
			page = updated.(*pages.DetailPage)

			checkFills(t, "raw", page.View().Content, width, height)

			// Pressing `e` gives the page to the editor.
			updated, _ = page.Update(tea.KeyPressMsg{Code: 'e', Text: "e"})
			page = updated.(*pages.DetailPage)

			checkFills(t, "editor", page.View().Content, width, height)
		}
	}
}

// checkFills asserts that a view is exactly the height of the page and
// no wider than it.
func checkFills(t *testing.T, name, view string, width, height int) {
	t.Helper()

	lines := strings.Split(view, "\n")

	if len(lines) != height {
		t.Errorf("the %s view at %dx%d rendered %d lines, want %d", name, width, height, len(lines), height)
	}

	for i, line := range lines {
		if w := lipgloss.Width(line); w > width {
			t.Errorf("the %s view at %dx%d: line %d is %d cells wide", name, width, height, i, w)

			break
		}
	}
}

// TestTheHomePageShowsTheCursor verifies that the window of the
// collections follows the cursor.
//
// The list is as long as the number of collections, and the page has a
// height: the cursor has to stay in sight.
func TestTheHomePageShowsTheCursor(t *testing.T) {
	const height = 20

	collections := make([]pbstore.Collection, 0, 60)
	for i := range 60 {
		collections = append(collections, pbstore.Collection{
			ID:   string(rune('a' + i%26)),
			Name: "collection" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Type: "base",
		})
	}

	store := &stubStore{collections: collections}

	runtime := state.NewRuntime()
	runtime.Collections = collections

	page := pages.NewHomePage(store, runtime, theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 80, Height: height})
	page = updated.(*pages.HomePage)

	// The page shows the collections it was given.
	updated, _ = page.Update(pages.HomeMsg{Collections: collections})
	page = updated.(*pages.HomePage)

	// Move the cursor to the end of the list.
	for range 59 {
		updated, _ = page.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		page = updated.(*pages.HomePage)
	}

	view := page.View().Content
	lines := strings.Split(view, "\n")

	if len(lines) > height {
		t.Errorf("the home page rendered %d lines for a %d line terminal", len(lines), height)
	}

	// The last collection is the selected one, and it is on the screen.
	last := collections[len(collections)-1].Name

	if !strings.Contains(view, last) {
		t.Errorf("the home page does not show the selected collection %q:\n%s", last, view)
	}
}

// TestTheAgentPageFitsTheTerminal verifies that the conversation is
// scrolled and follows the end.
//
// A conversation that renders more lines than the terminal has is cut
// off, and the lines at the end are the ones that are lost: the last
// message of the conversation, the one the reader is waiting for.
func TestTheAgentPageFitsTheTerminal(t *testing.T) {
	const height = 20

	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: "ok"}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 80, Height: height})
	page = updated.(*pages.AgentPage)

	for i := range 10 {
		updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
		page = updated.(*pages.AgentPage)

		updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		page = updated.(*pages.AgentPage)

		updated, _ = page.Update(pages.AgentDoneMsg{
			RunID:     fmt.Sprintf("run-%d", 2*i+1),
			Text:      fmt.Sprintf("the answer %d", i),
			SessionID: "s1",
		})
		page = updated.(*pages.AgentPage)
	}

	view := page.View().Content
	lines := strings.Split(view, "\n")

	if len(lines) > height {
		t.Errorf("the agent page rendered %d lines for a %d line terminal", len(lines), height)
	}

	// The last message is on the screen: the page follows the end.
	if !strings.Contains(view, "the answer 9") {
		t.Errorf("the agent page does not show the last message:\n%s", view)
	}
}

// TestTheAgentPageScrollsUp verifies that the reader can scroll back
// through the conversation.
//
// The page follows the end while the reader is there, and the reader
// decides when to look at something else.
func TestTheAgentPageScrollsUp(t *testing.T) {
	const height = 20

	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: "ok"}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 80, Height: height})
	page = updated.(*pages.AgentPage)

	for i := range 10 {
		updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
		page = updated.(*pages.AgentPage)

		updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		page = updated.(*pages.AgentPage)

		updated, _ = page.Update(pages.AgentDoneMsg{
			RunID:     fmt.Sprintf("run-%d", 2*i+1),
			Text:      fmt.Sprintf("the answer %d", i),
			SessionID: "s1",
		})
		page = updated.(*pages.AgentPage)
	}

	// Scroll up.
	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	page = updated.(*pages.AgentPage)

	view := page.View().Content

	// The last message is not on the screen any more: the reader is
	// looking at something else.
	if strings.Contains(view, "the answer 9") {
		t.Errorf("the page did not scroll up:\n%s", view)
	}

	// And the page says that it can be scrolled.
	if !strings.Contains(view, "pgup/pgdn scroll") {
		t.Errorf("the page does not advertise the scroll keys:\n%s", view)
	}
}

// TestTheAgentPageFollowsTheEndAgain verifies that the page goes back
// to the end when the reader comes back to it.
//
// The reader decides what is in view: the content does not move under
// them, and it does not stay where they left it either.
func TestTheAgentPageFollowsTheEndAgain(t *testing.T) {
	const height = 20

	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: "ok"}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 80, Height: height})
	page = updated.(*pages.AgentPage)

	for i := range 10 {
		updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
		page = updated.(*pages.AgentPage)

		updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		page = updated.(*pages.AgentPage)

		updated, _ = page.Update(pages.AgentDoneMsg{
			RunID:     fmt.Sprintf("run-%d", 2*i+1),
			Text:      fmt.Sprintf("the answer %d", i),
			SessionID: "s1",
		})
		page = updated.(*pages.AgentPage)
	}

	// Scroll up, then back down.
	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	page = updated.(*pages.AgentPage)

	view := page.View().Content

	if !strings.Contains(view, "the answer 9") {
		t.Errorf("the page did not follow the end again:\n%s", view)
	}

	if strings.Contains(view, "pgup/pgdn scroll") {
		t.Errorf("the page still says it can be scrolled:\n%s", view)
	}
}

// TestThePlanModeKeepsThePlanIntact verifies that a plan is not
// mangled by the wrapping.
//
// The content of the scroller is wrapped before it reaches the
// viewport, and the viewport counts the lines again: a blank line that
// is counted twice becomes two blank lines, and the plan of the model
// grows a line for every step it has.
func TestThePlanModeKeepsThePlanIntact(t *testing.T) {
	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: "ok"}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	page = updated.(*pages.AgentPage)

	// The plan mode.
	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	page = updated.(*pages.AgentPage)

	if page.Mode() != agent.ModePlan {
		t.Fatalf("the mode is %q, want plan", page.Mode())
	}

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	plan := "# Plan\n\n1. Do this.\n2. Do that.\n\n" + agent.PlanReadyMarker

	updated, _ = page.Update(pages.AgentDoneMsg{RunID: "run-1", Text: plan, SessionID: "s1"})
	page = updated.(*pages.AgentPage)

	view := page.View().Content

	// The plan is ready for the approval.
	if !strings.Contains(view, "The plan is ready") {
		t.Errorf("the plan is not ready:\n%s", view)
	}

	// The markers are not shown: they are instructions to the model.
	if strings.Contains(view, agent.PlanReadyMarker) {
		t.Errorf("the marker is on the screen:\n%s", view)
	}

	// The steps are there, each on its own line.
	if !strings.Contains(view, "1. Do this.") || !strings.Contains(view, "2. Do that.") {
		t.Errorf("the steps are missing:\n%s", view)
	}
}

// TestThePlanApprovalSwitchesToActMode verifies the handoff.
//
// The mode returns to acting, and the plan becomes the first message
// of the run that implements it.
func TestThePlanApprovalSwitchesToActMode(t *testing.T) {
	runner := agent.NewRunner(agent.Options{Model: &answerModel{text: "ok"}})

	page := pages.NewAgentPage(runner, nil, nil, state.NewRuntime(), theme.Default())

	updated, _ := page.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	page = updated.(*pages.AgentPage)

	updated, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	plan := "# Plan\n\n1. Do this.\n\n" + agent.PlanReadyMarker

	updated, _ = page.Update(pages.AgentDoneMsg{RunID: "run-1", Text: plan, SessionID: "s1"})
	page = updated.(*pages.AgentPage)

	// The approval.
	updated, cmd := page.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	page = updated.(*pages.AgentPage)

	if cmd == nil {
		t.Fatal("the approval did not return a command")
	}

	if page.Mode() != agent.ModeAct {
		t.Errorf("the mode is %q, want act", page.Mode())
	}

	if runner.Mode() != agent.ModeAct {
		t.Errorf("the runner is in %q mode, want act", runner.Mode())
	}

	view := page.View().Content

	if !strings.Contains(view, "Implement this plan") {
		t.Errorf("the run does not implement the plan:\n%s", view)
	}
}
