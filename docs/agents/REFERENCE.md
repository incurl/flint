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
	SyncCollection(ctx context.Context, req SyncRequest) (*SyncReport, error)
	Close() error
}
```

`EnsureCollection` creates a collection with the fields the application
needs. The embedded backend creates it; the remote backend cannot, and
returns an error: there, the collections are created by a migration on
the server.

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
func NewListRecordsTool(store pbstore.Store) Tool
func NewGetRecordTool(store pbstore.Store) Tool
func NewSearchRecordsTool(store pbstore.Store) Tool
func NewCountRecordsTool(store pbstore.Store) Tool
func NewListCollectionsTool(store pbstore.Store) Tool
func NewCreateRecordTool(store pbstore.Store) Tool
func NewUpdateRecordTool(store pbstore.Store) Tool
func NewDeleteRecordTool(store pbstore.Store) Tool

// The runner.
func NewRunner(options Options) *Runner
func (r *Runner) Run(ctx context.Context, request RunRequest) (string, error)
func (r *Runner) Cancel(runID string) bool
func (r *Runner) CancelAll()
func (r *Runner) Busy() bool
func (r *Runner) AddPublisher(publisher Publisher)

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
	ToolListRecords     = "list_records"
	ToolGetRecord       = "get_record"
	ToolSearchRecords   = "search_records"
	ToolCountRecords    = "count_records"
	ToolListCollections = "list_collections"
	ToolCreateRecord    = "create_record"
	ToolUpdateRecord    = "update_record"
	ToolDeleteRecord    = "delete_record"
)
```

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
- The panel is only offered on a terminal of at least 121 columns: the
  record table is 88 columns wide, the panel takes 32, and a table
  whose viewport is narrower than its columns renders *empty*.
- The page is told about the narrower width with a `tea.WindowSizeMsg`
  that carries the width of the page, not the width of the terminal.
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

The supported providers are `minimax`, `minimax-global`, `openai`,
`openaicompat` (alias `openai-compatible`), `openrouter` and `local`.
The MiniMax ones have a fixed endpoint — `api.minimax.cn` and
`api.minimax.io` — and read the key from `MINIMAX_API_KEY`.

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

**Keep this whenever you add an asynchronous message.** Without it, a
slow query that resolves after the user moved on overwrites what is on
screen with stale data.

The same rule applies to a single record, where the identity is the
record id rather than the page:

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
