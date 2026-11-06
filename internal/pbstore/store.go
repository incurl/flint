// Package pbstore provides a stable, backend-agnostic access layer over
// PocketBase.
//
// The package is built on top of PocketBase's *native* primitives rather than
// reimplementing them:
//
//   - [github.com/pocketbase/dbx] expressions for filters and sorting;
//   - [github.com/pocketbase/pocketbase/core] record model, validation and
//     error types;
//   - [github.com/pocketbase/pocketbase/tools/search] filter/sort provider,
//     which is the exact same machinery the PocketBase admin UI and REST API
//     use;
//   - [github.com/pocketbase/pocketbase/core.App] Store() — PocketBase's own
//     concurrent-safe runtime key/value store, reused here as the
//     process-local cache registry.
//
// # Modes
//
// Two implementations of [Store] are provided:
//
//   - [Embedded] runs a full PocketBase instance inside the current process
//     and talks to its SQLite database directly;
//   - [Remote] talks to a PocketBase server over its REST API.
//
// Both share the same [Store] contract so that downstream domain code never
// needs to branch on the backend.
//
// # Data dictionaries and lookup caches
//
// Persistent lookup caches and controlled vocabularies are *not* stored in
// ad-hoc Go maps. They are stored as ordinary PocketBase collections and
// queried through [Store]. See [Store.EnsureDictionaryCollection] and
// [Store.SyncCollection] for the building blocks.
package pbstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/search"
)

// Mode identifies the backend flavour behind a [Store].
type Mode string

const (
	// ModeEmbedded is an in-process PocketBase instance backed by SQLite.
	ModeEmbedded Mode = "embedded"
	// ModeRemote is a PocketBase server reached over HTTP.
	ModeRemote Mode = "remote"
)

// String implements [fmt.Stringer].
func (m Mode) String() string { return string(m) }

// Errors returned by the package.
//
// Store implementations wrap the underlying PocketBase error where
// possible, so callers can use [errors.Is] with these sentinels.
var (
	// ErrNotFound is returned when a record or collection does not exist.
	ErrNotFound = errors.New("pbstore: not found")
	// ErrCollectionNotAllowed is returned when a collection is not part of
	// the configured allowlist.
	ErrCollectionNotAllowed = errors.New("pbstore: collection is not allowed")
	// ErrInvalidFilter is returned when a filter expression cannot be
	// parsed by the PocketBase filter grammar.
	ErrInvalidFilter = errors.New("pbstore: invalid filter")
	// ErrInvalidSort is returned when a sort expression cannot be parsed.
	ErrInvalidSort = errors.New("pbstore: invalid sort")
	// ErrClosed is returned when the store is used after being closed.
	ErrClosed = errors.New("pbstore: store is closed")
	// ErrNotConnected is returned when the remote backend cannot reach the
	// configured server.
	ErrNotConnected = errors.New("pbstore: not connected")
)

// Collection describes a collection exposed by a [Store].
type Collection struct {
	// ID is the collection identifier.
	ID string `json:"id"`
	// Name is the collection name.
	Name string `json:"name"`
	// Type is the collection type (base, auth or view).
	Type string `json:"type"`
}

// IsSystemCollection reports whether a collection name is reserved for the
// internals of PocketBase or of the application.
//
// PocketBase keeps its own collections behind a leading underscore — the
// superusers, the authentication helpers, the request logs — and the template
// keeps its preferences and its conversations the same way
// ([PreferencesCollection], `_flint_sessions`, `_flint_messages`).
//
// They are not data the user keeps: a listing that is read as the data of the
// application — the agent's view of the store, for instance — leaves them out.
// The predicate is a property of the name, so it holds for both backends: the
// remote one does not report the system flag.
func IsSystemCollection(name string) bool {
	return strings.HasPrefix(name, "_")
}

// Record is a backend-agnostic view over a PocketBase record.
//
// It intentionally exposes the exported field data as a plain map so that
// domain code can work with arbitrary schemas without knowing the concrete
// PocketBase collection layout.
type Record struct {
	// ID is the record identifier.
	ID string `json:"id"`
	// CollectionID is the identifier of the owning collection.
	CollectionID string `json:"collectionId"`
	// CollectionName is the name of the owning collection.
	CollectionName string `json:"collectionName"`
	// Fields holds the exported record field data.
	Fields map[string]any `json:"fields"`
	// Expand holds the resolved relation expansions, if any.
	Expand map[string]any `json:"expand,omitempty"`
	// Created is the record creation timestamp.
	Created string `json:"created,omitempty"`
	// Updated is the record last-update timestamp.
	Updated string `json:"updated,omitempty"`
}

// Field returns the raw value of the named field.
func (r *Record) Field(name string) (any, bool) {
	if r == nil || r.Fields == nil {
		return nil, false
	}

	v, ok := r.Fields[name]

	return v, ok
}

// StringField returns the named field as a string, or "" when missing.
func (r *Record) StringField(name string) string {
	v, ok := r.Field(name)
	if !ok || v == nil {
		return ""
	}

	if s, ok := v.(string); ok {
		return s
	}

	return fmt.Sprintf("%v", v)
}

// JSON returns the record field data as an indented JSON document.
//
// It is used by the raw JSON viewer and editor, which operate on the
// unstructured payload of a record rather than on individual fields.
func (r *Record) JSON() (string, error) {
	return marshalIndent(r.Fields)
}

// TimeField returns the named field as a time.
//
// The field must hold a date: a PocketBase date field, or one of the
// timestamps the record itself carries. A field that holds something
// else is an error, so that a caller does not silently read a zero
// time as a real date.
func (r *Record) TimeField(name string) (time.Time, error) {
	v, ok := r.Field(name)
	if !ok || v == nil {
		return time.Time{}, fmt.Errorf("pbstore: the field %q is missing", name)
	}

	switch value := v.(type) {
	case time.Time:
		return value, nil
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}, fmt.Errorf("pbstore: the field %q is not a date: %w", name, err)
		}

		return parsed, nil
	default:
		return time.Time{}, fmt.Errorf("pbstore: the field %q is not a date", name)
	}
}

// Query describes a record listing request.
//
// The fields mirror the native PocketBase search semantics so that a query
// can be translated 1:1 into a REST call for the remote backend and into a
// [search.Provider] execution for the embedded backend.
type Query struct {
	// Collection is the target collection name or identifier.
	Collection string
	// Filter is a PocketBase filter expression, eg. `title ~ "go"`.
	Filter string
	// Sort is a PocketBase sort expression, eg. `-created,name`.
	Sort string
	// Page is the 1-based page number.
	Page int
	// PerPage is the number of records per page.
	PerPage int
	// Expand is a comma separated list of relation fields to expand.
	Expand string
	// SkipTotal disables the expensive total-count query.
	SkipTotal bool
	// Fields limits the returned fields (PocketBase "fields" param).
	Fields string
}

// PageResult is a paginated slice of records.
type PageResult struct {
	// Records holds the page content.
	Records []*Record
	// Page is the 1-based page number.
	Page int
	// PerPage is the page size.
	PerPage int
	// TotalItems is the total number of matching records.
	TotalItems int
	// TotalPages is the total number of pages.
	TotalPages int
	// Truncated reports that the result is incomplete.
	//
	// It is set when the records were filtered locally and the bound of
	// the local evaluation was reached, so that the total counts the
	// fetched records only and not the whole collection.
	Truncated bool
}

// SortFields parses the provided sort expression into native PocketBase
// [search.SortField] values.
//
// It reuses [search.ParseSortFromString] so the accepted syntax is exactly
// the one used by the PocketBase REST API (`-field` for descending,
// `field` for ascending, comma separated).
func SortFields(sort string) []search.SortField {
	if sort == "" {
		return nil
	}

	return search.ParseSortFromString(sort)
}

// FilterExpr builds a native PocketBase WHERE expression out of the provided
// filter string.
//
// The returned expression can be embedded into a [dbx.SelectQuery] with
// `AndWhere`, exactly like the PocketBase admin UI does. An empty filter
// yields a nil expression and no error.
func FilterExpr(filter string, resolver search.FieldResolver, params ...dbx.Params) (dbx.Expression, error) {
	if filter == "" {
		return nil, nil
	}

	if resolver == nil {
		resolver = search.NewSimpleFieldResolver(`^\w+[\w\.]*$`)
	}

	expr, err := search.FilterData(filter).BuildExpr(resolver, params...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFilter, err)
	}

	return expr, nil
}

// Store is the stable contract every downstream project depends on.
//
// Implementations must be safe for concurrent use: Bubble Tea commands
// execute in background goroutines and may call the store while the render
// loop is active.
//
// All methods that perform I/O accept a [context.Context] so that
// long-running queries can be cancelled during shutdown.
type Store interface {
	// Mode reports which backend backs the store.
	Mode() Mode

	// HealthCheck verifies that the backend is reachable and usable.
	//
	// It returns a human readable status and an error when the backend is
	// not operational.
	HealthCheck(ctx context.Context) (string, error)

	// Collections lists the collections visible to the current credentials.
	//
	// When an allowlist is configured, only allowlisted collections are
	// returned.
	Collections(ctx context.Context) ([]Collection, error)

	// List returns a page of records matching the provided query.
	//
	// The query filter/sort expressions use the native PocketBase grammar.
	List(ctx context.Context, q Query) (*PageResult, error)

	// Get returns a single record by id.
	//
	// The id is normalised by the store, so that a short id from a
	// remote source finds the record the sync wrote.
	Get(ctx context.Context, collection, id string) (*Record, error)

	// FindFirst returns the first record matching the provided filter.
	FindFirst(ctx context.Context, collection, filter string) (*Record, error)

	// Create inserts a new record with the provided field data.
	Create(ctx context.Context, collection string, data map[string]any) (*Record, error)

	// Update merges the provided field data into an existing record.
	//
	// The fields that are not present in data are left untouched. The
	// id is normalised by the store, exactly like the one of [Store.Get].
	Update(ctx context.Context, collection, id string, data map[string]any) (*Record, error)

	// Delete removes a record.
	//
	// The id is normalised by the store, exactly like the one of
	// [Store.Get].
	Delete(ctx context.Context, collection, id string) error

	// Count returns the number of records matching the provided filter.
	Count(ctx context.Context, collection, filter string) (int, error)

	// EnsureDictionaryCollection creates a base collection for a controlled
	// vocabulary / lookup dictionary if it does not exist yet.
	//
	// The returned collection is the one stored in the backend, which makes
	// the call idempotent and safe to run on every start.
	//
	// Only the embedded backend can create collections; the remote backend
	// returns an error.
	EnsureDictionaryCollection(ctx context.Context, name string, fields []DictionaryField) (Collection, error)

	// SyncCollection copies records from a remote PocketBase server into the
	// local embedded store.
	//
	// It is an explicit, user-triggered operation: nothing syncs in the
	// background. Records are matched by id so that a repeated sync updates
	// the existing local copy instead of duplicating it.
	SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error)

	// EnsureCollection creates a base collection with the given fields
	// if it does not exist yet.
	//
	// It is the call the application uses to create the collections it
	// needs at startup. The remote backend cannot create a collection:
	// there, the collections are created by a migration on the server.
	EnsureCollection(ctx context.Context, name string, fields []CollectionField) (Collection, error)

	// CollectionSchema returns the fields of a collection.
	//
	// It is the shape of the collection, and not its records: the
	// names of the fields, their types, and whether they are required.
	// The system fields of PocketBase — the id, the creation date, the
	// update date — are left out: they are not fields a caller writes.
	//
	// A collection that does not exist is an error, and not an empty
	// schema: an empty answer for a collection that is not there would
	// be read as a collection with no fields.
	CollectionSchema(ctx context.Context, name string) ([]CollectionField, error)

	// Close releases the resources held by the store.
	//
	// It is safe to call Close more than once.
	Close() error
}

// CollectionField describes a single field of a collection.
//
// It is the shape [Store.EnsureCollection] takes, so that a downstream
// project can create its own collections through the same call.
type CollectionField struct {
	// Name is the field name.
	Name string
	// Type is one of: text, editor, json, number, bool, date, select.
	Type string
	// Required marks the field as required.
	Required bool
	// Max is the maximum length of a text field.
	Max int
	// Options lists the allowed values for select fields.
	Options []string
}

// DictionaryField describes a single field of a dictionary collection.
//
// The set of field types intentionally mirrors the small subset of PocketBase
// field types that make sense for a flat vocabulary entry.
type DictionaryField struct {
	// Name is the field name.
	Name string
	// Type is one of: text, editor, json, number, bool, date, select.
	Type string
	// Required marks the field as required.
	Required bool
	// Options lists the allowed values for select fields.
	Options []string
}

// Dictionary field types supported by [Store.EnsureDictionaryCollection].
const (
	FieldTypeText   = "text"
	FieldTypeEditor = "editor"
	FieldTypeJSON   = "json"
	FieldTypeNumber = "number"
	FieldTypeBool   = "bool"
	FieldTypeDate   = "date"
	FieldTypeSelect = "select"
	// FieldTypeAutodate is a timestamp the backend fills in.
	//
	// The field name decides when it is filled: "created" is set when
	// the record is created, "updated" when it is changed.
	FieldTypeAutodate = "autodate"
)

// SyncRequest describes a cache/dictionary sync operation.
type SyncRequest struct {
	// RemoteCollection is the source collection on the remote server.
	RemoteCollection string
	// LocalCollection is the destination collection in the embedded store.
	//
	// Defaults to RemoteCollection when empty.
	LocalCollection string
	// Filter limits the synced records (native PocketBase filter syntax).
	Filter string
	// BatchSize is the number of records fetched per request.
	//
	// Defaults to [DefaultSyncBatchSize] when zero.
	BatchSize int
	// MaxRecords caps the total number of synced records.
	//
	// Zero means unlimited.
	MaxRecords int
	// Since restricts the sync to records updated after the given time.
	Since *time.Time
}

// DefaultSyncBatchSize is the default number of records fetched per remote
// request during a sync.
const DefaultSyncBatchSize = 200

// SyncReport summarises the outcome of a [Store.SyncCollection] call.
type SyncReport struct {
	// Collection is the local collection the records were written to.
	Collection string
	// Fetched is the number of records read from the remote source.
	Fetched int
	// Created is the number of new local records.
	Created int
	// Updated is the number of existing local records that were refreshed.
	Updated int
	// Skipped is the number of records that were left untouched.
	Skipped int
	// Duration is the wall-clock time the sync took.
	Duration time.Duration
}

// recordFromCore converts a native PocketBase record into the
// backend-agnostic [Record] view.
//
// Custom data is exported so that arbitrary, non-schema JSON payloads are
// preserved and visible to the JSON viewer.
func recordFromCore(rec *core.Record) *Record {
	if rec == nil {
		return nil
	}

	out := &Record{
		ID:             rec.Id,
		CollectionID:   rec.Collection().Id,
		CollectionName: rec.Collection().Name,
		Fields:         make(map[string]any),
		Expand:         rec.Expand(),
	}

	// PublicExport only returns schema fields, so the raw field data is
	// merged in to also expose custom (non-schema) values.
	for k, v := range rec.FieldsData() {
		out.Fields[k] = v
	}

	for k, v := range rec.CustomData() {
		if _, ok := out.Fields[k]; !ok {
			out.Fields[k] = v
		}
	}

	if v, ok := out.Fields[core.FieldNameId]; ok {
		out.ID = fmt.Sprintf("%v", v)
	}

	if v, ok := out.Fields[core.FieldNameCollectionId]; ok {
		out.CollectionID = fmt.Sprintf("%v", v)
	}

	if v, ok := out.Fields[core.FieldNameCollectionName]; ok {
		out.CollectionName = fmt.Sprintf("%v", v)
	}

	if v, ok := out.Fields[fieldNameCreated]; ok {
		out.Created = fmt.Sprintf("%v", v)
	}

	if v, ok := out.Fields[fieldNameUpdated]; ok {
		out.Updated = fmt.Sprintf("%v", v)
	}

	return out
}
