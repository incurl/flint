package components

import "github.com/incurl/flint/internal/pbstore"

// Preloader is a renderer that computes its display data ahead of the
// render loop.
//
// A renderer that evaluates a script implements it: the script must not
// run while a frame is rendered, because it would block the loop. The
// preload is therefore called from a command, and the render methods
// only read the cached outcome.
type Preloader interface {
	RecordRenderer

	// Preload renders the records that are not rendered yet.
	//
	// It returns the number of records that were rendered, which is 0
	// when every record was already cached.
	Preload(records []*pbstore.Record) int
}

// Preload renders the records with the renderer, when it preloads.
//
// It is a no-op for a renderer that does not preload, so a page can
// call it unconditionally.
//
// It must be called from a command: it blocks for as long as the
// renderer takes.
func Preload(renderer RecordRenderer, records []*pbstore.Record) int {
	preloader, ok := renderer.(Preloader)
	if !ok {
		return 0
	}

	return preloader.Preload(records)
}
