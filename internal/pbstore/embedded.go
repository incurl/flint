package pbstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/search"
	"github.com/pocketbase/pocketbase/tools/store"
)

// EmbeddedConfig configures an [Embedded] store.
type EmbeddedConfig struct {
	// DataDir is the PocketBase data directory.
	//
	// It holds the SQLite database and the uploaded files.
	DataDir string
	// EncryptionEnv is the name of the env variable holding the 32
	// character encryption key used to encrypt the app settings.
	//
	// Leave empty to disable settings encryption.
	EncryptionEnv string
	// Dev enables the PocketBase development mode (verbose logs and SQL
	// statements).
	//
	// Keep it disabled in production: the TUI render loop must not be
	// flooded with query logs.
	Dev bool
	// QueryTimeout limits the duration of a single SELECT query.
	//
	// Zero falls back to the PocketBase default.
	QueryTimeout time.Duration
	// Collections restricts the collections that can be queried.
	//
	// An empty list allows every collection.
	Collections []string
	// Migrations holds the application migrations executed on startup.
	//
	// They are registered into [core.AppMigrations] and applied by the
	// embedded store during [OpenEmbedded].
	Migrations []core.Migration
	// SyncRemoteURL is the base URL of the PocketBase server used as the
	// source of the [Embedded.SyncCollection] calls.
	//
	// It is usually set from the `remote.url` configuration value so that
	// the embedded store can pull a remote reference dataset into the
	// local database.
	SyncRemoteURL string
	// SyncRemoteToken authenticates the requests made to the sync source.
	//
	// It is required when the remote collections have list rules that are
	// not "public". It is usually set from the `remote.token`
	// configuration value.
	SyncRemoteToken string
	// Logger receives the PocketBase debug logs.
	//
	// When nil the PocketBase default logger is used.
	Logger *slog.Logger
}

// Embedded is a [Store] backed by an in-process PocketBase instance.
//
// It owns a real PocketBase application, so records are persisted in a
// regular SQLite database and benefit from the native validation, hooks and
// query machinery.
//
// The PocketBase runtime store ([core.App] Store()) is reused as the
// process-local cache registry: it is a concurrent-safe key/value store and
// is exactly what PocketBase itself uses to memoise collections and other
// hot data.
type Embedded struct {
	app     core.App
	config  EmbeddedConfig
	allowed map[string]bool

	mu     sync.RWMutex
	closed bool
}

// Compile-time check that Embedded satisfies the Store contract.
var _ Store = (*Embedded)(nil)

// OpenEmbedded creates and bootstraps an [Embedded] store.
//
// The provided PocketBase app instance is adopted by the store, which means
// the caller must not use it directly afterwards.
func OpenEmbedded(app core.App, config EmbeddedConfig) (*Embedded, error) {
	if app == nil {
		return nil, errors.New("pbstore: nil pocketbase app")
	}

	if config.DataDir == "" {
		return nil, errors.New("pbstore: embedded data dir is required")
	}

	// Register the user provided migrations before the app is bootstrapped
	// so that they are applied together with the system migrations.
	for _, m := range config.Migrations {
		core.AppMigrations.Add(&m)
	}

	if err := app.Bootstrap(); err != nil {
		return nil, fmt.Errorf("pbstore: bootstrap embedded store: %w", err)
	}

	// Apply the system + app migrations (creating the collections needed by
	// the dictionary/reference data).
	if err := app.RunAllMigrations(); err != nil {
		_ = app.ClearBootstrap()
		return nil, fmt.Errorf("pbstore: run embedded migrations: %w", err)
	}

	return &Embedded{
		app:     app,
		config:  config,
		allowed: toSet(config.Collections),
	}, nil
}

// Mode implements [Store].
func (s *Embedded) Mode() Mode { return ModeEmbedded }

// App exposes the underlying PocketBase application.
//
// It is the escape hatch for downstream code that needs a native PocketBase
// API (hooks, collection management, raw dbx queries, ...).
func (s *Embedded) App() core.App { return s.app }

// RuntimeStore exposes the native PocketBase runtime store.
//
// The returned store is the very same instance PocketBase uses internally
// (see [core.App] Store()). It is a concurrent-safe key/value store and is
// meant for *volatile* runtime data only — persistent lookup caches belong
// in a PocketBase collection, not here.
func (s *Embedded) RuntimeStore() *store.Store[string, any] { return s.app.Store() }

// HealthCheck implements [Store].
func (s *Embedded) HealthCheck(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "cancelled", err
	}

	if s.isClosed() {
		return "closed", ErrClosed
	}

	// A trivial query proves that the db connection is alive.
	collections, err := s.app.FindAllCollections()
	if err != nil {
		return "unhealthy", fmt.Errorf("pbstore: health check failed: %w", err)
	}

	return fmt.Sprintf("ok (%d collections)", len(collections)), nil
}

// Collections implements [Store].
func (s *Embedded) Collections(ctx context.Context) ([]Collection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.isClosed() {
		return nil, ErrClosed
	}

	models, err := s.app.FindAllCollections()
	if err != nil {
		return nil, fmt.Errorf("pbstore: list collections: %w", err)
	}

	out := make([]Collection, 0, len(models))
	for _, m := range models {
		if !s.isAllowed(m.Name) {
			continue
		}

		out = append(out, Collection{ID: m.Id, Name: m.Name, Type: m.Type})
	}

	return out, nil
}

// List implements [Store].
//
// The listing is executed with the native PocketBase search provider, so
// filters, sorting and pagination behave exactly like in the PocketBase
// admin UI.
func (s *Embedded) List(ctx context.Context, q Query) (*PageResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.isClosed() {
		return nil, ErrClosed
	}

	if q.Collection == "" {
		return nil, errors.New("pbstore: collection is required")
	}

	if !s.isAllowed(q.Collection) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, q.Collection)
	}

	collection, err := s.app.FindCachedCollectionByNameOrId(q.Collection)
	if err != nil {
		return nil, fmt.Errorf("pbstore: resolve collection %q: %w", q.Collection, err)
	}

	resolver := core.NewRecordFieldResolver(s.app, collection, nil, true)

	query := s.app.RecordQuery(collection)
	provider := search.NewProvider(resolver).Query(query)

	// Reuse the native URL parameter parsing so that the embedded backend
	// and the remote backend interpret the very same query strings.
	if err := provider.Parse(QueryParams(q).Encode()); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidFilter, err)
	}

	provider.SkipTotal(q.SkipTotal)

	records := []*core.Record{}

	result, err := provider.Exec(&records)
	if err != nil {
		return nil, fmt.Errorf("pbstore: list %q: %w", q.Collection, err)
	}

	// Resolve the relation expansions.
	if expands := ExpandList(q.Expand); len(expands) > 0 {
		// ExpandRecords reports a per-relation error map rather than a
		// single error, so the failures are joined together.
		if expandErrs := s.app.ExpandRecords(records, expands, nil); len(expandErrs) > 0 {
			return nil, fmt.Errorf("pbstore: expand %q: %w", q.Collection, joinErrors(expandErrs))
		}
	}

	out := &PageResult{
		Records:    make([]*Record, 0, len(records)),
		Page:       result.Page,
		PerPage:    result.PerPage,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	}
	for _, rec := range records {
		out.Records = append(out.Records, recordFromCore(rec))
	}

	return out, nil
}

// Get implements [Store].
func (s *Embedded) Get(ctx context.Context, collection, id string) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.isClosed() {
		return nil, ErrClosed
	}

	if !s.isAllowed(collection) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	rec, err := s.app.FindRecordById(collection, normalizeID(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, collection, id)
		}

		return nil, fmt.Errorf("pbstore: get %s/%s: %w", collection, id, err)
	}

	return recordFromCore(rec), nil
}

// FindFirst implements [Store].
func (s *Embedded) FindFirst(ctx context.Context, collection, filter string) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.isClosed() {
		return nil, ErrClosed
	}

	if !s.isAllowed(collection) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	rec, err := s.app.FindFirstRecordByFilter(collection, filter)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, collection)
		}

		return nil, fmt.Errorf("pbstore: find first %q: %w", collection, err)
	}

	return recordFromCore(rec), nil
}

// Create implements [Store].
func (s *Embedded) Create(ctx context.Context, collection string, data map[string]any) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.isClosed() {
		return nil, ErrClosed
	}

	if !s.isAllowed(collection) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	model, err := s.app.FindCachedCollectionByNameOrId(collection)
	if err != nil {
		return nil, fmt.Errorf("pbstore: resolve collection %q: %w", collection, err)
	}

	rec := core.NewRecord(model)
	rec.Load(data)

	if err := s.app.Save(rec); err != nil {
		return nil, fmt.Errorf("pbstore: create %q: %w", collection, err)
	}

	return recordFromCore(rec), nil
}

// Update implements [Store].
func (s *Embedded) Update(ctx context.Context, collection, id string, data map[string]any) (*Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.isClosed() {
		return nil, ErrClosed
	}

	if !s.isAllowed(collection) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	rec, err := s.app.FindRecordById(collection, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s/%s", ErrNotFound, collection, id)
		}

		return nil, fmt.Errorf("pbstore: get %s/%s: %w", collection, id, err)
	}

	rec.Load(data)

	if err := s.app.Save(rec); err != nil {
		return nil, fmt.Errorf("pbstore: update %s/%s: %w", collection, id, err)
	}

	return recordFromCore(rec), nil
}

// Delete implements [Store].
func (s *Embedded) Delete(ctx context.Context, collection, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if s.isClosed() {
		return ErrClosed
	}

	if !s.isAllowed(collection) {
		return fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	rec, err := s.app.FindRecordById(collection, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %s/%s", ErrNotFound, collection, id)
		}

		return fmt.Errorf("pbstore: get %s/%s: %w", collection, id, err)
	}

	if err := s.app.Delete(rec); err != nil {
		return fmt.Errorf("pbstore: delete %s/%s: %w", collection, id, err)
	}

	return nil
}

// Count implements [Store].
func (s *Embedded) Count(ctx context.Context, collection, filter string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}

	if s.isClosed() {
		return 0, ErrClosed
	}

	if !s.isAllowed(collection) {
		return 0, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, collection)
	}

	result, err := s.List(ctx, Query{
		Collection: collection,
		Filter:     filter,
		PerPage:    1,
	})
	if err != nil {
		return 0, err
	}

	return result.TotalItems, nil
}

// EnsureDictionaryCollection implements [Store].
//
// The collection is created with the native PocketBase collection model, so
// it behaves like any hand-crafted collection created through the admin UI:
// it has system fields, indexes and validation.
func (s *Embedded) EnsureDictionaryCollection(ctx context.Context, name string, fields []DictionaryField) (Collection, error) {
	if err := ctx.Err(); err != nil {
		return Collection{}, err
	}

	if s.isClosed() {
		return Collection{}, ErrClosed
	}

	if name == "" {
		return Collection{}, errors.New("pbstore: dictionary collection name is required")
	}

	// Reuse the existing collection when it is already there.
	if existing, err := s.app.FindCachedCollectionByNameOrId(name); err == nil && existing != nil {
		return Collection{ID: existing.Id, Name: existing.Name, Type: existing.Type}, nil
	}

	collection := core.NewBaseCollection(name)
	collection.Fields.Add(
		&core.TextField{Name: "key", Required: true, Max: 200},
		&core.TextField{Name: "label", Max: 500},
		&core.TextField{Name: "description", Max: 5000},
	)

	for _, f := range fields {
		field, err := buildDictionaryField(f)
		if err != nil {
			return Collection{}, err
		}

		collection.Fields.Add(field)
	}

	if err := s.app.Save(collection); err != nil {
		return Collection{}, fmt.Errorf("pbstore: create dictionary collection %q: %w", name, err)
	}

	return Collection{ID: collection.Id, Name: collection.Name, Type: collection.Type}, nil
}

// buildDictionaryField converts a [DictionaryField] description into a
// native PocketBase field.
func buildDictionaryField(f DictionaryField) (core.Field, error) {
	switch f.Type {
	case FieldTypeText, "":
		return &core.TextField{Name: f.Name, Required: f.Required}, nil
	case FieldTypeEditor:
		return &core.EditorField{Name: f.Name, Required: f.Required}, nil
	case FieldTypeJSON:
		return &core.JSONField{Name: f.Name, Required: f.Required}, nil
	case FieldTypeNumber:
		return &core.NumberField{Name: f.Name, Required: f.Required}, nil
	case FieldTypeBool:
		return &core.BoolField{Name: f.Name, Required: f.Required}, nil
	case FieldTypeDate:
		return &core.DateField{Name: f.Name, Required: f.Required}, nil
	case FieldTypeSelect:
		return &core.SelectField{Name: f.Name, Required: f.Required, Values: f.Options}, nil
	default:
		return nil, fmt.Errorf("pbstore: unsupported dictionary field type %q", f.Type)
	}
}

// SyncCollection implements [Store].
//
// The sync pulls the records from a remote PocketBase server and writes them
// into the local embedded database. Records are matched by id, so the
// operation is idempotent and can be repeated to refresh the cache.
func (s *Embedded) SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if s.isClosed() {
		return nil, ErrClosed
	}

	if req.RemoteCollection == "" {
		return nil, errors.New("pbstore: remote collection is required for sync")
	}

	local := req.LocalCollection
	if local == "" {
		local = req.RemoteCollection
	}

	if !s.isAllowed(local) {
		return nil, fmt.Errorf("%w: %q", ErrCollectionNotAllowed, local)
	}

	report := &SyncReport{Collection: local}
	start := time.Now()

	// The local collection is created on the fly when it does not exist
	// yet: the whole point of the sync is to populate the local cache.
	if _, err := s.app.FindCachedCollectionByNameOrId(local); err != nil {
		if _, err := s.EnsureDictionaryCollection(ctx, local, nil); err != nil {
			return nil, fmt.Errorf("pbstore: create the local collection %q: %w", local, err)
		}
	}

	remote, err := OpenRemote(RemoteConfig{
		BaseURL: s.config.SyncRemoteURL,
		Token:   s.config.SyncRemoteToken,
	})
	if err != nil {
		return nil, fmt.Errorf("pbstore: open sync source: %w", err)
	}
	defer func() { _ = remote.Close() }()

	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultSyncBatchSize
	}

	var since *string

	if req.Since != nil {
		s := req.Since.UTC().Format("2006-01-02 15:04:05.000Z")
		since = &s
	}

	page := 1

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		result, err := remote.List(ctx, Query{
			Collection: req.RemoteCollection,
			Filter:     SyncFilter(req.Filter, since),
			Sort:       "id",
			Page:       page,
			PerPage:    batchSize,
		})
		if err != nil {
			return nil, fmt.Errorf("pbstore: sync fetch: %w", err)
		}

		for _, rec := range result.Records {
			if req.MaxRecords > 0 && report.Fetched >= req.MaxRecords {
				report.Skipped++
				continue
			}

			report.Fetched++

			created, err := s.upsertSynced(ctx, local, rec)
			if err != nil {
				return nil, err
			}

			if created {
				report.Created++
			} else {
				report.Updated++
			}
		}

		if len(result.Records) < batchSize || result.TotalPages <= page {
			break
		}

		page++
	}

	report.Duration = time.Since(start)

	return report, nil
}

// upsertSynced writes a synced record into the local collection.
//
// It returns true when a new record was created and false when an existing
// record was refreshed.
func (s *Embedded) upsertSynced(ctx context.Context, collection string, rec *Record) (bool, error) {
	if rec == nil {
		return false, errors.New("pbstore: nil synced record")
	}

	if err := ctx.Err(); err != nil {
		return false, err
	}

	// The system fields are owned by the local database and must not be
	// overwritten by the remote copy.
	data := make(map[string]any, len(rec.Fields))
	for k, v := range rec.Fields {
		switch k {
		case core.FieldNameId, core.FieldNameCollectionId, core.FieldNameCollectionName,
			fieldNameCreated, fieldNameUpdated:
			continue
		}

		data[k] = v
	}

	existing, err := s.app.FindRecordById(collection, normalizeID(rec.ID))
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return false, fmt.Errorf("pbstore: sync lookup %s/%s: %w", collection, rec.ID, err)
		}

		model, err := s.app.FindCachedCollectionByNameOrId(collection)
		if err != nil {
			return false, fmt.Errorf("pbstore: sync resolve collection %q: %w", collection, err)
		}

		newRec := core.NewRecord(model)
		// PocketBase requires an id of at least 15 characters, so a
		// shorter remote id is padded instead of being rejected.
		newRec.Id = normalizeID(rec.ID)
		newRec.Load(data)

		if err := s.app.SaveWithContext(ctx, newRec); err != nil {
			return false, fmt.Errorf("pbstore: sync insert %s/%s: %w", collection, rec.ID, err)
		}

		return true, nil
	}

	existing.Load(data)

	if err := s.app.SaveWithContext(ctx, existing); err != nil {
		return false, fmt.Errorf("pbstore: sync update %s/%s: %w", collection, rec.ID, err)
	}

	return false, nil
}

// minRecordIDLength is the minimum length of a PocketBase record id.
const minRecordIDLength = 15

// normalizeID adapts a remote record id to the PocketBase requirements.
//
// PocketBase requires an id of at least 15 characters matching the
// `^\w+$` pattern. A shorter remote id is padded with zeros so that the
// same remote record always maps to the same local id, which keeps the
// sync idempotent.
func normalizeID(id string) string {
	if len(id) >= minRecordIDLength {
		return id
	}

	return id + strings.Repeat("0", minRecordIDLength-len(id))
}

// Close implements [Store].
//
// It releases the PocketBase application resources (db connections, cron
// ticker, ...).
func (s *Embedded) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	if s.app != nil {
		if err := s.app.ClearBootstrap(); err != nil {
			return fmt.Errorf("pbstore: close embedded store: %w", err)
		}
	}

	return nil
}

// isClosed reports whether the store has been closed.
func (s *Embedded) isClosed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.closed
}

// isAllowed reports whether the collection passes the configured allowlist.
//
// An empty allowlist allows every collection.
func (s *Embedded) isAllowed(collection string) bool {
	if len(s.allowed) == 0 {
		return true
	}

	return s.allowed[strings.ToLower(collection)]
}

// toSet converts a slice of names into a lower-cased lookup set.
func toSet(names []string) map[string]bool {
	if len(names) == 0 {
		return nil
	}

	set := make(map[string]bool, len(names))
	for _, n := range names {
		n = strings.TrimSpace(strings.ToLower(n))
		if n != "" {
			set[n] = true
		}
	}

	return set
}
