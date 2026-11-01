// Package state holds the lightweight runtime state shared by the TUI
// pages.
//
// The state is intentionally *volatile*: it describes what the user is
// currently looking at (active page, hotkey context, connection health).
// It is never persisted. Anything that must survive a restart belongs in a
// PocketBase collection, accessed through the pbstore package.
package state

import (
	"time"

	"github.com/incurl/flint/internal/pbstore"
)

// Page identifies a screen of the application.
type Page string

const (
	// PageHome is the landing screen.
	PageHome Page = "home"
	// PageRecords lists the records of the active collection.
	PageRecords Page = "records"
	// PageDetail shows a single record.
	PageDetail Page = "detail"
	// PageSearch is the dedicated filter/search screen.
	PageSearch Page = "search"
	// PageSettings shows the effective configuration.
	PageSettings Page = "settings"
	// PageSync drives the remote -> local cache synchronisation.
	PageSync Page = "sync"
)

// AllPages lists the pages in their navigation order.
func AllPages() []Page {
	return []Page{
		PageHome,
		PageRecords,
		PageDetail,
		PageSearch,
		PageSettings,
		PageSync,
	}
}

// Title returns the human readable name of a page.
func (p Page) Title() string {
	switch p {
	case PageHome:
		return "Home"
	case PageRecords:
		return "Records"
	case PageDetail:
		return "Detail"
	case PageSearch:
		return "Search"
	case PageSettings:
		return "Settings"
	case PageSync:
		return "Sync"
	default:
		return string(p)
	}
}

// ConnectionState describes the health of the store connection.
type ConnectionState string

const (
	// ConnectionUnknown is the initial state.
	ConnectionUnknown ConnectionState = "unknown"
	// ConnectionChecking means a health check is in flight.
	ConnectionChecking ConnectionState = "checking"
	// ConnectionOK means the backend answered successfully.
	ConnectionOK ConnectionState = "ok"
	// ConnectionError means the backend could not be reached.
	ConnectionError ConnectionState = "error"
)

// SyncState describes the progress of a cache synchronisation.
type SyncState string

const (
	// SyncIdle means no sync is running.
	SyncIdle SyncState = "idle"
	// SyncRunning means a sync is in progress.
	SyncRunning SyncState = "running"
	// SyncDone means the last sync completed successfully.
	SyncDone SyncState = "done"
	// SyncFailed means the last sync failed.
	SyncFailed SyncState = "failed"
)

// Runtime is the mutable state shared between the root model and its pages.
//
// It must only be accessed from the Bubble Tea update loop, which is
// single-threaded, so no locking is required.
type Runtime struct {
	// Page is the currently displayed page.
	Page Page
	// PreviousPage is the page that was displayed before the current one.
	//
	// It is used by the "back" hotkey.
	PreviousPage Page
	// Collection is the name of the active collection.
	Collection string
	// Collections holds the collections discovered at startup.
	Collections []pbstore.Collection
	// Filter is the active PocketBase filter expression.
	Filter string
	// Sort is the active PocketBase sort expression.
	Sort string
	// PageNumber is the active pagination page number.
	//
	// It is named PageNumber to avoid clashing with the Page type.
	PageNumber int
	// PerPage is the page size used by the record list.
	PerPage int
	// SelectedID is the id of the record opened in the detail page.
	SelectedID string
	// Connection describes the health of the backend.
	Connection ConnectionState
	// ConnectionDetail holds the last health check message.
	ConnectionDetail string
	// LastCheck is the time of the last health check.
	LastCheck time.Time
	// Sync describes the progress of the last synchronisation.
	Sync SyncState
	// SyncProgress holds a human readable sync progress message.
	SyncProgress string
	// LastError holds the most recent store level error.
	LastError string
	// RawJSON toggles the raw JSON view of the detail page.
	RawJSON bool
}

// NewRuntime returns a runtime state initialised with sane defaults.
func NewRuntime() *Runtime {
	return &Runtime{
		Page:       PageHome,
		PageNumber: 1,
		PerPage:    30,
		Connection: ConnectionUnknown,
		Sync:       SyncIdle,
	}
}

// SetPage switches the active page and remembers the previous one.
//
// The "back" hotkey uses the remembered page to restore the previous view.
func (r *Runtime) SetPage(page Page) {
	if r.Page == page {
		return
	}

	r.PreviousPage = r.Page
	r.Page = page
}

// GoBack restores the previous page.
//
// It is a no-op when there is no previous page.
func (r *Runtime) GoBack() {
	if r.PreviousPage == "" {
		return
	}

	current := r.Page
	r.Page, r.PreviousPage = r.PreviousPage, current
}

// SetError records the last store level error.
func (r *Runtime) SetError(err error) {
	if err == nil {
		r.LastError = ""
		return
	}

	r.LastError = err.Error()
}
