package state_test

import (
	"testing"

	"github.com/incurl/flint/internal/state"
)

// TestNewRuntime verifies the default runtime state.
func TestNewRuntime(t *testing.T) {
	r := state.NewRuntime()

	if r.Page != state.PageHome {
		t.Errorf("Page = %q, want %q", r.Page, state.PageHome)
	}

	if r.PageNumber != 1 {
		t.Errorf("PageNumber = %d, want 1", r.PageNumber)
	}

	if r.PerPage != 30 {
		t.Errorf("PerPage = %d, want 30", r.PerPage)
	}

	if r.Connection != state.ConnectionUnknown {
		t.Errorf("Connection = %q, want %q", r.Connection, state.ConnectionUnknown)
	}

	if r.Sync != state.SyncIdle {
		t.Errorf("Sync = %q, want %q", r.Sync, state.SyncIdle)
	}
}

// TestSetPage verifies the navigation and the page history.
func TestSetPage(t *testing.T) {
	r := state.NewRuntime()

	r.SetPage(state.PageRecords)

	if r.Page != state.PageRecords {
		t.Errorf("Page = %q, want %q", r.Page, state.PageRecords)
	}

	if r.PreviousPage != state.PageHome {
		t.Errorf("PreviousPage = %q, want %q", r.PreviousPage, state.PageHome)
	}

	// Setting the same page must not change the history.
	r.SetPage(state.PageRecords)

	if r.PreviousPage != state.PageHome {
		t.Errorf("PreviousPage = %q after a repeated navigation, want %q", r.PreviousPage, state.PageHome)
	}
}

// TestGoBack verifies the back navigation.
func TestGoBack(t *testing.T) {
	r := state.NewRuntime()

	// There is no previous page yet.
	r.GoBack()

	if r.Page != state.PageHome {
		t.Errorf("Page = %q, want %q", r.Page, state.PageHome)
	}

	r.SetPage(state.PageRecords)
	r.SetPage(state.PageDetail)
	r.GoBack()

	if r.Page != state.PageRecords {
		t.Errorf("Page = %q, want %q", r.Page, state.PageRecords)
	}
}

// TestSetError verifies the error tracking.
func TestSetError(t *testing.T) {
	r := state.NewRuntime()

	if r.LastError != "" {
		t.Errorf("LastError = %q, want an empty string", r.LastError)
	}

	r.SetError(errTest)

	if r.LastError != errTest.Error() {
		t.Errorf("LastError = %q, want %q", r.LastError, errTest.Error())
	}

	r.SetError(nil)

	if r.LastError != "" {
		t.Errorf("LastError = %q, want an empty string", r.LastError)
	}
}

// TestPageTitle verifies the page titles.
func TestPageTitle(t *testing.T) {
	for _, page := range state.AllPages() {
		if page.Title() == "" {
			t.Errorf("Page(%q).Title() is empty", page)
		}
	}
}

// errTest is the error used by the tests.
var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }
