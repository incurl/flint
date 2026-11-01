# Chapter 17 — Sync, Caching, and the Offline-First Problem

## 17.1 The scenario

You have built a beautiful TUI for browsing a reference dataset — a
controlled vocabulary, a label table, a Wikidata dump. The data lives
on a PocketBase server.

Now the requirements change:

- the tool must work **offline**, on a plane, in a basement;
- the data must be **up to date** when it is connected;
- the download must be **small** — 40 MB of vocabulary should not be
  re-downloaded every morning;
- the user must **know** whether they are looking at fresh data.

This chapter is about solving that.

## 17.2 The three architectures

### A — Proxy: always remote

```
TUI ──HTTP──▶ Server ──▶ Database
```

Every query goes over the network. Simple, always fresh, no offline
support.

### B — Snapshot: always local

```
TUI ──▶ Local SQLite   (populated once, by hand)
```

Fast, offline, but stale and manual.

### C — Replica: local cache, explicit sync

```
                    ┌── sync (explicit) ──▶ Server
                    │
TUI ──▶ Local SQLite ◀┘
```

The local database is the read path; the server is the write path. The
sync is explicit and user-triggered.

⚠️ **The template implements C.** And the choice of "explicit" is
deliberate, for reasons we will examine in §17.8.

## 17.3 The sync model

### Full sync

Copy every record from the remote collection into the local one.

```go
report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
    RemoteCollection: "labels",
    LocalCollection:  "labels",
})
```

### Incremental sync

Copy only the records that changed since the last sync.

```go
since := lastSuccessfulSync

report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
    RemoteCollection: "labels",
    Since:            &since,
})
```

### The filter

Restrict the sync to a subset.

```go
report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
    RemoteCollection: "labels",
    Filter:           `namespace = "en"`,
})
```

### The three combined

```go
report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
    RemoteCollection: "labels",
    LocalCollection:  "labels_local",
    Filter:           `namespace = "en"`,
    Since:            &since,
    BatchSize:        500,
    MaxRecords:       10000,
})
```

## 17.4 The filter construction

The incremental filter is built for you:

```go
// SyncFilter builds the native filter expression used to pull the records
// that need to be refreshed during an incremental sync.
func SyncFilter(userFilter string, since *string) string {
    clauses := make([]string, 0, 2)

    if s := strings.TrimSpace(userFilter); s != "" {
        clauses = append(clauses, "("+s+")")
    }
    if since != nil && *since != "" {
        clauses = append(clauses, `updated > "`+escapeFilterValue(*since)+`"`)
    }

    if len(clauses) == 0 {
        return ""
    }
    return strings.Join(clauses, " && ")
}
```

The results:

| User filter | Since      | Result                                              |
| ----------- | ---------- | --------------------------------------------------- |
| (none)      | (none)     | `""` (everything)                                    |
| `ns = "en"` | (none)     | `(ns = "en")`                                        |
| (none)      | a timestamp| `updated > "2024-05-01 10:00:00.000Z"`              |
| `ns = "en"` | a timestamp| `(ns = "en") && updated > "2024-05-01 10:00:00.000Z"` |

⚠️ **The user filter is parenthesised.** Without the parentheses,
`a = 1 || b = 2 && updated > "..."` would parse as
`a = 1 || (b = 2 && updated > "...")` — the `&&` binds tighter than
`||`. The parentheses preserve the user's intent.

⚠️ **The timestamp is escaped.**

```go
func escapeFilterValue(v string) string {
    replacer := strings.NewReplacer(
        `\`, `\\`,
        `"`, `\"`,
    )
    return replacer.Replace(v)
}
```

Without it, a timestamp containing a quote would break the filter. The
timestamps come from `time.Format`, so in practice they are safe — but
the escaping makes the helper correct for any input.

## 17.5 The sync implementation

```go
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
```

Let us examine the decisions.

### The sync source is authenticated

```go
remote, err := OpenRemote(RemoteConfig{
    BaseURL: s.config.SyncRemoteURL,
    Token:   s.config.SyncRemoteToken,
})
```

The sync opens a second client against the remote server, and that client
needs its own credentials. Without a token the requests are anonymous, so
a remote collection whose list rule is not `""` (the public rule) returns
an empty list instead of an error: the sync silently replicates zero
records.

⚠️ **This was a bug in the template.** The sync ignored the configured
`remote.token`, so syncing from a private collection appeared to work and
produced an empty local dataset. The fix is to carry a dedicated
`SyncRemoteToken` in the embedded configuration and to forward it when the
sync client is opened.

### The local collection is created on the fly

```go
if _, err := s.app.FindCachedCollectionByNameOrId(local); err != nil {
    if _, err := s.EnsureDictionaryCollection(ctx, local, nil); err != nil {
        return nil, fmt.Errorf("pbstore: create the local collection %q: %w", local, err)
    }
}
```

⚠️ **This was a bug in the template.** The sync originally failed with
`sql: no rows in result set` when the local collection did not exist.
The user had to create it by hand first, which defeats the purpose of
a one-click sync.

The fix is to create it. The collection is created with the default
dictionary shape (`key`, `label`, `description`), which is a sensible
default for a cache.

⚠️ **But the default shape may not match the remote schema.** If the
remote collection has a `term` field, the local copy created this way
will store it as *custom data* (which works — `CustomData()` is merged
into the view), but it will not be a schema field, so it will not be
filterable with the native grammar.

For a production sync, you would want to create the local collection
with the *same schema* as the remote one. The remote backend can fetch
the schema from `GET /api/collections/{name}` (with a superuser
token) and replicate it.

### The sort is by id

```go
Sort: "id",
```

The pagination is stable only if the sort is deterministic. Sorting by
the primary key guarantees that the same record is not on two pages.

⚠️ **Never paginate without a deterministic sort.** If the sort is
`updated`, and a record is updated while you are paging, it can move
between pages and be fetched twice (or skipped).

### The loop condition

```go
if len(result.Records) < batchSize || result.TotalPages <= page {
    break
}
```

Two exit conditions: a short page (the last one) or the last page.

⚠️ **`result.TotalPages` can be 0** when the server does not return it
(which happens with `skipTotal`). The condition `TotalPages <= page`
then breaks after the first page. If you rely on `skipTotal`, use only
the short-page condition.

### The report

```go
type SyncReport struct {
    Collection string
    Fetched    int
    Created    int
    Updated    int
    Skipped    int
    Duration   time.Duration
}
```

This is what the user sees:

```
✓ vocab: 3 fetched · 3 created · 0 updated · 4ms
```

## 17.6 The upsert

```go
func (s *Embedded) upsertSynced(ctx context.Context, collection string, rec *Record) (bool, error) {
    if rec == nil {
        return false, errors.New("pbstore: nil synced record")
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

        if err := s.app.Save(newRec); err != nil {
            return false, fmt.Errorf("pbstore: sync insert %s/%s: %w", collection, rec.ID, err)
        }
        return true, nil
    }

    existing.Load(data)
    if err := s.app.Save(existing); err != nil {
        return false, fmt.Errorf("pbstore: sync update %s/%s: %w", collection, rec.ID, err)
    }
    return false, nil
}
```

### The system fields are skipped

```go
switch k {
case core.FieldNameId, core.FieldNameCollectionId, core.FieldNameCollectionName,
    fieldNameCreated, fieldNameUpdated:
    continue
}
```

The `id`, `collectionId`, `collectionName`, `created` and `updated`
fields belong to the *local* database. Overwriting `created` with the
remote value would be wrong (the local record may have been created at
a different time), and overwriting `id` would break the lookup.

⚠️ **This is a subtle but important detail.** The `created`/`updated`
fields are `Autodate` fields, managed by the framework. If you set
them explicitly, PocketBase's behaviour depends on the field
configuration — and the local `updated` would not reflect the local
write.

### The id normalisation

```go
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
```

⚠️ **This was found by a test.** The sync test used a remote id of
`"remote1"` (7 characters), and PocketBase rejected it:

```
id: Must be at least 15 character(s).
```

The first fix padded with underscores, which produced a different
error:

```
id: Invalid value format.
```

The id must match `^\w+$` — **word characters only**. Underscores are
word characters; hyphens are not. But the *padding* must not introduce
a non-word character... and it did not. The real problem was that
padding with `_` produced an id that the *validation* rejected for a
different reason.

The correct padding is `0`, which is a word character. The rule:

> **PocketBase ids must be at least 15 characters and match `^\w+$`.**

### The idempotency

The upsert matches by id, so a repeated sync *updates* rather than
*duplicates*. This is verified by the test:

```go
// A second sync must update the existing record instead of
// duplicating it.
report, err = store.SyncCollection(ctx, pbstore.SyncRequest{...})
if report.Created != 0 {
    t.Errorf("Created = %d on the second sync, want 0", report.Created)
}
if report.Updated != 1 {
    t.Errorf("Updated = %d on the second sync, want 1", report.Updated)
}
```

## 17.7 Tracking the sync state

For an incremental sync you need to remember when the last successful
sync happened. ⚠️ **Not in a Go map** — it must survive a restart.

### The state collection

Create a small collection to hold the sync metadata:

```go
{
    File: "1700000004_create_sync_state.go",
    Up: func(txApp core.App) error {
        collection := core.NewBaseCollection("_sync_state")
        collection.Fields.Add(
            &core.TextField{Name: "collection", Required: true},
            &core.TextField{Name: "last_sync", Required: true},
        )
        return txApp.Save(collection)
    },
    Down: func(txApp core.App) error {
        collection, err := txApp.FindCollectionByNameOrId("_sync_state")
        if err != nil {
            return nil
        }
        return txApp.Delete(collection)
    },
}
```

### Reading and writing the timestamp

```go
// LastSync returns the timestamp of the last successful sync for a
// collection.
func (s *Embedded) LastSync(ctx context.Context, collection string) (*time.Time, error) {
    rec, err := s.FindFirst(ctx, "_sync_state", fmt.Sprintf(
        "collection = %q", collection,
    ))
    if err != nil {
        if errors.Is(err, ErrNotFound) {
            return nil, nil    // never synced
        }
        return nil, err
    }

    parsed, err := time.Parse("2006-01-02 15:04:05.000Z", rec.StringField("last_sync"))
    if err != nil {
        return nil, fmt.Errorf("pbstore: parse the last sync timestamp: %w", err)
    }
    return &parsed, nil
}

// SetLastSync records the timestamp of a successful sync.
func (s *Embedded) SetLastSync(ctx context.Context, collection string, at time.Time) error {
    value := at.UTC().Format("2006-01-02 15:04:05.000Z")

    rec, err := s.FindFirst(ctx, "_sync_state", fmt.Sprintf(
        "collection = %q", collection,
    ))
    if err != nil {
        if !errors.Is(err, ErrNotFound) {
            return err
        }

        _, err := s.Create(ctx, "_sync_state", map[string]any{
            "collection": collection,
            "last_sync":  value,
        })
        return err
    }

    _, err = s.Update(ctx, "_sync_state", rec.ID, map[string]any{
        "last_sync": value,
    })
    return err
}
```

⚠️ **Record the timestamp only after a *successful* sync.** If you
record it before, a failed sync would advance the watermark and the
next sync would miss the failed records.

⚠️ **The timestamp format must match the filter format.** The filter
uses `2006-01-02 15:04:05.000Z` and the state uses the same. If they
diverge, the incremental filter silently matches nothing (or
everything).

## 17.8 Why the sync is explicit

The brief that produced the template said:

> *"Do NOT add automatic background sync by default; sync is an
> explicit user-triggered action in the TUI, an extension point."*

This is the right call, and the reasons are worth understanding.

### The arguments for explicit

**The user knows when the data is stale.** A status bar that says
"synced 2 hours ago" is honest. A background sync that runs every 5
minutes makes the state unknowable.

**No surprise network traffic.** A tool that works offline should not
phone home without being asked.

**No surprise CPU and battery usage.** A background sync of a large
collection is expensive.

**The failures are visible.** A background sync that fails silently is
worse than no sync.

**It is testable.** An explicit sync is a function you can call. A
background sync is a goroutine that races with everything.

### The arguments for automatic

**The data stays fresh without effort.**

**The user does not have to think about it.**

### The resolution

The template makes it explicit *by default*, and provides the extension
point for automatic:

```go
// The "y" hotkey triggers a sync.
case "y":
    return m.triggerSync(), true
```

```go
func (m *Model) triggerSync() tea.Cmd {
    store := m.store
    collection := m.runtime.Collection

    if store == nil {
        return m.toast.Show(components.ToastError, "no store configured")
    }
    if store.Mode() != pbstore.ModeEmbedded {
        return m.toast.Show(components.ToastWarning, "sync is only available in the embedded mode")
    }
    if collection == "" {
        return m.toast.Show(components.ToastWarning, "select a collection first")
    }

    req := pbstore.SyncRequest{
        RemoteCollection: collection,
        LocalCollection:  collection,
        BatchSize:        pbstore.DefaultSyncBatchSize,
    }

    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
        defer cancel()

        report, err := store.SyncCollection(ctx, req)
        if err != nil {
            return SyncFinishedMsg{Err: err}
        }
        return SyncFinishedMsg{Report: report}
    }
}
```

⚠️ **A project that wants automatic sync can add it**, by scheduling a
`tea.Tick` in the root model that calls `triggerSync` periodically. The
extension point exists; the default is conservative.

## 17.9 The sync in the UI

### The sync page

```go
func (p *SyncPage) View() tea.View {
    var b []string

    b = append(b, p.theme.Title.Render("Sync — pull a remote collection into the local store"))
    b = append(b, "")

    if p.store == nil || p.store.Mode() != pbstore.ModeEmbedded {
        b = append(b, p.theme.Warning.Render("sync is only available in the embedded mode"))
        b = append(b, "")
        b = append(b, p.theme.Muted.Render("the remote mode queries the server directly"))
        return tea.NewView(joinLines(b))
    }

    // ... the inputs, the spinner, the report ...
}
```

### The status bar

```go
case SyncStartedMsg:
    m.runtime.Sync = state.SyncRunning
    m.runtime.SyncProgress = msg.Collection
    return m, nil

case SyncFinishedMsg:
    m.runtime.Sync = state.SyncDone
    m.runtime.SyncProgress = ""
    if msg.Err != nil {
        m.runtime.Sync = state.SyncFailed
        m.runtime.SetError(msg.Err)
        return m, tea.Batch(
            m.toast.Show(components.ToastError, "sync failed: "+msg.Err.Error()),
            m.reloadActivePage(),
        )
    }
    return m, tea.Batch(
        m.toast.Show(components.ToastSuccess, fmt.Sprintf(
            "synced %d records (%d created, %d updated)",
            msg.Report.Fetched, msg.Report.Created, msg.Report.Updated)),
        m.reloadActivePage(),
    )
```

### The refresh after the sync

⚠️ **The sync changes the data, so the visible pages must be
reloaded.** The `reloadActivePage()` call in the `SyncFinishedMsg`
handler does that.

Without it, the user syncs, sees the toast, and the record list still
shows the old data — a very confusing bug.

## 17.10 Conflict resolution

What happens when a record was modified in both places?

### Last-write-wins (what the template does)

The sync overwrites the local copy with the remote one. The local
changes are lost.

This is the right default for a **cache**: the remote is the source of
truth.

### A smarter approach

Compare the `updated` timestamps:

```go
if existing.Updated.After(remoteUpdated) {
    // The local copy is newer: skip it.
    report.Skipped++
    continue
}
```

⚠️ **This requires comparing timestamps reliably**, which means
normalising the formats. The remote returns PocketBase's `types.DateTime`
format; the local one is the same. It works, but it is a decision you
have to make explicitly.

### A merge strategy

For a dictionary where both sides add terms, a set-union merge is more
appropriate: keep the local record if the remote does not have it, and
update it if the remote is newer.

⚠️ **Do not build a general merge engine.** Decide the policy for your
domain and implement that one.

## 17.11 Deletions

The sync does not propagate deletions. A record deleted on the server
stays in the local cache.

### Why

Detecting deletions requires either:
- a full sync that compares the id sets (expensive), or
- a tombstone field on the server (a schema change).

### The options

**Option 1: ignore the deletions.** The cache grows stale. Acceptable
for a reference dataset where deletions are rare.

**Option 2: a periodic full reconciliation.** Every N syncs, fetch all
the ids and delete the local records that are not in the set.

**Option 3: a tombstone.** Add a `deleted` field to the remote
collection, set it instead of deleting, and have the sync delete the
local copies of the tombstoned records.

⚠️ **Option 3 is the cleanest** if you control the remote schema.

## 17.12 The progress reporting

A sync of 100,000 records takes a while. The user needs feedback.

### The current implementation

The template reports the progress at the end:

```
✓ vocab: 3 fetched · 3 created · 0 updated · 4ms
```

### A better implementation

Emit a progress message from the sync loop:

```go
type SyncProgressMsg struct {
    Collection string
    Fetched    int
    Total      int
}
```

```go
// Inside the sync loop, after each batch:
p.send(SyncProgressMsg{Collection: local, Fetched: report.Fetched, Total: result.TotalItems})
```

⚠️ **The sync runs in a command goroutine, so it cannot touch the
model.** It needs a way to report progress — a callback, or a channel.

The callback approach:

```go
type SyncRequest struct {
    // ...
    // OnProgress is called after each batch with the number of records
    // fetched so far.
    OnProgress func(fetched, total int)
}
```

```go
// In the command:
report, err := store.SyncCollection(ctx, req)
```

with the callback set inside the command:

```go
req.OnProgress = func(fetched, total int) {
    program.Send(syncProgressMsg{fetched: fetched, total: total})
}
```

⚠️ **`program.Send` from the sync callback** is the escape hatch
described in Chapter 3. The callback runs on the command goroutine, so
it must not touch the model — `program.Send` is the safe way to
communicate.

## 17.13 Testing the sync

The test uses an `httptest` server that mimics the records endpoint:

```go
func TestEmbeddedSyncCreatesLocalCollection(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        _, _ = w.Write([]byte(`{
            "page": 1,
            "perPage": 30,
            "totalItems": 1,
            "totalPages": 1,
            "items": [{
                "id": "remote1",
                "collectionId": "col1",
                "collectionName": "vocab",
                "key": "go",
                "label": "Go"
            }]
        }`))
    }))
    defer server.Close()

    // ... open the store with SyncRemoteURL: server.URL ...

    report, err := store.SyncCollection(ctx, pbstore.SyncRequest{
        RemoteCollection: "vocab",
        LocalCollection:  "vocab",
    })
    if err != nil {
        t.Fatalf("SyncCollection() error = %v", err)
    }

    if report.Fetched != 1 {
        t.Errorf("Fetched = %d, want 1", report.Fetched)
    }
    if report.Created != 1 {
        t.Errorf("Created = %d, want 1", report.Created)
    }

    // The record must be readable from the local store.
    rec, err := store.Get(ctx, "vocab", "remote1")
    if err != nil {
        t.Fatalf("Get() error = %v", err)
    }
    if got := rec.StringField("label"); got != "Go" {
        t.Errorf("label = %q, want %q", got, "Go")
    }

    // A second sync must update the existing record instead of
    // duplicating it.
    report, err = store.SyncCollection(ctx, pbstore.SyncRequest{
        RemoteCollection: "vocab",
        LocalCollection:  "vocab",
    })
    if report.Created != 0 {
        t.Errorf("Created = %d on the second sync, want 0", report.Created)
    }
    if report.Updated != 1 {
        t.Errorf("Updated = %d on the second sync, want 1", report.Updated)
    }
}
```

⚠️ **Notice that the test found two bugs**: the missing local
collection and the id length. Both were invisible until the test
exercised the real code path.

## 17.14 Exercises

1. Implement the incremental sync end to end: the state collection, the
   `LastSync`/`SetLastSync` methods, and the filter construction. Test
   it with a mock server that returns different records for different
   filters.

2. Implement the deletion propagation with a tombstone field. Test it.

3. Add progress reporting to the sync. Verify the progress bar in the
   TUI updates.

4. Measure the sync time for 1,000, 10,000 and 100,000 records. Find
   the bottleneck.

5. Implement a "preview" mode: show what the sync *would* do (how many
   records would be created, updated, skipped) without writing.

6. Implement a conflict policy that skips the local records that are
   newer than the remote ones.

7. Add a "sync all the configured collections" button, using the
   `sync.collections` config value.

## 17.15 What you learned

- The **replica** architecture — a local cache with an explicit sync —
  gives you both offline operation and shared data.
- The sync is **idempotent** because it matches by id.
- The **incremental filter** combines the user filter and the `updated`
  timestamp, with the user filter **parenthesised**.
- The local collection is **created on the fly** by the sync.
- The pagination must use a **deterministic sort** (`id`).
- The **system fields** are not overwritten by the remote copy.
- PocketBase ids must be **≥15 characters and match `^\w+$`**, so
  remote ids are normalised.
- The **watermark must be persisted** in a collection, and recorded
  only after a successful sync.
- The sync is **explicit by default**, and that is a deliberate,
  defensible choice.
- The sync **changes the data**, so the visible pages must be
  reloaded.

## 17.16 Further reading

- `internal/pbstore/embedded.go` — `SyncCollection` and `upsertSynced`.
- `internal/pbstore/queries.go` — `SyncFilter` and `escapeFilterValue`.
- `internal/pages/sync.go` — the sync page.
- `internal/app/app.go` — `triggerSync` and the `SyncFinishedMsg`
  handler.
- `internal/pbstore/embedded_test.go` — the sync test.

Next: [Chapter 18 — Testing Terminal Applications](ch18-testing.md).
