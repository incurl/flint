# API reference

The exact surface of the Flint packages. Every symbol below is verified
against the code — if something is not listed here, grep for it rather
than assuming it exists.

Import paths use the template's module path, `github.com/incurl/flint`.
**Change the module path when you start a new project.**

---

## `internal/pbstore` — the data layer

Import: `github.com/incurl/flint/internal/pbstore`

### `Store` interface

The only thing domain code should depend on.

```go
type Store interface {
	Mode() Mode
	HealthCheck(ctx context.Context) (string, error)
	Collections(ctx context.Context) ([]Collection, error)
	List(ctx context.Context, q Query) (*PageResult, error)
	Get(ctx context.Context, collection, id string) (*Record, error)
	FindFirst(ctx context.Context, collection, filter string) (*Record, error)
	Create(ctx context.Context, collection string, data map[string]any) (*Record, error)
	Update(ctx context.Context, collection, id string, data map[string]any) (*Record, error)
	Delete(ctx context.Context, collection, id string) error
	Count(ctx context.Context, collection, filter string) (int, error)
	EnsureDictionaryCollection(ctx context.Context, name string, fields []DictionaryField) (Collection, error)
	SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error)
	Close() error
}
```

Implementations must be **safe for concurrent use** — commands run in
background goroutines and call the store while the render loop is live.

### Modes and errors

```go
type Mode string

const (
	ModeEmbedded Mode = "embedded" // in-process PocketBase, SQLite
	ModeRemote   Mode = "remote"   // PocketBase server over REST
)
```

```go
var (
	ErrNotFound            = errors.New("pbstore: not found")
	ErrCollectionNotAllowed = errors.New("pbstore: collection is not allowed")
	ErrInvalidFilter       = errors.New("pbstore: invalid filter")
	ErrInvalidSort         = errors.New("pbstore: invalid sort")
	ErrClosed              = errors.New("pbstore: store is closed")
	ErrNotConnected        = errors.New("pbstore: not connected")
)
```

Always test with `errors.Is`:

```go
if errors.Is(err, pbstore.ErrNotFound) {
	// ...
}
```

### `Query`

```go
type Query struct {
	Collection string // collection name or id
	Filter     string // native PocketBase filter, eg. `title ~ "go"`
	Sort       string // native PocketBase sort, eg. `-created,name`
	Page       int    // 1-based
	PerPage    int
	Expand     string // comma separated relation fields to expand
	SkipTotal  bool   // skip the expensive total-count query
	Fields     string // limit the returned fields
}
```

### `PageResult`

```go
type PageResult struct {
	Records    []*Record
	Page       int
	PerPage    int
	TotalItems int
	TotalPages int
}
```

### `Record`

```go
type Record struct {
	ID             string
	CollectionID   string
	CollectionName string
	Fields         map[string]any // exported field data + custom data
	Expand         map[string]any // resolved relations
	Created        string
	Updated        string
}

func (r *Record) Field(name string) (any, bool)    // raw value + presence
func (r *Record) StringField(name string) string   // "" when absent or not a string
func (r *Record) JSON() (string, error)            // indented JSON of Fields
```

`Field` returns `(any, bool)`. The value's dynamic type depends on the
schema: `string`, `float64`, `bool`, `[]any`, `map[string]any`.

### `Collection`, `DictionaryField`, `SyncRequest`, `SyncReport`

```go
type Collection struct {
	ID   string
	Name string
	Type string // "base", "auth" or "view"
}

type DictionaryField struct {
	Name     string
	Type     string // FieldTypeText | Editor | JSON | Number | Bool | Date | Select
	Required bool
	Options  []string // for select fields
}

const (
	FieldTypeText   = "text"
	FieldTypeEditor = "editor"
	FieldTypeJSON   = "json"
	FieldTypeNumber = "number"
	FieldTypeBool   = "bool"
	FieldTypeDate   = "date"
	FieldTypeSelect = "select"
)

type SyncRequest struct {
	RemoteCollection string
	LocalCollection  string // defaults to RemoteCollection
	Filter           string
	BatchSize        int    // defaults to DefaultSyncBatchSize (200)
	MaxRecords       int    // 0 = unlimited
	Since            *time.Time
}

type SyncReport struct {
	Collection string
	Fetched    int
	Created    int
	Updated    int
	Skipped    int
	Duration   time.Duration
}
```

### Constructors

```go
func OpenEmbedded(app core.App, config EmbeddedConfig) (*Embedded, error)
func OpenRemote(config RemoteConfig) (*Remote, error)
```

```go
type EmbeddedConfig struct {
	DataDir         string          // required
	EncryptionEnv   string
	Dev             bool
	QueryTimeout    time.Duration
	Collections     []string        // allowlist; empty = all
	Migrations      []core.Migration
	SyncRemoteURL   string          // source for SyncCollection
	SyncRemoteToken string          // REQUIRED for non-public collections
	Logger          *slog.Logger
}

type RemoteConfig struct {
	BaseURL       string        // required
	Token         string
	Collections   []string      // allowlist; empty = all
	Timeout       time.Duration // defaults to DefaultRemoteTimeout (30s)
	HTTPClient    *http.Client
	RetryAttempts int           // extra attempts on network/5xx failures
}
```

**`Embedded` also exposes:**

```go
func (s *Embedded) RuntimeStore() *store.Store[string, any] // PocketBase's volatile KV cache
```

### Query helpers (`pbstore/queries.go`)

```go
func QueryParams(q Query) url.Values          // builds the native PocketBase params
func BuildFilter(filter string, params ...any) (search.Expression, error)
func SortFields(sort string) []string
func ExpandList(expand string) []string
func SearchURL(collection string) string       // /api/collections/{c}/records
func RecordURL(collection, id string) string   // /api/collections/{c}/records/{id}
func CollectionURL(collection string) string
```

`BuildFilter` is the one to use when you need to validate a filter before
sending it. It returns `ErrInvalidFilter` on a malformed expression.

---

## `internal/state` — shared runtime state

Import: `github.com/incurl/flint/internal/state`

```go
type Page string

const (
	PageHome     Page = "home"
	PageRecords  Page = "records"
	PageDetail   Page = "detail"
	PageSearch   Page = "search"
	PageSettings Page = "settings"
	PageSync     Page = "sync"
)

func AllPages() []Page
func (p Page) Title() string
```

```go
type ConnectionState string // ConnectionUnknown, ConnectionOK, ConnectionError
type SyncState string       // SyncIdle, SyncRunning, SyncDone, SyncFailed
```

```go
type Runtime struct {
	Page         Page
	PreviousPage Page
	PageNumber   int // active pagination page, 1-based
	PerPage      int
	Collection   string
	Filter       string
	Sort         string
	SelectedID   string
	Connection   ConnectionState
	LastError    string
	Sync         SyncState
	SyncProgress string
	// …more
}

func NewRuntime() *Runtime   // Page=home, PageNumber=1, PerPage=30
func (r *Runtime) SetPage(page Page)  // remembers the previous page
func (r *Runtime) GoBack()            // swaps Page and PreviousPage
func (r *Runtime) SetError(err error) // nil clears the error
```

`Runtime` is **volatile**. It is not persisted and is not a database.

---

## `internal/theme` — the palette

Import: `github.com/incurl/flint/internal/theme`

```go
type Variant string
const (
	VariantDark  Variant = "dark"
	VariantLight Variant = "light"
)

func New(variant Variant) *Theme
func Default() *Theme                    // dark
func (v Variant) Valid() bool
func (t *Theme) StatusColor(status string) lipgloss.Style
```

`Theme` carries the styles: `Title`, `Subtitle`, `Muted`, `Label`,
`Value`, `Accent`, `Error`, `Warning`, `StatusBar`, `StatusMode`,
`TableSelected`, `TableHeader`, `TableHeaderBorder`, `TableCell`, and
the `Help` style.

**Add a style to `Theme`, never build a `lipgloss.Style` inline.**

`internal/theme/layout.go` has width helpers:

```go
func SplitWidth(total, left, gap int) (int, int)
func Truncate(s string, width int) string
```

---

## `internal/components` — reusable widgets

Import: `github.com/incurl/flint/internal/components`

### `RecordRenderer` — the main extension point

```go
type RecordRenderer interface {
	Title(rec *pbstore.Record) string
	Summary(rec *pbstore.Record) string
	Fields(rec *pbstore.Record) []FieldRow
}

type FieldRow struct {
	Name  string
	Value string
}
```

The generic implementation:

```go
type DefaultRecordRenderer struct {
	TitleFields []string // preferred title fields, in order
	MaxFields   int      // 0 = 8
}
```

`RecordRenderer` drives the record table, the record card and the
search results at once. Implement it to change how records look
everywhere.

### Widgets

```go
func NewRecordCard(t *theme.Theme, renderer RecordRenderer) *RecordCard
func NewJSONViewer(t *theme.Theme) *JSONViewer
func NewJSONEditor(t *theme.Theme) *JSONEditor
func NewToast(t *theme.Theme) *Toast
func NewStatusBar(t *theme.Theme) *StatusBar
func NewPagination(t *theme.Theme, perPage int) *Pagination
```

### Toast

```go
type ToastLevel int
const (
	ToastInfo ToastLevel = iota
	ToastSuccess
	ToastError
	ToastWarning
)

type ToastMsg struct {
	Level ToastLevel
	Text  string
}

func (t *Toast) Show(level ToastLevel, text string) tea.Cmd
func (t *Toast) ShowWithDuration(level ToastLevel, text string, d time.Duration) tea.Cmd
```

The root model handles `ToastMsg`; a page just returns
`m.toast.Show(...)` as a command.

### Status bar

```go
type StatusInfo struct {
	Mode         string
	Connection   string
	Collection   string
	Page         string
	Error        string
	Sync         string
	SyncProgress string
	Hints        []string
}

func FormatHints(pairs [][2]string) []string // {"q","quit"} -> "q quit"
```

---

## `internal/pages` — the screens

Import: `github.com/incurl/flint/internal/pages`

Every page follows the same shape:

```go
func NewXPage(store pbstore.Store, runtime *state.Runtime, t *theme.Theme) *XPage

func (p *XPage) Init() tea.Cmd
func (p *XPage) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (p *XPage) View() string
func (p *XPage) Refresh() tea.Cmd   // called by the root model on navigation
```

Existing pages: `HomePage`, `RecordsPage`, `DetailPage`, `SearchPage`,
`SettingsPage`, `SyncPage`.

Messages a page may emit (defined in `pages/common.go` so the page does
not have to import `app`):

```go
type NavigateMsg struct{ Page state.Page }
type OpenRecordMsg struct{ ID string }
```

---

## `internal/app` — the root model

Import: `github.com/incurl/flint/internal/app`

```go
func New(store pbstore.Store, cfg *config.Config, renderer components.RecordRenderer) *Model

func (m *Model) Init() tea.Cmd
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m *Model) View() string
```

Root-level messages (`internal/app/messages.go`):

```go
type HealthMsg struct {
	Status string
	Err    error
}
type CollectionsMsg struct {
	Collections []pbstore.Collection
	Err         error
}
type SyncStartedMsg struct{ Collection string }
type SyncFinishedMsg struct {
	Report *pbstore.SyncReport
	Err    error
}
type Toast = components.ToastMsg
```

---

## `config` — settings

Import: `github.com/incurl/flint/config`

```go
func Load(configPath string, flags Flags) (*Config, error)
func ParseFlags(args []string) (*Flags, []string, error)
func Default() *Config
```

**Precedence: CLI flags > environment variables > TOML file.**

```go
type Config struct {
	Mode     string   `toml:"mode"`     // "embedded" | "remote"
	Theme    string   `toml:"theme"`    // "dark" | "light"
	PerPage  int      `toml:"per_page"`
	LogLevel string   `toml:"log_level"` // "debug"|"info"|"warn"|"error"
	Embedded EmbeddedConfig `toml:"embedded"`
	Remote   RemoteConfig   `toml:"remote"`
	Sync     SyncConfig     `toml:"sync"`
}
```

Environment variables are prefixed `FLINT_`: `FLINT_MODE`,
`FLINT_THEME`, `FLINT_PER_PAGE`, `FLINT_LOG_LEVEL`, `FLINT_DATA_DIR`,
`FLINT_ENCRYPTION_ENV`, `FLINT_DEV`, `FLINT_REMOTE_URL`,
`FLINT_REMOTE_TOKEN`, `FLINT_REMOTE_COLLECTIONS`, `FLINT_REMOTE_TIMEOUT`,
`FLINT_SYNC_COLLECTIONS`, `FLINT_SYNC_BATCH_SIZE`,
`FLINT_SYNC_MAX_RECORDS`.

CLI flags: `--mode`, `--theme`, `--per-page`, `--log-level`,
`--data-dir`, `--encryption-env`, `--dev`, `--remote-url`,
`--remote-token`, `--remote-collections`, `--collections`,
`--sync-collections`, `--sync-batch-size`, `--sync-max-records`,
`--migrate-only`, `--config`.

---

## PocketBase reference

The APIs used by this template, verified against PocketBase v0.40.6.

```go
// core.App — the embedded application
app.Bootstrap() error
app.RunAllMigrations() error
app.FindCachedCollectionByNameOrId(nameOrId string) (*core.Collection, error)
app.FindRecordById(collection any, recordId string, ...) (*core.Record, error)
app.RecordQuery(collection any) *dbx.SelectQuery
app.Save(model core.Model) error
app.SaveWithContext(ctx context.Context, model core.Model) error
app.Store() *store.Store[string, any]   // VOLATILE runtime KV cache

// Creating a collection in a migration
c := core.NewBaseCollection("my_collection")
c.Fields.Add(
	&core.TextField{Name: "title", Required: true, Max: 200},
	&core.JSONField{Name: "meta"},
)
err := txApp.Save(c)

// The runtime key/value store (NOT a database)
rt := app.Store()
rt.Set("key", value)
v, ok := rt.Get("key")
```

**In a migration, always use the `txApp` argument, not the outer app.**
Using the outer app escapes the transaction and the migration is no
longer atomic.

**`App.Store()` is volatile.** It is a process-local cache that PocketBase
uses to memoise the collection registry. Persistent dictionaries belong
in a real collection.

---

## The stale response guard

Every list-style message carries the page it was requested for, and the
handler discards a response that is no longer current:

```go
type RecordsMsg struct {
	Result *pbstore.PageResult
	Err    error
	Page   int // the page that was requested
}

// in Update:
case RecordsMsg:
	p.loading = false
	if msg.Err != nil {
		return p, nil
	}
	// Discard the response of a query that is no longer current.
	if msg.Page != p.runtime.PageNumber {
		return p, nil
	}
	p.applyResult(msg.Result)
	return p, nil
```

**Keep this whenever you add a paginated message.** Without it, a slow
query that resolves after the user moved on overwrites the visible rows
with stale data.

The root model applies the same guard so that a stale error does not
raise a toast:

```go
case pages.RecordsMsg:
	if msg.Page != m.runtime.PageNumber {
		break
	}
	// ...
```

---

## Filter grammar cheat sheet

Native PocketBase syntax — **not** SQL.

| Expression             | Meaning                          |
| ---------------------- | -------------------------------- |
| `title = "go"`         | equality                         |
| `title != "go"`        | inequality                       |
| `title ~ "go"`         | contains                         |
| `title !~ "go"`        | does not contain                 |
| `title ~ "go" && x > 5`| logical and                      |
| `title ~ "go" \|\| x > 5` | logical or                   |
| `id != ""`             | non-empty                        |
| `created > "2024-01-01"` | date comparison              |

Sorting: `-created,name` (descending first, then ascending).

Field names must exist in the collection schema. A filter on an unknown
field fails with `ErrInvalidFilter`.
