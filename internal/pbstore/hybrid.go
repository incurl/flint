package pbstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pocketbase/pocketbase/tools/search"

	"github.com/incurl/flint/internal/jsonfilter"
)

// Hybrid wraps a [Store] and evaluates the JSON filters that the native
// PocketBase grammar cannot express.
//
// The native grammar reaches into the nested objects of a JSON field, so
// `meta.lang = "go"` and `meta.level > 2` are evaluated by the database
// itself. It cannot reach into an array: the expression parser rejects
// the `[` character, and an array field compared with `=` matches
// nothing.
//
// The hybrid routes the two kinds of expression to the layer that can
// handle them:
//
//	meta.lang = "go"        the database, as usual
//	meta.tags.#(=="cli")    locally, over the fetched records
//
// The local evaluation is bounded: it never fetches more than
// [Hybrid.MaxLocalRecords] records, so a filter over a large collection
// cannot pull the whole dataset into memory. When the bound is reached
// the result is truncated and reported through [PageResult.Truncated].
type Hybrid struct {
	// inner is the wrapped store.
	inner Store
	// maxLocalRecords bounds the number of records the local evaluation
	// fetches.
	maxLocalRecords int
}

// DefaultMaxLocalRecords is the default bound of the local evaluation.
const DefaultMaxLocalRecords = 1000

// HybridConfig configures a [Hybrid].
type HybridConfig struct {
	// MaxLocalRecords bounds the number of records the local evaluation
	// fetches.
	//
	// It defaults to [DefaultMaxLocalRecords]. A value below 1 falls back
	// to the default.
	MaxLocalRecords int
}

// NewHybrid wraps a store with the hybrid JSON filter evaluation.
//
// The wrapped store keeps its own mode: a hybrid over a remote store is
// still a remote store.
func NewHybrid(inner Store, config HybridConfig) *Hybrid {
	maxRecords := config.MaxLocalRecords
	if maxRecords < 1 {
		maxRecords = DefaultMaxLocalRecords
	}

	return &Hybrid{inner: inner, maxLocalRecords: maxRecords}
}

// Mode reports which backend backs the store.
func (h *Hybrid) Mode() Mode { return h.inner.Mode() }

// HealthCheck verifies that the backend is reachable and usable.
func (h *Hybrid) HealthCheck(ctx context.Context) (string, error) {
	return h.inner.HealthCheck(ctx)
}

// Collections lists the collections visible to the current credentials.
func (h *Hybrid) Collections(ctx context.Context) ([]Collection, error) {
	return h.inner.Collections(ctx)
}

// List returns a page of records matching the provided query.
//
// A filter that the native grammar can evaluate is passed straight
// through to the wrapped store. A filter that needs the local evaluation
// is applied to the records fetched by the wrapped store.
func (h *Hybrid) List(ctx context.Context, q Query) (*PageResult, error) {
	if !jsonfilter.NeedsLocal(q.Filter) {
		return h.inner.List(ctx, q)
	}

	return h.listLocal(ctx, q)
}

// listLocal evaluates a filter over the records fetched by the wrapped
// store.
func (h *Hybrid) listLocal(ctx context.Context, q Query) (*PageResult, error) {
	filter, err := jsonfilter.Parse(q.Filter)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFilter, err)
	}

	// The whole set is fetched, up to the bound, and then filtered
	// locally.
	fetch := q
	fetch.Filter = ""
	fetch.Page = 1
	fetch.PerPage = h.maxLocalRecords
	fetch.SkipTotal = false

	page, err := h.inner.List(ctx, fetch)
	if err != nil {
		return nil, err
	}

	matched := make([]*Record, 0, len(page.Records))

	for _, record := range page.Records {
		if h.matchRecord(filter, record) {
			matched = append(matched, record)
		}
	}

	truncated := len(page.Records) >= h.maxLocalRecords

	// The page is cut out of the matched records.
	perPage := q.PerPage
	if perPage < 1 {
		perPage = search.DefaultPerPage
	}

	total := len(matched)

	start := (q.Page - 1) * perPage
	if start < 0 {
		start = 0
	}

	end := start + perPage
	if end > total {
		end = total
	}

	return &PageResult{
		Records:    matched[start:end],
		Page:       q.Page,
		PerPage:    perPage,
		TotalItems: total,
		TotalPages: (total + perPage - 1) / perPage,
		Truncated:  truncated,
	}, nil
}

// matchRecord reports whether a record satisfies the filter.
//
// The filter is evaluated against the JSON document of the record, so
// that the path of the expression is relative to the record itself.
func (h *Hybrid) matchRecord(filter *jsonfilter.Filter, record *Record) bool {
	if record == nil {
		return false
	}

	document, err := json.Marshal(record.Fields)
	if err != nil {
		return false
	}

	return filter.Match(string(document))
}

// Get returns a single record by id.
func (h *Hybrid) Get(ctx context.Context, collection, id string) (*Record, error) {
	return h.inner.Get(ctx, collection, id)
}

// FindFirst returns the first record matching the provided filter.
//
// A filter that the native grammar cannot evaluate falls back to a
// local evaluation over the collection.
func (h *Hybrid) FindFirst(ctx context.Context, collection, filter string) (*Record, error) {
	if !jsonfilter.NeedsLocal(filter) {
		return h.inner.FindFirst(ctx, collection, filter)
	}

	page, err := h.listLocal(ctx, Query{
		Collection: collection,
		Filter:     filter,
		Page:       1,
		PerPage:    1,
	})
	if err != nil {
		return nil, err
	}

	if len(page.Records) == 0 {
		return nil, ErrNotFound
	}

	return page.Records[0], nil
}

// Create inserts a new record with the provided field data.
func (h *Hybrid) Create(ctx context.Context, collection string, data map[string]any) (*Record, error) {
	return h.inner.Create(ctx, collection, data)
}

// Update merges the provided field data into an existing record.
func (h *Hybrid) Update(ctx context.Context, collection, id string, data map[string]any) (*Record, error) {
	return h.inner.Update(ctx, collection, id, data)
}

// Delete removes a record.
func (h *Hybrid) Delete(ctx context.Context, collection, id string) error {
	return h.inner.Delete(ctx, collection, id)
}

// Count returns the number of records matching the provided filter.
//
// A filter that the native grammar cannot evaluate falls back to a
// local evaluation over the bounded set of records.
func (h *Hybrid) Count(ctx context.Context, collection, filter string) (int, error) {
	if !jsonfilter.NeedsLocal(filter) {
		return h.inner.Count(ctx, collection, filter)
	}

	page, err := h.listLocal(ctx, Query{
		Collection: collection,
		Filter:     filter,
		Page:       1,
		PerPage:    h.maxLocalRecords,
	})
	if err != nil {
		return 0, err
	}

	return page.TotalItems, nil
}

// EnsureDictionaryCollection creates a base collection for a controlled
// vocabulary / lookup dictionary if it does not exist yet.
func (h *Hybrid) EnsureDictionaryCollection(ctx context.Context, name string, fields []DictionaryField) (Collection, error) {
	return h.inner.EnsureDictionaryCollection(ctx, name, fields)
}

// SyncCollection copies records from a remote PocketBase server into the
// local embedded store.
func (h *Hybrid) SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error) {
	return h.inner.SyncCollection(ctx, req)
}

// Close releases the resources held by the store.
func (h *Hybrid) Close() error { return h.inner.Close() }

// Unwrap returns the wrapped store.
func (h *Hybrid) Unwrap() Store { return h.inner }
