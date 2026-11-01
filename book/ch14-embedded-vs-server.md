# Chapter 14 — Embedded or Server: One Library, Two Deployments

## 14.1 The same library, two deployments

PocketBase gives you two ways to reach the data, and both are built on
the same `core` package.

```
                    ┌──────────────────────────┐
   embedded mode    │  your process             │
                    │    └─ core.App ──▶ SQLite  │
                    └──────────────────────────┘

                    ┌──────────────────────────┐
   remote mode      │  your process             │
                    │    └─ HTTP ──▶ PocketBase │
                    │              server       │
                    │                └─▶ SQLite │
                    └──────────────────────────┘
```

The difference is not in the data model, the filter grammar, the
validation or the record shape. **All of that is identical.** The
difference is in how the bytes travel.

## 14.2 The REST API

The remote backend speaks the native PocketBase REST API:

| Operation    | Endpoint                                          |
| ------------ | ------------------------------------------------- |
| List         | `GET /api/collections/{collection}/records`        |
| Get          | `GET /api/collections/{collection}/records/{id}`    |
| Create       | `POST /api/collections/{collection}/records`       |
| Update       | `PATCH /api/collections/{collection}/records/{id}`  |
| Delete       | `DELETE /api/collections/{collection}/records/{id}` |
| Collections  | `GET /api/collections`                             |
| Health       | `GET /api/health`                                  |

The query parameters are the native ones:

```
GET /api/collections/items/records?page=2&perPage=50&filter=key%20%3D%20%22go%22&sort=-created&expand=author
```

The list response:

```json
{
  "page": 1,
  "perPage": 30,
  "totalItems": 3,
  "totalPages": 1,
  "items": [
    {
      "id": "28vntlshlihnmum",
      "collectionId": "pbc_2373489703",
      "collectionName": "vocab",
      "key": "go",
      "label": "Go"
    }
  ]
}
```

The single-record response is wrapped in a `data` envelope:

```json
{
  "data": {
    "id": "28vntlshlihnmum",
    "collectionName": "vocab",
    "key": "go"
  }
}
```

⚠️ **The two shapes must both be handled.** The template's
`recordFromMap` unwraps the envelope when it is present:

```go
func recordFromMap(payload map[string]any) *Record {
    if payload == nil {
        return nil
    }

    // Unwrap the single-record envelope when present.
    if data, ok := payload["data"].(map[string]any); ok {
        payload = data
    }

    // ...
}
```

## 14.3 The token

The remote backend sends the token as the `Authorization` header:

```go
if auth && s.token != "" {
    req.Header.Set("Authorization", s.token)
}
```

That is exactly what the PocketBase admin UI does. The token is a
superuser or user token, obtained from the admin dashboard.

⚠️ **Never commit a token.** The template's config comments say so:

```go
// Token is the API token used to authenticate the requests.
//
// SECURITY: never hardcode a token in a committed file. Prefer the
// `FLINT_REMOTE_TOKEN` environment variable or an untracked config
// file.
```

## 14.4 The collections endpoint and the token problem

Here is a practical problem that the template had to solve.

`GET /api/collections` is a **superuser-only** endpoint. Without a
token, it returns:

```json
{
  "status": 401,
  "message": "The request requires valid record authorization token.",
  "data": {}
}
```

So a remote-mode application without a token cannot discover the
collections. The template solves it by falling back to the allowlist:

```go
err := s.doJSON(ctx, http.MethodGet, "/api/collections?perPage=200", nil, &payload, true)
if err != nil {
    // The collections endpoint requires an authenticated (usually
    // superuser) request. Without a token the allowlist is the only
    // source of truth, so the configured collections are used as a
    // fallback.
    if len(s.allowed) > 0 && isUnauthorized(err) {
        return s.allowedCollections(), nil
    }
    return nil, err
}
```

```go
// allowedCollections returns the configured allowlist as a collection list.
func (s *Remote) allowedCollections() []Collection {
    names := make([]string, 0, len(s.allowed))
    for name := range s.allowed {
        names = append(names, name)
    }
    sort.Strings(names)

    out := make([]Collection, 0, len(names))
    for _, name := range names {
        out = append(out, Collection{Name: name, Type: "base"})
    }
    return out
}
```

So the remote mode works in two ways:

| Configuration                | How the collections are discovered        |
| ---------------------------- | ----------------------------------------- |
| `--remote-token` provided    | `GET /api/collections` (the real schema)   |
| `--collections a,b` only     | The allowlist (names only, no ids)         |

⚠️ **With the allowlist fallback, the collection *ids* are unknown.**
The ids are only needed for the relations and the admin operations, so
this is rarely a problem — but it is a real limitation.

## 14.5 The embedded implementation

The embedded backend is a direct call. Here is the whole of a listing:

```go
func (s *Embedded) List(ctx context.Context, q Query) (*PageResult, error) {
    // ... validation ...

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
        return nil, fmt.Errorf("%w: %v", ErrInvalidFilter, err)
    }
    provider.SkipTotal(q.SkipTotal)

    records := []*core.Record{}
    result, err := provider.Exec(&records)
    if err != nil {
        return nil, fmt.Errorf("pbstore: list %q: %w", q.Collection, err)
    }

    // ... convert ...
}
```

## 14.6 The remote implementation

The remote backend builds the URL and parses the JSON:

```go
func (s *Remote) List(ctx context.Context, q Query) (*PageResult, error) {
    // ... validation ...

    params := QueryParams(q)

    var payload struct {
        Items      []map[string]any `json:"items"`
        Page       int              `json:"page"`
        PerPage    int              `json:"perPage"`
        TotalItems int              `json:"totalItems"`
        TotalPages int              `json:"totalPages"`
    }

    endpoint := SearchURL(q.Collection) + "?" + params.Encode()
    if err := s.doJSON(ctx, http.MethodGet, endpoint, nil, &payload, true); err != nil {
        return nil, err
    }

    out := &PageResult{
        Records:    make([]*Record, 0, len(payload.Items)),
        Page:       payload.Page,
        PerPage:    payload.PerPage,
        TotalItems: payload.TotalItems,
        TotalPages: payload.TotalPages,
    }
    for _, item := range payload.Items {
        out.Records = append(out.Records, recordFromMap(item))
    }
    return out, nil
}
```

### The symmetry

Notice how both implementations:

1. validate the same things;
2. build the query parameters with the **same** `QueryParams` function;
3. produce the same `PageResult` shape;
4. convert to the same `Record` shape.

That symmetry is the entire point. **The only difference is the middle
step** — a direct call versus an HTTP round trip.

## 14.7 The transport layer

The remote backend's HTTP helper handles the details:

```go
func (s *Remote) doJSON(ctx context.Context, method, path string, body any, out any, auth bool) error {
    var payload []byte

    if body != nil {
        encoded, err := json.Marshal(body)
        if err != nil {
            return fmt.Errorf("pbstore: encode request body: %w", err)
        }
        payload = encoded
    }

    attempts := s.retries + 1
    if attempts < 1 {
        attempts = 1
    }

    var lastErr error
    for attempt := 0; attempt < attempts; attempt++ {
        if attempt > 0 {
            // Linear backoff between the retries.
            select {
            case <-ctx.Done():
                return ctx.Err()
            case <-time.After(time.Duration(attempt) * 250 * time.Millisecond):
            }
        }

        req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, bytes.NewReader(payload))
        if err != nil {
            return fmt.Errorf("pbstore: build request: %w", err)
        }

        req.Header.Set("Accept", "application/json")
        if body != nil {
            req.Header.Set("Content-Type", "application/json")
        }
        if auth && s.token != "" {
            req.Header.Set("Authorization", s.token)
        }

        resp, err := s.client.Do(req)
        if err != nil {
            lastErr = fmt.Errorf("pbstore: %s %s: %w", method, path, err)
            continue
        }

        raw, readErr := io.ReadAll(resp.Body)
        _ = resp.Body.Close()

        if resp.StatusCode >= 500 {
            lastErr = fmt.Errorf("pbstore: %s %s: server error %d", method, path, resp.StatusCode)
            continue
        }

        if resp.StatusCode == http.StatusNotFound {
            return fmt.Errorf("%w: %s", ErrNotFound, path)
        }

        if resp.StatusCode < 200 || resp.StatusCode > 299 {
            return fmt.Errorf("pbstore: %s %s: unexpected status %d: %s", method, path, resp.StatusCode, summarize(raw))
        }

        if readErr != nil {
            return fmt.Errorf("pbstore: read response: %w", readErr)
        }

        if out == nil {
            return nil
        }
        if len(raw) == 0 {
            return nil
        }
        if err := json.Unmarshal(raw, out); err != nil {
            return fmt.Errorf("pbstore: decode response: %w", err)
        }
        return nil
    }

    return lastErr
}
```

### The retry policy

The retries happen on:
- **network errors** (the connection was refused, the DNS failed);
- **5xx responses** (the server is temporarily broken).

They do **not** happen on:
- **4xx responses** (the request was wrong; retrying will not help).

⚠️ **Never retry a 4xx.** A `400 Bad Request` will fail identically
every time, and retrying it just wastes the user's time.

⚠️ **The retry uses a linear backoff, not an exponential one.** For a
TUI, a linear backoff of a few hundred milliseconds is fine. For a
library, an exponential backoff with jitter is better.

### The context

`http.NewRequestWithContext` ties the request to the context, so a
timeout or a cancellation aborts the in-flight request.

## 14.8 The trade-offs

| Aspect                | Embedded                        | Remote                             |
| --------------------- | ------------------------------- | ---------------------------------- |
| Latency               | Microseconds                    | Milliseconds                       |
| Failure modes         | The process crashes              | The server is unreachable          |
| Concurrency           | SQLite locking                  | The server's concurrency control   |
| Multi-user            | Not supported (file lock)       | Supported                          |
| Deployment            | One binary                       | A server + a client                |
| Offline                | Always                           | Never (unless you sync)            |
| Setup                 | None                            | A running server                   |
| The dashboard         | Not available                    | Available                          |
| Backups               | Manual (copy the data dir)       | Built in                           |
| Realtime              | Possible (in-process)            | Built in                           |
| The admin UI          | You would have to build it       | Free                               |

### The embedded concurrency limit

This deserves emphasis, because it is the main functional difference.

SQLite allows **one writer at a time**. Two processes cannot open the
same database file for writing. PocketBase works around this by
allowing only the *first* process to open the database in read-write
mode; subsequent processes get a read-only connection.

For a single-user TUI, this is perfect. For a multi-user tool, it is
not — use the server.

⚠️ **Do not try to run two instances of an embedded application against
the same data directory.** The second one will either fail to start or
silently see stale data.

## 14.9 The third mode: embedded with a sync

The most interesting mode is the combination:

```
   PocketBase server  ──sync──▶  your local SQLite  ──query──▶  your TUI
```

This gives you:
- **offline operation** (the local database is complete);
- **shared data** (the server is the source of truth);
- **fast queries** (no network hop for the common case);
- **explicit refresh** (the user triggers the sync).

The sync is the subject of Chapter 17.

## 14.10 Choosing

A decision tree:

```
Does the data need to be shared between users or machines?
├── yes → Remote mode
│        (or embedded + sync, if offline is also needed)
└── no  → Does the user need the admin dashboard?
         ├── yes → Remote mode
         └── no  → Does the tool need to work offline?
                  ├── yes → Embedded mode
                  └── no  → Either; embedded is simpler
```

And a strong default:

> **Start embedded.** It is one binary, zero setup, and it always
> works. Add the remote mode when you actually need a server — the
> `Store` interface makes it a configuration change, not a rewrite.

## 14.11 The configuration

The mode is a configuration value, resolved at startup:

```toml
mode = "embedded"

[embedded]
data_dir = "./pb_data"

[remote]
url = "http://127.0.0.1:8090"
collections = ["items", "vocab"]
```

```go
func openStore(cfg *config.Config) (pbstore.Store, error) {
    switch pbstore.Mode(cfg.Mode) {
    case pbstore.ModeRemote:
        return pbstore.OpenRemote(pbstore.RemoteConfig{
            BaseURL:       cfg.Remote.URL,
            Token:         cfg.Remote.Token,
            Collections:   cfg.Remote.Collections,
            Timeout:       cfg.RemoteTimeout(),
            RetryAttempts: 2,
        })

    case pbstore.ModeEmbedded:
        pb := pocketbase.NewWithConfig(pocketbase.Config{
            DefaultDataDir:       cfg.Embedded.DataDir,
            DefaultEncryptionEnv: cfg.Embedded.EncryptionEnv,
            DefaultDev:           cfg.Embedded.Dev,
            HideStartBanner:      true,
        })

        return pbstore.OpenEmbedded(pb, pbstore.EmbeddedConfig{
            DataDir:       cfg.Embedded.DataDir,
            EncryptionEnv: cfg.Embedded.EncryptionEnv,
            Dev:           cfg.Embedded.Dev,
            Collections:   cfg.Remote.Collections,
            SyncRemoteURL: cfg.Remote.URL,
            Migrations:    appMigrations(),
        })

    default:
        return nil, fmt.Errorf("unsupported mode %q", cfg.Mode)
    }
}
```

⚠️ **Note the `SyncRemoteURL`.** In embedded mode, the remote URL is
still configured — it is the *source* for the sync. This is what makes
the third mode possible.

## 14.12 Testing both backends

The template tests both against the same contract.

The embedded backend, against a **real** database:

```go
func TestEmbeddedCRUD(t *testing.T) {
    store := newEmbeddedStore(t)
    ctx := context.Background()

    collection, err := store.EnsureDictionaryCollection(ctx, "vocab", ...)
    // ...
}
```

The remote backend, against an `httptest` server:

```go
func TestRemoteList(t *testing.T) {
    var gotPath string
    var gotQuery string
    var gotAuth string

    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotPath = r.URL.Path
        gotQuery = r.URL.RawQuery
        gotAuth = r.Header.Get("Authorization")

        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{ /* ... */ })
    }))
    defer server.Close()

    store, err := pbstore.OpenRemote(pbstore.RemoteConfig{
        BaseURL: server.URL,
        Token:   "secret-token",
    })
    // ...

    result, err := store.List(context.Background(), pbstore.Query{
        Collection: "items",
        Filter:     `key = "alpha"`,
        Sort:       "key",
    })
    // ...

    if gotPath != "/api/collections/items/records" {
        t.Errorf("request path = %q, want %q", gotPath, "/api/collections/items/records")
    }
    if gotAuth != "secret-token" {
        t.Errorf("Authorization header = %q, want %q", gotAuth, "secret-token")
    }
}
```

⚠️ **Test the wire format, not just the result.** The remote test
asserts the path, the query string and the `Authorization` header. If
you only assert the parsed result, a bug that sends the wrong filter
would go unnoticed if the mock server ignores the query.

## 14.13 Exercises

1. Run the same test suite against both backends. Write a table-driven
   test that parameterises over the store implementation.

2. Measure the latency of a listing in both modes, with 1, 100 and
   10,000 records.

3. Start two embedded instances against the same data directory.
   Observe what the second one does. Explain it in terms of the SQLite
   locking model.

4. Implement the remote backend's `Expand`. The REST API returns the
   expanded data in the `expand` field of each record.

5. Add a `Retry-After` handling to the remote backend: if the server
   returns a 429, wait the indicated time before retrying.

6. Implement a `Head`-style health check that verifies the *token* is
   valid, not just that the server is up.

7. Decide, for a project you have in mind, which of the three modes to
   use, and write down the reasoning.

## 14.14 What you learned

- The **same `core` package** backs both modes; only the transport
  differs.
- The remote mode speaks the **native REST API**, with the native
  query parameters.
- The list response and the single-record response have **different
  shapes** (the latter has a `data` envelope).
- The token is sent as the **`Authorization` header**.
- **`GET /api/collections` requires a superuser token**; without one,
  the allowlist is the fallback.
- The two implementations are **symmetric**: same validation, same
  parameters, same output.
- The remote transport retries **network errors and 5xx**, never 4xx.
- **SQLite allows one writer at a time** — this is the main functional
  limit of the embedded mode.
- The third mode, **embedded + sync**, gives you both offline
  operation and shared data.
- Start **embedded**; add the remote mode when you need it.

## 14.15 Further reading

- `internal/pbstore/embedded.go` — the embedded implementation.
- `internal/pbstore/remote.go` — the remote implementation and the
  transport.
- `internal/pbstore/store.go` — the interface both satisfy.
- `config/config.go` — the mode resolution.
- `cmd/tui/main.go` — `openStore`.

Next: [Chapter 15 — Architecture for TUI + Database Applications](ch15-architecture.md).
