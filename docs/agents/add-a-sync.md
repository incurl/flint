# Skill: add a sync

**Goal:** pull records from a remote PocketBase server into the local
embedded database.

**Time:** ~20 minutes. Touches 2–3 files.

---

## What the sync already does

The template ships a working, idempotent sync:

- It fetches the remote records in **batches** (default 200).
- It **matches by id**: a repeated sync updates the existing local
  record instead of duplicating it.
- It **creates the local collection** on the fly if it does not exist.
- It **pads short remote ids** to the 15 characters PocketBase requires.
- It **skips the system fields** (`id`, `collectionId`, `collectionName`,
  `created`, `updated`) — those belong to the local database.
- It is **explicit**: nothing syncs in the background. The user triggers
  it from the sync page with `ctrl+r`.

**Read `internal/pbstore/embedded.go` (`SyncCollection` and
`upsertSynced`) before changing anything.** It is about 100 lines and
every line is load-bearing.

---

## When do you need a new sync?

Most of the time you do not. The generic sync handles any collection.

Write a custom sync only when you need to **transform** the data on the
way in: renaming fields, flattening nested JSON, computing a value, or
filtering on the remote side in a way the generic filter cannot express.

---

## Step 1 — configure the source

The embedded store needs to know where to sync from:

```go
// cmd/tui/main.go
return pbstore.OpenEmbedded(pb, pbstore.EmbeddedConfig{
	DataDir:         cfg.Embedded.DataDir,
	SyncRemoteURL:   cfg.Remote.URL,
	SyncRemoteToken: cfg.Remote.Token, // ← REQUIRED for non-public collections
	Migrations:      appMigrations(),
})
```

**`SyncRemoteToken` is not optional in practice.** A remote collection
whose list rule is not `""` answers an unauthenticated request with an
**empty list, not an error**. The sync then appears to work and
replicates nothing. This is the most common way the sync silently
fails.

---

## Step 2 — configure which collections to sync

```toml
[sync]
collections = ["vocab", "authors"]
batch_size = 200
max_records = 10000
```

Or per-run:

```bash
flint --sync-collections vocab,authors --sync-batch-size 500
```

---

## Step 3 — trigger it

The sync page already does this. From your own code:

```go
report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
	RemoteCollection: "vocab",
	LocalCollection:  "wd_vocab", // optional; defaults to RemoteCollection
	Filter:           `key != ""`, // native PocketBase filter
	BatchSize:        200,
	MaxRecords:       10000,
	Since:            &lastSync,   // incremental
})
```

---

## Step 4 — a custom transform

If you need to reshape the data, do it **after** the generic sync, in
your own code. Do not fork `SyncCollection`.

```go
// internal/domain/import.go

// ImportVocabulary pulls the remote vocabulary and normalises it.
//
// The generic sync copies the records verbatim; this pass rewrites the
// label into the shape the renderer expects.
func ImportVocabulary(ctx context.Context, store pbstore.Store) error {
	report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
		RemoteCollection: "vocab",
	})
	if err != nil {
		return fmt.Errorf("domain: sync the vocabulary: %w", err)
	}

	records, err := store.List(ctx, pbstore.Query{
		Collection: "vocab",
		PerPage:    report.Fetched,
	})
	if err != nil {
		return fmt.Errorf("domain: read the vocabulary: %w", err)
	}

	for _, rec := range records.Records {
		label := normaliseLabel(rec.StringField("label"))
		if label == "" {
			continue
		}

		if _, err := store.Update(ctx, "vocab", rec.ID, map[string]any{
			"label": label,
		}); err != nil {
			return fmt.Errorf("domain: update %s: %w", rec.ID, err)
		}
	}

	return nil
}
```

Call it from a command, never from `Update`:

```go
func (p *SyncPage) importCmd() tea.Cmd {
	store := p.store

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := domain.ImportVocabulary(ctx, store); err != nil {
			return ImportMsg{Err: err}
		}

		return ImportMsg{}
	}
}
```

---

## Step 5 — track the sync state

For an incremental sync, store the timestamp of the last successful
sync **in a PocketBase collection**, not in a Go map.

```go
// The state collection holds one row per synced collection.
const SyncStateCollection = "sync_state"

// in a migration
syncState := core.NewBaseCollection(SyncStateCollection)
syncState.Fields.Add(
	&core.TextField{Name: "collection", Required: true, Max: 100},
	&core.DateField{Name: "last_sync"},
)
```

Read it before the sync, write it after:

```go
func LastSync(ctx context.Context, store pbstore.Store, collection string) (*time.Time, error) {
	rec, err := store.FindFirst(ctx, SyncStateCollection,
		fmt.Sprintf("collection = %q", collection))
	if err != nil {
		if errors.Is(err, pbstore.ErrNotFound) {
			return nil, nil // never synced
		}

		return nil, err
	}

	raw := rec.StringField("last_sync")
	if raw == "" {
		return nil, nil
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, nil
	}

	return &parsed, nil
}
```

**Why a collection and not a map?** A Go map is lost when the process
exits and is not shared between processes. `App.Store()` is volatile for
the same reason. A collection is durable and queryable.
---

## Step 6 — test it

The `internal/pbstore/embedded_test.go` file has the pattern: an
`httptest` server that mimics the PocketBase records endpoint.

```go
func TestSyncSendsToken(t *testing.T) {
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		// … return a page of records
	}))
	defer server.Close()

	// … open the embedded store with SyncRemoteURL and SyncRemoteToken

	report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
		RemoteCollection: "vocab",
	})
	if err != nil {
		t.Fatalf("SyncCollection() error = %v", err)
	}

	if gotAuth != token {
		t.Errorf("Authorization header = %q, want %q", gotAuth, token)
	}

	if report.Fetched != 1 {
		t.Errorf("Fetched = %d, want 1", report.Fetched)
	}
}
```

---

## Verify

```bash
gofmt -w ./internal
go build ./... && go test ./...
golangci-lint run

# End to end against a real server.
FLINT_REMOTE_TOKEN=... flint --mode remote --remote-url http://127.0.0.1:8090
```

---

## Gotchas

| Symptoke                                          | Cause                                                        |
| ------------------------------------------------ | ------------------------------------------------------------ |
| The sync succeeds but syncs 0 records            | No token for a non-public collection.                        |
| The local collection does not exist              | The generic sync creates it — unless you bypassed it.        |
| The remote ids are rejected                     | They are shorter than 15 chars. `normalizeID` pads them.     |
| The sync duplicates records                     | The ids differ between runs. Matching is by id.              |
| The local `created` timestamp changes on sync    | You are writing the system fields. They are skipped.         |
| The sync is slow                                 | Lower `BatchSize`, or use `Since` for an incremental sync.   |

The report tells you what happened:

```go
fmt.Printf("fetched=%d created=%d updated=%d skipped=%d in %s\n",
	report.Fetched, report.Created, report.Updated, report.Skipped,
	report.Duration)
```

