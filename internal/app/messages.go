// Package app holds the root Bubble Tea model of the application.
//
// The root model owns the global concerns: the page routing, the global
// hotkey layer, the persistent status bar and the toast notifications.
// The page specific concerns live in the internal/pages package.
package app

import (
	"github.com/incurl/flint/internal/components"
	"github.com/incurl/flint/internal/pbstore"
)

// HealthMsg is the message carrying the result of a store health check.
type HealthMsg struct {
	// Status is the human readable status.
	Status string
	// Err holds the error, when the check failed.
	Err error
}

// CollectionsMsg is the message carrying the discovered collections.
type CollectionsMsg struct {
	// Collections holds the discovered collections.
	Collections []pbstore.Collection
	// Err holds the error, when the discovery failed.
	Err error
}

// SyncStartedMsg is emitted when a synchronisation starts.
type SyncStartedMsg struct {
	// Collection is the collection being synchronised.
	Collection string
}

// SyncFinishedMsg is the message carrying the result of a synchronisation.
type SyncFinishedMsg struct {
	// Report holds the sync summary.
	Report *pbstore.SyncReport
	// Err holds the error, when the sync failed.
	Err error
}

// Toast is a convenience alias for the toast notification message.
type Toast = components.ToastMsg
