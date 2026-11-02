package components

import (
	"sync"

	"github.com/incurl/flint/internal/pbstore"
)

// CachedRenderer renders a record with a script, caching the result.
//
// The script is evaluated **only by [CachedRenderer.Preload]**, which
// the application calls from a command. The three
// [RecordRenderer] methods are pure cache lookups: they never evaluate
// the script, so the render loop never waits for the JavaScript
// runtime.
//
// A record that is not in the cache falls back to the Go renderer. That
// is the intended behaviour: the first frame of a new record shows the
// Go rendering, and the next frame — after the preload — shows the
// script rendering.
//
// The cache is keyed by the record id and the record update time: a
// record that changes is rendered again, and a record that does not
// change is not.
//
// The fallback renderer is used when the script is missing, fails or
// times out, so a broken script degrades to the Go rendering instead
// of breaking the screen.
type CachedRenderer struct {
	// script evaluates the record.
	script *ScriptRenderer
	// fallback renders the record when the script does not.
	fallback RecordRenderer

	// mu guards the cache.
	mu sync.RWMutex
	// cache holds the rendered records, keyed by the record id.
	cache map[string]cachedEntry
}

// cachedEntry is one cached rendering.
type cachedEntry struct {
	// updated is the update timestamp of the rendered record.
	updated string
	// result is the rendered outcome.
	result ScriptResult
	// ok reports whether the script succeeded.
	ok bool
}

// NewCachedRenderer creates a renderer that evaluates the script and
// caches the outcome.
//
// The fallback is used when the script is empty or fails.
func NewCachedRenderer(script string, fallback RecordRenderer) *CachedRenderer {
	if fallback == nil {
		fallback = &DefaultRecordRenderer{}
	}

	return &CachedRenderer{
		script:   NewScriptRenderer(script, fallback),
		fallback: fallback,
		cache:    map[string]cachedEntry{},
	}
}

// Title implements [RecordRenderer].
func (r *CachedRenderer) Title(rec *pbstore.Record) string {
	result, ok := r.rendered(rec)
	if !ok {
		return r.fallback.Title(rec)
	}

	return result.Title
}

// Summary implements [RecordRenderer].
func (r *CachedRenderer) Summary(rec *pbstore.Record) string {
	result, ok := r.rendered(rec)
	if !ok {
		return r.fallback.Summary(rec)
	}

	return result.Summary
}

// Fields implements [RecordRenderer].
func (r *CachedRenderer) Fields(rec *pbstore.Record) []FieldRow {
	result, ok := r.rendered(rec)
	if !ok {
		return r.fallback.Fields(rec)
	}

	return result.Fields
}

// rendered returns the cached rendering of the record.
//
// The second return value reports whether the script succeeded.
//
// It never evaluates the script: a missing entry means the record was
// not preloaded yet, and the caller falls back to the Go renderer.
func (r *CachedRenderer) rendered(rec *pbstore.Record) (ScriptResult, bool) {
	if rec == nil {
		return ScriptResult{}, false
	}

	r.mu.RLock()
	entry, cached := r.cache[rec.ID]
	r.mu.RUnlock()

	if !cached || entry.updated != rec.Updated {
		return ScriptResult{}, false
	}

	return entry.result, entry.ok
}

// Preload evaluates the script for the records that are not cached.
//
// It is the only method that evaluates the script, and it must be
// called from a command: it blocks for as long as the script takes,
// up to [ScriptTimeout] per record.
//
// It returns the number of records that were rendered, so that the
// caller can decide whether a re-render is needed.
func (r *CachedRenderer) Preload(records []*pbstore.Record) int {
	rendered := 0

	for _, rec := range records {
		if rec == nil {
			continue
		}

		if _, ok := r.rendered(rec); ok {
			continue
		}

		// The record may be cached as a failure, which is a result
		// too: it must not be evaluated on every preload.
		if r.cachedFailure(rec) {
			continue
		}

		result, err := r.script.evaluateWithTimeout(rec)
		if err != nil {
			// The failure is cached, so that a broken script is not
			// evaluated again for the same record.
			r.store(rec, cachedEntry{updated: rec.Updated, ok: false})

			continue
		}

		r.store(rec, cachedEntry{updated: rec.Updated, result: result, ok: true})

		rendered++
	}

	return rendered
}

// cachedFailure reports whether the record is cached as a failure.
//
// The update time is checked, so that a changed record is evaluated
// again.
func (r *CachedRenderer) cachedFailure(rec *pbstore.Record) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, cached := r.cache[rec.ID]

	return cached && entry.updated == rec.Updated
}

// store caches the rendering of the record.
func (r *CachedRenderer) store(rec *pbstore.Record, entry cachedEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache[rec.ID] = entry
}

// Validate compiles the script and checks that it defines flintRender.
//
// It does not render anything. The application calls it at startup, so
// that a broken script is reported once instead of silently falling
// back on every record.
func (r *CachedRenderer) Validate() error {
	return r.script.Validate()
}

// Invalidate drops the cached rendering of a record.
//
// The application calls it when a record changes, so that the next
// frame renders it again.
func (r *CachedRenderer) Invalidate(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.cache, id)
}

// InvalidateAll drops every cached rendering.
func (r *CachedRenderer) InvalidateAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache = map[string]cachedEntry{}
}

// Len returns the number of cached renderings.
func (r *CachedRenderer) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.cache)
}
