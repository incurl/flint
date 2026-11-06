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
	EnsureCollection(ctx context.Context, name string, fields []CollectionField) (Collection, error)
	CollectionSchema(ctx context.Context, name string) ([]CollectionField, error)
	SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error)
	Close() error
}
```

`EnsureCollection` creates a collection with the fields the application
needs. The embedded backend creates it; the remote backend cannot, and
returns an error: there, the collections are created by a migration on
the server.

`CollectionSchema` reads the fields of a collection. It works with both
backends, and it returns the fields a caller writes — the system fields
of PocketBase are left out. A collection that does not exist is an
error (`pbstore.ErrNotFound`), and not an empty schema.

Implementations must be **safe for concurrent use** — commands run in
background goroutines and call the store while the render loop is live.

### The `Hybrid` wrapper

`hybridstore.NewHybrid` wraps a store and evaluates the JSON filters
that the native grammar cannot express. It implements `pbstore.Store`,
so the rest of the code does not know about it.

It lives in its own package — not in `pbstore` — because it imports
`internal/jsonfilter`, and `pbstore` must import nothing from
`internal/`.

```go
func NewHybrid(inner pbstore.Store, config HybridConfig) *Hybrid

type HybridConfig struct {
	// MaxLocalRecords bounds the local evaluation.
	MaxLocalRecords int
}
```

The application builds it in `cmd/tui/main.go`:

```go
store, err := newBackendStore(cfg)
if err != nil {
	return nil, err
}

return hybridstore.NewHybrid(store, hybridstore.HybridConfig{
	MaxLocalRecords: cfg.MaxLocalRecords,
}), nil
```

`Hybrid.Unwrap()` returns the wrapped store, which is useful in tests
to compare the two paths.

### The preferences

The user preferences — the theme, the UI scale and the page size —
survive a restart. They live in the embedded store, in the
`_flint_preferences` collection, which holds a single record.

```go
// The preferences of the current user.
type Preferences struct {
	Theme   string // "dark" or "light"
	Scale   int    // the UI scale, as a percentage
	PerPage int    // 0 means the deployment default
	Compact bool   // render the record list without the separators
}

func LoadPreferences(ctx context.Context, store Store) (Preferences, error)
func SavePreferences(ctx context.Context, store Store, prefs Preferences) error
```

`SavePreferences` is idempotent: it updates the single record rather
than creating a second one.

The **remote backend cannot hold the preferences**: it cannot create a
collection, so `SavePreferences` returns
`ErrPreferencesUnavailable` and the change is not persisted. The
application treats that as a normal outcome and stays silent.

The distinction matters:

| State                                       | Where it lives    |
| ------------------------------------------- | ----------------- |
| Deployment configuration (mode, data dir)   | The TOML file     |
| User preferences (theme, scale, page size)  | The PocketBase    |
| Runtime state (page, collection, filter)    | Memory only       |

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

// IsSystemCollection reports whether a name is reserved for the
// internals of PocketBase or of the application: anything behind a
// leading underscore (_superusers, _flint_sessions, ...).
func IsSystemCollection(name string) bool

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
func SortFields(sort string) []search.SortField
func ExpandList(expand string) []string
func SearchURL(collection string) string       // /api/collections/{c}/records
func RecordURL(collection, id string) string   // /api/collections/{c}/records/{id}
func CollectionURL(collection string) string
func QuoteFilterValue(v string) string         // escapes a value for a filter
```

`QuoteFilterValue` is the one to use when a filter is built by
concatenation: it escapes the quotes of the value, so that a value that
holds one cannot end the literal and change the meaning of the filter.

```go
filter := "sessionId = " + pbstore.QuoteFilterValue(sessionID)
```

A filter that comes from the model is not built this way: it is
handed to the store, which parses it with the native grammar and
refuses it when it is malformed (`ErrInvalidFilter`).

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

// Scale is the UI scale factor, as a percentage.
type Scale int
const (
	ScaleMin     Scale = 50
	ScaleMax     Scale = 200
	ScaleDefault Scale = 100
	ScaleStep    Scale = 10
)

func New(variant Variant, scale Scale) *Theme
func Default() *Theme                    // dark, 100%
func (v Variant) Valid() bool
func (t *Theme) StatusColor(status string) lipgloss.Style

// Apply rebuilds the styles in place.
func (t *Theme) Apply(variant Variant, scale Scale)

// The scale helpers.
func ScaleZoomIn(s Scale) Scale
func ScaleZoomOut(s Scale) Scale
```

`Theme` carries the styles: `Title`, `Subtitle`, `Muted`, `Label`,
`Value`, `Accent`, `Error`, `Warning`, `StatusBar`, `StatusMode`,
`TableSelected`, `TableHeader`, `TableHeaderBorder`, `TableCell`, and
the `Help` style.

**Add a style to `Theme`, never build a `lipgloss.Style` inline.**

### The theme is mutated in place

Every page holds the **same** `*theme.Theme` pointer and reads the
styles when it renders. A runtime change is therefore one call:

```go
m.theme.Apply(theme.VariantLight, theme.Scale(120))
```

Every page picks it up on the next frame. **Never replace the
pointer** — the pages would keep the old theme.

### The runtime hotkeys

| Key   | Effect                                    |
| ----- | ----------------------------------------- |
| `+`   | Zoom in (the scale grows by 10)           |
| `-`   | Zoom out                                  |
| `T`   | Switch between the dark and light palette |
| `c`   | Toggle the compact record list            |

The zoom is bounded to 50%..200%. The hotkeys are in the global key
layer, so they are suspended while a text input has the focus.

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

### The JavaScript renderer

A script can **override** the Go renderer without recompiling:

```go
// The script is loaded from the directory, then validated.
script, err := components.LoadScript(cfg.RendererDirPath())

renderer := components.NewCachedRenderer(script, nil)
if err := renderer.Validate(); err != nil {
	return err   // a broken script fails at startup
}
```

The script defines one function:

```js
function flintRender(record) {
    return {
        title:   record.label || record.id,
        summary: record.description || "",
        fields:  [{ name: "Key", value: String(record.key) }],
    }
}
```

`record` carries the record fields plus `id`, `collectionId`,
`collectionName`, `created` and `updated`.

**`CachedRenderer.Title/Summary/Fields` never evaluate the script.**
They read a cache, because they are called from `View()`. The script
runs only in `Preload`, which the pages call from the commands that
fetch the records:

```go
components.Preload(p.renderer, result.Records)
```

A record that is not cached falls back to the Go renderer, which means
the first frame of a new record shows the Go rendering and the next
frame shows the script rendering.

```go
// The preload hook.
type Preloader interface {
	RecordRenderer
	Preload(records []*pbstore.Record) int
}

func Preload(renderer RecordRenderer, records []*pbstore.Record) int

// The cached renderer.
func NewCachedRenderer(script string, fallback RecordRenderer) *CachedRenderer
func (r *CachedRenderer) Validate() error

// The script files.
const ScriptExtension = ".pb.js"
func LoadScript(dir string) (string, error)

// The per-evaluation bound.
const ScriptTimeout = 2 * time.Second
```

Configure it with `--js-renderer`, `FLINT_JS_RENDERER=1` or
`js_renderer = true`, and the directory with `--renderer-dir`,
`FLINT_RENDERER_DIR` or `renderer_dir`. The default directory is a
`flint_hooks` directory next to the data directory.

⚠️ **The script runs in its own goja runtime, not the PocketBase one.**
It has no `$app` binding: it cannot read or write the database.

### Widgets

```go
func NewRecordCard(t *theme.Theme, renderer RecordRenderer) *RecordCard
func NewJSONViewer(t *theme.Theme) *JSONViewer
func NewJSONEditor(t *theme.Theme) *JSONEditor
func NewToast(t *theme.Theme) *Toast
func NewStatusBar(t *theme.Theme) *StatusBar
func NewPagination(t *theme.Theme, perPage int) *Pagination
func NewAgentPanel(t *theme.Theme) *AgentPanel
func NewAgentSetup(t *theme.Theme) *AgentSetup
```

### The agent setup

The guided setup of the agent (`internal/components/agentsetup.go`).
It appears on the agent page when no model is configured, and it walks
the user through the provider, the model and the key.

```go
const (
	AgentSetupProvider = "provider"
	AgentSetupModel    = "model"
	AgentSetupKey      = "key"
	AgentSetupDone     = "done"
)

func AgentSetupProviders() []AgentSetupProviderInfo

func (s *AgentSetup) Update(msg tea.KeyPressMsg) (tea.Cmd, bool)
func (s *AgentSetup) View() tea.View
func (s *AgentSetup) Focus() tea.Cmd
func (s *AgentSetup) SetSize(width, height int)

// The writer. The setup writes an example configuration file.
func (s *AgentSetup) SetWriter(path string, writer func(path string, settings AgentSetupSettings) error)
func (s *AgentSetup) Write() bool
func (s *AgentSetup) Written() bool
func (s *AgentSetup) WriteError() error
func (s *AgentSetup) ConfigPath() string

// The applier. The setup installs the model, so that the agent works
// without a restart.
func (s *AgentSetup) SetApplier(applier func(settings AgentSetupSettings) error)
func (s *AgentSetup) Apply() bool
func (s *AgentSetup) Applied() bool
func (s *AgentSetup) ApplyError() error

// The result of the setup.
func (s *AgentSetup) Provider() AgentSetupProviderInfo
func (s *AgentSetup) Model() string
func (s *AgentSetup) APIKey() string
func (s *AgentSetup) KeyEnv() string
func (s *AgentSetup) Settings() AgentSetupSettings
func (s *AgentSetup) ConfigSnippet() string
func (s *AgentSetup) Summary() string

// The settings the setup collected.
type AgentSetupSettings struct {
	Provider  string
	Model     string
	BaseURL   string
	APIKeyEnv string
	// APIKey is the key the user typed. It is held in memory and it
	// is never written to a file.
	APIKey string
}
```

Four rules hold it together:

- **The key is masked.** It is typed in a `textinput` with
  `EchoPassword`, so it never reaches the screen.
- **The key is never written to a file.** `ConfigSnippet` returns the
  TOML to copy, and it has no field for the key: a secret belongs in
  the environment, not in a file that is copied around.
- **The key is used in memory.** `AgentSetupSettings.APIKey` carries it
  to the applier, which builds a model from it. That is what makes
  the agent work without a restart.
- **The writer and the applier are callbacks.** The components do not
  depend on the configuration package — they are widgets — so the
  application wires them. The application maps `AgentSetupSettings` to
  its own configuration type.

`Update` returns whether the key was handled. An unhandled key belongs
to the page.

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
func (p *XPage) View() tea.View
func (p *XPage) Refresh() tea.Cmd   // called by the root model on navigation
```

Existing pages: `HomePage`, `RecordsPage`, `DetailPage`, `SearchPage`,
`SettingsPage`, `SyncPage`.

The detail page adds one method, which the root model reads to keep the
global hotkeys out of the raw JSON editor while it is open:

```go
func (p *DetailPage) Editing() bool
```

Messages a page may emit (defined in `pages/common.go` so the page does
not have to import `app`):

```go
type NavigateMsg struct{ Page state.Page }
type OpenRecordMsg struct{ ID string }
```

The detail page adds one of its own, for the outcome of a write. It
carries the identity of the record the write was made for, so that a
response for a record the user has already left is discarded:

```go
type SavedMsg struct {
	ID  string
	Err error
}
```

### The request token

A page that fetches its data gives every request an identity, and the
answer carries it. See § *The stale response guard*.

```go
type RequestToken uint64
```

The token is issued by the page, through `nextRequest`, and compared
with `isCurrent`. A page exposes `IsCurrent` so that the root model
can ask it whether an answer is still the one it is waiting for:

```go
func (p *RecordsPage) IsCurrent(token RequestToken) bool
```

---

## `internal/agent` — the agentic layer

Import: `github.com/incurl/flint/internal/agent`

```go
// The tools.
type Tool interface {
	Name() string
	Description() string
	Run(ctx context.Context, input string) (string, error)
}

// The registry.
func NewRegistry() *Registry
func (r *Registry) Register(tool Tool)
func (r *Registry) Tools() []Tool
func (r *Registry) Get(name string) (Tool, error)
func (r *Registry) Names() []string

// The tools of a mode. In the plan mode the tools that change the
// data are left out: the model does not see them, so it cannot call
// them.
func (r *Registry) AgentTools() []fantasy.AgentTool
func (r *Registry) AgentToolsFor(mode Mode) []fantasy.AgentTool
func (r *Registry) ToolNamesFor(mode Mode) []string

// The store tools.
func RegisterStoreTools(registry *Registry, store pbstore.Store, gate *Gate)

// The task list. It belongs to the conversation of the run, which the
// tool finds in the context.
func RegisterTodosTool(registry *Registry, runner *Runner)
func NewTodosTool(store *todoStore) Tool
func (r *Runner) Todos(sessionID string) []Todo

type Todo struct {
	Content    string `json:"content"`
	Status     string `json:"status"`
	ActiveForm string `json:"active_form"`
}

const ToolTodos = "todos"

const (
	TodoPending    = "pending"
	TodoInProgress = "in_progress"
	TodoCompleted  = "completed"
)

func SortTodos(todos []Todo) []Todo
func TodoProgress(todos []Todo) (completed, total int)

// The session id travels in the context: a tool that belongs to a
// conversation needs to know which one, and the signature of a tool
// does not carry it.
func WithSessionID(ctx context.Context, sessionID string) context.Context
func SessionIDFrom(ctx context.Context) string
func NewListRecordsTool(store pbstore.Store) Tool
func NewGetRecordTool(store pbstore.Store) Tool
func NewSearchRecordsTool(store pbstore.Store) Tool
func NewCountRecordsTool(store pbstore.Store) Tool
func NewListCollectionsTool(store pbstore.Store) Tool
func NewCreateRecordTool(store pbstore.Store) Tool
func NewUpdateRecordTool(store pbstore.Store) Tool
func NewDeleteRecordTool(store pbstore.Store) Tool

// The code tools. They read and change the source code of the
// application, through a language server.
func RegisterCodeTools(registry *Registry, manager *code.Manager, gate *Gate)
func NewCodeSymbolsTool(manager *code.Manager) Tool
func NewCodeDefinitionTool(manager *code.Manager) Tool
func NewCodeReferencesTool(manager *code.Manager) Tool
func NewCodeDiagnosticsTool(manager *code.Manager) Tool
func NewCodeCallHierarchyTool(manager *code.Manager) Tool
func NewCodeSearchTool(manager *code.Manager) Tool
func NewCodeFindReferencesTool(manager *code.Manager) Tool
func NewCodeRenameTool(manager *code.Manager) Tool
func NewCodeReplaceSymbolTool(manager *code.Manager) Tool
func NewCodeCreateFileTool(manager *code.Manager) Tool
func NewCodeMoveFileTool(manager *code.Manager) Tool
func NewCodeDeleteFileTool(manager *code.Manager) Tool

const (
	ToolCodeSymbols         = "code_symbols"
	ToolCodeDefinition      = "code_definition"
	ToolCodeReferences      = "code_references"
	ToolCodeDiagnostics     = "code_diagnostics"
	ToolCodeCallHierarchy   = "code_call_hierarchy"
	ToolCodeSearch          = "code_search"
	ToolCodeFindReferences  = "code_find_references"
	ToolCodeRename          = "code_rename"
	ToolCodeReplaceSymbol   = "code_replace_symbol"
	ToolCodeCreateFile      = "code_create_file"
	ToolCodeMoveFile        = "code_move_file"
	ToolCodeDeleteFile      = "code_delete_file"
)

// The bounds of the code tools.
//
// A question about the whole workspace can have a very large answer:
// the search of a common name matches thousands of symbols, and the
// references of a widely used one match thousands of places. The
// answer would fill the context window of the model, and the run would
// end without the model having read the results.
//
// The answers are bounded, and the bound is reported: a truncated
// answer says that it is partial, so that the model knows that it has
// not seen everything.
const (
	maxCodeResults   = 100 // the symbols of a workspace search
	maxCodeLocations = 100 // the places of a reference search
)

// The runner.
func NewRunner(options Options) *Runner
func (r *Runner) Run(ctx context.Context, request RunRequest) (string, error)
func (r *Runner) Cancel(runID string) bool
func (r *Runner) CancelAll()
func (r *Runner) Busy() bool
func (r *Runner) AddPublisher(publisher Publisher)

// The wrapping of a text, on the words.
//
// The viewport soft-wraps by cutting the lines at the width, which
// splits the words in the middle.
func Wrap(text string, width int) []string

// The scroller. It shows a block of content that is taller than the
// space it has, and it follows the end while the reader is there.
func NewScroller(height int) *Scroller
func (s *Scroller) SetSize(width, height int)
func (s *Scroller) SetContent(content string)
func (s *Scroller) Content() string
func (s *Scroller) AtBottom() bool
func (s *Scroller) Follow() bool
func (s *Scroller) SetFollow(follow bool)
func (s *Scroller) GotoBottom()
func (s *Scroller) Update(msg tea.Msg) (tea.Cmd, bool)
func (s *Scroller) View() string

// The model of the runner. It can be replaced while the application
// runs: the guided setup installs one, so that the agent works without
// a restart.
func (r *Runner) Model() fantasy.LanguageModel
func (r *Runner) SetModel(model fantasy.LanguageModel) error

// The mode of the runner. It decides which tools the model can call.
func (r *Runner) Mode() Mode
func (r *Runner) SetMode(mode Mode)

// The modes.
type Mode int

const (
	// ModeAct reads and writes the records.
	ModeAct Mode = iota
	// ModePlan reads the records and proposes a plan. It cannot
	// change anything.
	ModePlan
)

func (m Mode) String() string
func (m Mode) IsPlan() bool

// The plan. The model wraps its plan in the markers, and the page
// recognises the end one to offer the plan for approval.
const PlanStartMarker = "<!-- FLINT_PLAN_START -->"
const PlanReadyMarker = "<!-- FLINT_PLAN_READY -->"

func PlanPrompt(tools []string) string
func HasPlanReady(text string) bool
func StripPlanMarkers(text string) string

// The loop detection. It stops a run that repeats the same call.
var ErrLoopDetected error

func IsLoopDetected(err error) bool
func IsMutatingTool(name string) bool

// The providers.
type ProviderConfig struct {
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
}

func NewModel(cfg ProviderConfig) (fantasy.LanguageModel, error)

// NewModelWithClient builds a model with a custom HTTP client.
//
// It is the seam a test uses to see which host the model reaches.
func NewModelWithClient(cfg ProviderConfig, client option.HTTPClient) (fantasy.LanguageModel, error)

// ErrNoAPIKey is returned when a provider needs a key and none was
// given.
//
// The request would carry no Authorization header at all, and the
// provider would answer with a message about the header.
var ErrNoAPIKey = errors.New("agent: no API key")

// The permission gate.
func NewGate(mode PermissionMode, requester PermissionRequester) *Gate
func NewGuardedTool(tool Tool, gate *Gate, description string) *GuardedTool
func NewDecisionBroker(requester RequestPublisher) *DecisionBroker
func (b *DecisionBroker) Resolve(id string, decision PermissionDecision) bool

// The conversation.
func NewHistory(service *session.Service, sessionID string) *History
func (h *History) Load(ctx context.Context) []fantasy.Message
```

The tool names are part of the contract with the model:

```go
const (
	ToolListRecords      = "list_records"
	ToolGetRecord        = "get_record"
	ToolSearchRecords    = "search_records"
	ToolCountRecords     = "count_records"
	ToolListCollections  = "list_collections"
	ToolCollectionSchema = "collection_schema"
	ToolCreateCollection = "create_collection"
	ToolCreateRecord     = "create_record"
	ToolUpdateRecord     = "update_record"
	ToolDeleteRecord     = "delete_record"
)
```

### The Wikidata tools

Three tools talk to Wikidata:

| Tool                 | What it does                                     | Gated? |
| -------------------- | ------------------------------------------------ | ------ |
| `wikidata_enrich`    | Enriches a collection with the facts of Wikidata | **yes** |
| `wikidata_analyze`   | Reports what the enrichment achieved            | no     |
| `wikidata_generate`  | Writes a Go project that reads the enriched data | **yes** |

`wikidata_enrich` writes the facts of Wikidata into the data of the
user, exactly like a record does: the plan proposes it, the act mode
carries it out. It is behind the permission gate.

`wikidata_analyze` only reads, so the planning mode may use it: a plan
for an enrichment needs to know what the enrichment achieved.

`wikidata_generate` writes files, so it is gated like the code tools:
the plan proposes the project, the act mode writes it. The project is
written inside the **root** — the code directory of the application —
and a path that escapes it is refused.

```go
// The three tools.
func NewWikidataEnrichTool(store pbstore.Store, client *wikidata.Client, logger *slog.Logger) Tool
func NewWikidataAnalyzeTool(store pbstore.Store, client *wikidata.Client, logger *slog.Logger) Tool
func NewWikidataGenerateTool(store pbstore.Store, root string, logger *slog.Logger) Tool

// The registration.
func RegisterWikidataTools(registry *Registry, store pbstore.Store, client *wikidata.Client, gate *Gate, root string, logger *slog.Logger)
```

The tools are only registered when the configuration asks for them: they
reach a public endpoint, and an application that never asks about
Wikidata must not open a connection to it.

The enrichment only works with the **embedded** backend, because it
creates the collection of the facts. The remote backend cannot create a
collection — there, the collections are created by a migration on the
server — and the error says so.

The **generation** works with both: it reads the schema through the
store, and the store reads a remote schema over REST.

#### The arguments

```go
// The arguments of the enrichment.
type WikidataEnrichParams struct {
    Collection string `json:"collection"`
    EntityType string `json:"entity_type,omitempty"`
    SampleSize int    `json:"sample_size,omitempty"`
    DryRun     bool   `json:"dry_run,omitempty"`
}

// The arguments of the analysis.
type WikidataAnalyzeParams struct {
    Collection string `json:"collection"`
}

// The arguments of the generation.
type WikidataGenerateParams struct {
    Collection string `json:"collection"`
    Kind       string `json:"kind,omitempty"`
    OutputDir  string `json:"output_dir,omitempty"`
    ModulePath string `json:"module_path,omitempty"`
    Overwrite  bool   `json:"overwrite,omitempty"`
}
```

The `sample_size` is capped at `wikidata.MaxSampleSize`. Every record is
a query, and the queries are rate limited.

#### What the user says

> **Enrich the `people` collection with the facts from Wikidata.**

→ `wikidata_enrich` with `{"collection": "people"}`

> **How many of the people did Wikidata know about?**

→ `wikidata_analyze` with `{"collection": "people"}`

> **Check what Wikidata would tell us before you change anything.**

→ `wikidata_enrich` with `{"collection": "people", "dry_run": true}`

> **Generate a small API that serves the people and their facts.**

→ `wikidata_generate` with `{"collection": "people", "kind": "api"}`

> **Regenerate it — the collection has a new field.**

→ `wikidata_generate` with `{"collection": "people", "overwrite": true}`

The `overwrite` is explicit: without it, the call fails with
`generator.ErrProjectExists`, and the error names the remedy. The files
of a generated project belong to the user once they are written.

### The collection tools

Two tools speak about the shape of a collection, and not about its
records:

| Tool                 | What it does                                 | Gated? |
| -------------------- | -------------------------------------------- | ------ |
| `collection_schema`  | Describes the fields of a collection          | no     |
| `create_collection`  | Creates a collection with the given fields    | **yes** |

`collection_schema` is read-only, and it is available in both backends.
It is the tool that lets a model write a record without guessing the
field names: the schema holds the names, the types, and whether the
fields are required.

`create_collection` is behind the permission gate, and it only works
with the **embedded** backend. The remote backend cannot create a
collection — there, the collections are created by a migration on the
server, and the error says so.

```go
// The two tools.
func NewCollectionSchemaTool(store pbstore.Store) Tool
func NewCreateCollectionTool(store pbstore.Store) Tool
```

The behaviour of the creation is worth stating plainly, because it is
not the one of a schema editor:

- A collection that does **not** exist is created with the requested
  fields.
- A collection that **already exists** is left as it is. Its fields are
  **not** changed, and the answer reports the fields the collection
  really holds — not the ones that were asked for. The call creates a
  collection; it does not alter a schema.
- A name that begins with an underscore is refused: those collections
  hold the internals of PocketBase or of the application, and they are
  not the data of the user.
- An unknown field type is refused, and the error names the type. The
  supported types are `text`, `editor`, `json`, `number`, `bool`,
  `date`, `select` and `autodate`.

The schema of a collection is read through the store:

```go
// In the pbstore.Store interface.
CollectionSchema(ctx context.Context, name string) ([]CollectionField, error)
```

It returns the fields a caller writes: the system fields of PocketBase
— the id, the creation date, the update date — are left out. A
collection that does not exist is an error (`pbstore.ErrNotFound`), and
not an empty schema: an empty answer for a collection that is not there
would be read as a collection with no fields.

---

## `internal/wikidata` — the facts of Wikidata

The package enriches a PocketBase collection with the facts of Wikidata.
It asks a SPARQL endpoint about the entities of a collection, writes what
it learns into a `wikidata_<collection>` collection, and reports what the
join between the two holds.

The data layer is reached through `pbstore.Store` and never through the
PocketBase app: the store is the only thing that knows whether the data is
local or remote.

### The client

```go
// The client.
func NewClient(endpoint string, opts ...Option) *Client
func (c *Client) Query(ctx context.Context, query string) (*graph.Graph, error)
func (c *Client) QueryRows(ctx context.Context, query string) (*sparql.Result, error)
func (c *Client) Endpoint() string

// The options.
func WithDelay(d time.Duration) Option
func WithTimeout(d time.Duration) Option
func WithMaxSolutions(n int) Option
func WithLogger(l *slog.Logger) Option
func WithHTTPClient(c *http.Client) Option
```

`QueryRows` is the method the enrichment uses: a `SELECT` is answered by
its variable names and its rows, and a `*graph.Graph` holds triples and
not bindings. `Query` is the CONSTRUCT-shaped answer.

Every query is bounded **twice**: by a deadline, which bounds the time,
and by a budget of solutions, which bounds the memory. A query that
spends the budget fails with `wikidata.ErrTooManySolutions` — it is not a
partial answer.

The client waits `WithDelay` between two queries. The public endpoint
asks for it, and a client that does not wait is a client that gets
blocked.

The errors are the sentinels: `ErrTooManySolutions`, `ErrTimeout`,
`ErrUnknownEntityType` and `ErrNoEndpoint`.

### The query builder

```go
// The query builder.
func BuildQueryFromPocketBase(record *core.Record, entityType string) (string, error)
func InferProperties(record *core.Record, entityType string) []Property
func QuoteLiteral(value string) string
```

The **entity type** selects the properties to ask for, and the **fields
of the record** select the entity to ask about:

- a record that holds a `wikidata_id` (or a `qid`) is asked about
  directly: the identifier is data, and it is never hardcoded;
- a record that only holds a name is searched for by that name.

Every value that goes into the query text is escaped by `QuoteLiteral`. A
value that holds a double quote would otherwise end the literal and change
the meaning of the query.

The property sets are **data**, and the registry is the extension point:

```go
// The registry.
func RegisterPropertySet(set PropertySet) error
func RegisterFieldProperty(field, property string) error
func PropertySetFor(entityType string) (PropertySet, error)
func KnownEntityTypes() []string
```

A downstream project registers the properties of an entity type of its
own, and the query builder picks them up:

```go
wikidata.RegisterPropertySet(wikidata.PropertySet{
    EntityType:  "species",
    Description: "A species of animal.",
    Properties: []wikidata.Property{
        {IRI: "wdt:P141", Label: "conservation status", FullIRI: wikidata.PrefixWDT + "P141"},
    },
})
```

A registration that adds a property to a known entity type **merges**:
the properties of the template are kept.

`RegisterFieldProperty` is the second half: it says what a field name is
worth, so that a collection that holds a field the template does not know
about is still enriched.

The registry is guarded by a mutex: the tools of a step may run in
parallel, and a registration may happen while a query is being built.

`InferProperties` is the inference: it looks at the field names of the
record (`birth_date` → `wdt:P569`, `country` → `wdt:P17`) and returns
the union of that and the properties of the entity type. A field name
that is not in the table contributes nothing and does not break the
inference.

### The planner and the executor

```go
// The planner.
type Planner interface {
    Plan(ctx context.Context, state State) (Action, error)
}
func NewStorePlanner(store pbstore.Store, logger *slog.Logger) *StorePlanner
func ReadState(ctx context.Context, store pbstore.Store, target string, refreshInterval time.Duration) (State, error)

// The executor.
type Executor interface {
    Execute(ctx context.Context, action Action) (Outcome, error)
}
func NewAgentExecutor(store pbstore.Store, client *Client, opts ExecutorOptions) *AgentExecutor
```

The planner **decides**, it does not follow a script:

| Situation                                          | Action         |
| -------------------------------------------------- | -------------- |
| No `wikidata_<target>` collection exists            | `ActionCreate` |
| The collection exists but is empty                  | `ActionEnrich` |
| It is populated and the last run is too old         | `ActionRefresh`|
| It is populated and was just enriched               | `ActionStop`   |

The executor writes **idempotently**: a fact is looked up by its natural
key (the entity, the property and the record it was read from) with
`store.FindFirst`, and it is updated rather than written twice. The
store has no transaction API, so a run that fails half way leaves the
facts it wrote, and a second run completes the work without duplicating
it.

The runs are recorded in the `agent_log` collection. The log is
append-only: a repeated run writes a second record, because a log of the
runs is a list of the runs. The **facts** are the ones that must not be
written twice.

A run reads at most `wikidata.MaxSampleSize` records. Every record is a
query, and the queries are rate limited: a model that asks for a million
records must not hold the endpoint for a day.

A **dry run** changes nothing at all: it does not create the collection,
it does not write the facts, and it does not leave a record in the log.

### The analysis

```go
// The analysis.
func AnalyzeWithWikidata(ctx context.Context, store pbstore.Store, originalCollectionName string) (*AnalysisReport, error)
func AnalyzeWithWikidataConfig(ctx context.Context, store pbstore.Store, originalCollectionName string, opts AnalysisOptions) (*AnalysisReport, error)
func EnrichedCollectionName(collection string) string
```

A collection that was never enriched is **not an error**: the report
holds zero enriched records, and the model can enrich the collection and
ask again. The two backends report a missing collection differently, and
the analysis knows both.

The join is **exact** when the original collection holds a Wikidata
identifier, and **fuzzy** when it does not: a record is then matched
against the labels of the facts by its name, with `github.com/sahilm/fuzzy`.

The fuzzy match reports its confidence. The score of the fuzzy library is
an unbounded number, so it is divided by the score of the name against
itself: the result is a ratio between zero and one, and a match below
the threshold is not counted. A guess is not a fact, and a report that
counts the guesses as facts is a lie.

The report is logged as JSON at INFO level, so that a downstream consumer
can parse the log line.

---

## `internal/generator` — the generated project

The generator writes a Go project that reads an enriched PocketBase
schema. It is the last step of the enrichment.

```go
// The generator.
func Generate(ctx context.Context, store pbstore.Store, opts Options) error

// The options.
type Options struct {
    OriginalCollection string
    EnrichedCollection string
    Kind               string
    OutputDir          string
    Root               string
    ModulePath         string
    PocketBaseURL      string
}
```

The schema is read through `pbstore.Store`, and not through the
PocketBase app: the store is the only thing that knows whether the data
is local or remote, and a generator that reads the app can only
generate from the local data. The `wikidata_*` collection of a remote
server is generated from like any other.

The **root** is the boundary of the output. The output directory is
relative to it, and a path that escapes it — through a `..` or through
a symbolic link — is refused with `generator.ErrOutsideRoot`. The check
is lexical, exactly like the one of the code tools.

The **kind** decides the shape of the application: `cli`, `api` or
`dashboard`.

The PocketBase field types are mapped to Go types with an explicit
switch. A type that is not known maps to `string` — and never to `any`,
which would lose the shape of the value without saying so.

The schema that the generator reads is the one of the store: the system
fields are left out, and a field type that the store does not describe
— a relation, for instance — is left out with them.

A field name that is not a valid Go identifier is converted (`first_name`
→ `FirstName`), and a name that starts with a digit is prefixed
(`2nd_place` → `Field2NdPlace`). The generated code is formatted, and it
is verified by a test that **builds** it.

The generated project reads the records over the REST API of PocketBase,
with the standard library alone. Its `go.mod` therefore holds **no
requirement**: a requirement the project does not use is one that
`go mod tidy` removes, and a `go.mod` that a tidy changes was not right.

A project that is already there is **not** overwritten. The files of a
generated project belong to the user once they are written, and a run
that replaced them would take the edits with it. The overwrite is
explicit:

```go
// The options.
type Options struct {
    // ...
    // Overwrite allows the generation over an existing project.
    Overwrite bool
}
```

---

## `internal/code` — the code of the application

Import: `github.com/incurl/flint/internal/code`

The package wraps a language server — `gopls`, for Go — and answers
the questions a model asks about a codebase. It is deliberately free
of any dependency on the agent: it knows about files and symbols, not
about models and permissions.

```go
// The manager owns the language server.
func NewManager(root string, logger *slog.Logger) (*Manager, error)
func (m *Manager) Root() string
func (m *Manager) Close()

// The questions.
func (m *Manager) Symbols(ctx context.Context, path string) ([]Symbol, error)
func (m *Manager) Definition(ctx context.Context, path string, line, column int) ([]Location, error)
func (m *Manager) References(ctx context.Context, path string, line, column int) ([]Location, error)

// The rename. It returns the edits of the text and the operations on
// the files themselves: a server may ask for both.
func (m *Manager) Rename(ctx context.Context, path string, line, column int, newName string) ([]Edit, []FileOperation, error)

// The problems the server found.
func (m *Manager) Diagnostics(ctx context.Context, path string) ([]Diagnostic, error)
func (m *Manager) AllDiagnostics(ctx context.Context) ([]Diagnostic, error)

// The call hierarchy: who calls a symbol, and what it calls.
func (m *Manager) Callers(ctx context.Context, path string, line, column int) ([]Call, error)
func (m *Manager) Callees(ctx context.Context, path string, line, column int) ([]Call, error)

// The replacement of a declaration. The action is "replace",
// "add_before", "add_after" or "delete".
func (m *Manager) ReplaceSymbol(ctx context.Context, path, name, action, replacement string) (string, error)

// The search of the workspace, and the uses of a symbol.
func (m *Manager) Search(ctx context.Context, query string) ([]SearchResult, error)
func (m *Manager) ReferencesOf(ctx context.Context, name string) ([]Location, error)

// The files themselves.
func (m *Manager) CreateFile(path string) error
func (m *Manager) MoveFile(from, to string) error
func (m *Manager) DeleteFile(path string, recursive bool) error

// The writes. The caller decides whether they happen: the tools that
// write are behind the permission gate.
func (m *Manager) Apply(edits []Edit) error
func (m *Manager) ApplyContent(path, content string) error
func (m *Manager) ApplyFileOperations(operations []FileOperation) error

type Symbol struct {
	Name     string
	Kind     string
	Line     int
	Detail   string
	Children []Symbol
}

type Location struct {
	Path   string
	Line   int
	Column int
	Text   string
}

type Edit struct {
	Path  string
	Start int
	End   int
	Line  int
	Text  string
}

type Diagnostic struct {
	Path     string
	Line     int
	Column   int
	Severity string
	Message  string
	Source   string
	Code     string
}

type Call struct {
	Name   string
	Path   string
	Line   int
	Column int
}

type FileOperation struct {
	Kind      string // "create", "rename" or "delete"
	Path      string
	OldPath   string
	Overwrite bool
	Recursive bool
}

type SearchResult struct {
	Name      string
	Kind      string
	Path      string
	Line      int
	Column    int
	Container string
}
```

⚠️ **The root is the boundary.** A path outside of it is refused, both
when it is read and when the edits are written. The check is lexical:
a symbolic link inside the root that points outside of it is not
followed.

⚠️ **The manager is safe for concurrent use.** The tools of a single
step may run in parallel, and two of them may ask for the server at
the same time. The server is started once — the start is guarded by a
mutex of its own — and it is published under the lock of the client.
The start happens outside of that lock: it takes a minute, and a
`Close` must not wait for it.

⚠️ **The server is looked up on the PATH.** When it is missing, the
error says so — `gopls is not on the PATH` — and the lookup is not
repeated for thirty seconds.

⚠️ **The column is corrected.** A model reads a file and knows the
line of a symbol; the column is often a guess. The column is
therefore corrected to the first identifier of the line when it does
not point at one.

---

## `internal/session` — the conversations

Import: `github.com/incurl/flint/internal/session`

```go
const (
	SessionsCollection = "_flint_sessions"
	MessagesCollection = "_flint_messages"
)

func NewService(store pbstore.Store) *Service
func EnsureCollections(ctx context.Context, store pbstore.Store) error

func (s *Service) CreateSession(ctx context.Context, title string) (*Session, error)
func (s *Service) Session(ctx context.Context, id string) (*Session, error)
func (s *Service) Sessions(ctx context.Context, limit int) ([]Session, error)
func (s *Service) DeleteSession(ctx context.Context, id string) error
func (s *Service) AppendMessage(ctx context.Context, sessionID string, message Message) (*Message, error)
func (s *Service) Messages(ctx context.Context, sessionID string, limit int) ([]Message, error)
```

---

## `internal/app` — the root model

Import: `github.com/incurl/flint/internal/app`

```go
func New(store pbstore.Store, cfg *config.Config, renderer components.RecordRenderer, setup *agent.Setup) *Model

func (m *Model) Init() tea.Cmd
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)
func (m *Model) View() tea.View
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

The root model also owns the configuration, and it wires the writer of
the agent setup:

```go
func (m *Model) SetConfigPath(path string)
```

### The agent panel

The root model owns a read-only panel that shows the conversation of
the agent next to the page (`internal/app/panel.go`).

```go
const (
	panelColumnWidth = components.AgentPanelWidth  // 32
	panelGap         = 1
	panelMinWidth    = 121
	panelMinHeight   = 12
)
```

- `ctrl+b` toggles it. The key is left to the page when the terminal
  is too small, so that a page may use it for something else.
- `ctrl+b` is a global hotkey, and the global layer is suspended while
  a text input has the focus. The toggle is therefore unreachable on
  the agent and the search pages. The status bar does not advertise it
  there, and a panel that is open when the user navigates to such a
  page is closed: the key that closes it does not work there, and the
  user would be left with a panel they cannot get rid of.
- The panel is only offered on a terminal of at least 121 columns: the
  record table is 88 columns wide, the panel takes 32, and a table
  whose viewport is narrower than its columns renders *empty*.
- The page is told about the narrower width with a `tea.WindowSizeMsg`
  that carries the width of the page, not the width of the terminal.
  The height is forwarded the same way: the status bar takes a line,
  and a page that thinks it has the whole terminal renders one line
  too many.
- The panel shows the conversation of the agent page. The events of a
  run reach the agent page whatever page is displayed, so a run keeps
  streaming after the user navigates away.

The widget itself is `components.AgentPanel`:

```go
func NewAgentPanel(t *theme.Theme) *AgentPanel
func (p *AgentPanel) SetEntries(entries []AgentPanelEntry)
func (p *AgentPanel) SetSize(width, height int)
func (p *AgentPanel) View() tea.View
```

---

## `config` — settings

Import: `github.com/incurl/flint/config`

```go
func Load(configPath string, flags Flags) (*Config, error)
func ParseFlags(args []string) (*Flags, []string, error)
func Default() *Config

// The writer.
func Write(cfg *Config, path string) error
func Render(cfg *Config) string
func WritePath(explicit string) string

// The methods of a configuration.
func (c *Config) Path() string
func (c *Config) FileValues() Config
func (c *Config) Save() error
func (c *Config) SaveAgent(agent AgentConfig, path string) error
```

**Precedence: CLI flags > environment variables > TOML file.**

```go
type Config struct {
	Mode     string   `toml:"mode"`     // "embedded" | "remote"
	Theme    string   `toml:"theme"`    // "dark" | "light"
	PerPage  int      `toml:"per_page"`
	LogLevel string   `toml:"log_level"` // "debug"|"info"|"warn"|"error"
	// MaxLocalRecords bounds the local JSON filter evaluation.
	MaxLocalRecords int `toml:"max_local_records"`
	Embedded EmbeddedConfig `toml:"embedded"`
	Remote   RemoteConfig   `toml:"remote"`
	Sync     SyncConfig     `toml:"sync"`
	// Agent holds the language model settings.
	Agent AgentConfig `toml:"agent"`
	// Code holds the settings of the code tools.
	Code CodeConfig `toml:"code"`
	// Wikidata holds the settings of the Wikidata tools.
	Wikidata WikidataConfig `toml:"wikidata"`
}

type CodeConfig struct {
	// Root is the directory the agent is allowed to read. It defaults
	// to a "flint_code" directory next to the data directory.
	Root string `toml:"root"`
	// Enabled turns the code tools on. They are on by default.
	Enabled bool `toml:"enabled"`
}

type WikidataConfig struct {
	// Enabled turns the Wikidata tools on. They are off by default:
	// they reach a public endpoint, and an application that never
	// asks about Wikidata must not open a connection to it.
	Enabled bool `toml:"enabled"`
	// Endpoint is the SPARQL endpoint. It defaults to the public
	// endpoint of Wikidata.
	Endpoint string `toml:"endpoint"`
	// DelayMS is the pause between two queries, in milliseconds. It
	// defaults to 100.
	DelayMS int `toml:"delay_ms"`
	// SampleSize is the number of records a run asks about.
	SampleSize int `toml:"sample_size"`
	// MaxSolutions is the budget of solutions of a single query.
	MaxSolutions int `toml:"max_solutions"`
}

type EmbeddedConfig struct {
	DataDir       string `toml:"data_dir"`
	EncryptionEnv string `toml:"encryption_env"`
	Dev           bool   `toml:"dev"`
	// HooksDir holds the JavaScript application hooks.
	HooksDir string `toml:"hooks_dir"`
	// MigrationsDir holds the JavaScript migrations.
	MigrationsDir string `toml:"migrations_dir"`
	// JavaScript enables the PocketBase JavaScriptVM plugin.
	JavaScript bool `toml:"javascript"`
}

// The agent settings. The agent is optional: without a provider the
// application still runs.
type AgentConfig struct {
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	// BaseURL overrides the endpoint of the provider.
	BaseURL string `toml:"base_url"`
	// APIKeyEnv names the environment variable holding the API key.
	//
	// The key is NEVER read from the configuration file.
	APIKeyEnv string `toml:"api_key_env"`
	// SystemPrompt is the instruction given to the model.
	SystemPrompt string `toml:"system_prompt"`
	// PermissionMode is "ask" or "allow".
	//
	// Any value that is not "allow" falls back to "ask": a typo must
	// not turn the questions off.
	PermissionMode string `toml:"permission_mode"`
	// MaxSteps bounds the model calls of a single run.
	MaxSteps int `toml:"max_steps"`
}
```

The supported providers are `minimax`, `minimax-cn`, `openai`,
`openaicompat` (alias `openai-compatible`), `openrouter` and `local`.
The MiniMax ones have a fixed endpoint — `api.minimax.io` for
`minimax` and `api.minimax.cn` for `minimax-cn` — and read the key
from `MINIMAX_API_KEY`. The two platforms have separate accounts and
separate balances: a key from one is refused by the other with an
"insufficient balance" error.

// The resolved JavaScript directories. They default to a "pb_hooks"
// and a "pb_migrations" directory next to the data directory.
func (c *Config) HooksDir() string
func (c *Config) MigrationsDir() string
```

Environment variables are prefixed `FLINT_`: `FLINT_MODE`,
`FLINT_THEME`, `FLINT_PER_PAGE`, `FLINT_LOG_LEVEL`,
`FLINT_MAX_LOCAL_RECORDS`, `FLINT_DATA_DIR`, `FLINT_HOOKS_DIR`,
`FLINT_MIGRATIONS_DIR`, `FLINT_JAVASCRIPT`,
`FLINT_ENCRYPTION_ENV`, `FLINT_DEV`, `FLINT_REMOTE_URL`,
`FLINT_REMOTE_TOKEN`, `FLINT_REMOTE_COLLECTIONS`, `FLINT_REMOTE_TIMEOUT`,
`FLINT_SYNC_COLLECTIONS`, `FLINT_SYNC_BATCH_SIZE`,
`FLINT_SYNC_MAX_RECORDS`.

CLI flags: `--mode`, `--theme`, `--per-page`, `--log-level`,
`--max-local-records`, `--data-dir`, `--hooks-dir`, `--migrations-dir`,
`--javascript`, `--encryption-env`, `--dev`,
`--remote-url`, `--remote-token`, `--remote-collections`, `--collections`,
`--sync-collections`, `--sync-batch-size`, `--sync-max-records`,
`--migrate-only`, `--check-agent`, `--config`.

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

Every asynchronous message carries the identity of the request it
belongs to, and the handler discards an answer that is no longer
current.

For a list, the identity is a **request token**: a counter that gives
every request its own identity.

```go
type RecordsMsg struct {
	Result *pbstore.PageResult
	Err    error
	Page   int             // the page that was requested
	Token  RequestToken    // the request the answer belongs to
}

// in Update:
case RecordsMsg:
	p.loading = false
	// Discard the answer of a request the user has moved away from.
	if !msg.Token.isCurrent(p.request) {
		return p, nil
	}
	if msg.Err != nil {
		return p, nil
	}
	p.applyResult(msg.Result)
	return p, nil
```

The token is issued when the request is started, and it travels with
the answer:

```go
func (p *RecordsPage) Load() tea.Cmd {
	// ...
	token := nextRequest(&p.request)

	return func() tea.Msg {
		// ...
		return RecordsMsg{Result: result, Page: page, Token: token}
	}
}
```

**A page number is not enough.** Two requests for the same page with
two different filters are two different requests, and the slower one
must not overwrite the other. The page number alone cannot tell them
apart: both are page 1.

**Keep this whenever you add an asynchronous message.** Without it, a
slow query that resolves after the user moved on overwrites what is on
screen with stale data.

The same rule applies to a single record, where the identity is the
record id rather than a token:

```go
type DetailMsg struct {
	Record *pbstore.Record
	Err    error
	ID     string // the record that was requested
}

// in Update:
case DetailMsg:
	if msg.ID != p.runtime.SelectedID {
		return p, nil
	}
```

Open record A, go back, open B before A's response lands: without the
id, the late response for A is applied while `SelectedID` says B, and
the detail page shows the wrong record.

The root model applies the same guard so that a stale error does not
raise a toast. It asks the page, because the page holds the counter:

```go
case pages.RecordsMsg:
	if !m.records.IsCurrent(msg.Token) {
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

### JSON fields

The native grammar reaches into the nested **objects** of a JSON field:

```
meta.lang = "go"           // equality
meta.level > 2             // numeric comparison
meta.nested.deep.value = "x"   // any depth
```

It cannot reach into an **array**. The expression parser rejects the
`[` character, and an array field compared with `=` matches nothing:

```
meta.tags = "cli"          // matches nothing, silently
meta.tags[0] = "cli"       // error: unexpected character '['
```

The `hybridstore.Hybrid` wrapper routes those expressions to a local
evaluation instead:

```
meta.tags.#(=="cli")       // any element equals "cli"
meta.items.0.name = "x"    // the "name" of the first element
meta.items.#.qty > 5      // a numeric comparison across the elements
not (meta.lang = "go")     // a negation
```

The routing is **syntactic**: `jsonfilter.NeedsLocal` decides before the
query is sent, so the two layers never disagree and the behaviour is
identical on the embedded and the remote backend.

The local evaluation is bounded by `max_local_records` (default 1000,
flag `--max-local-records`). When the bound is reached,
`PageResult.Truncated` is set and the pagination shows `(partial)`, so
a partial count is never presented as a complete one.
