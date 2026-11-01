# Appendix B — PocketBase API Reference

A quick reference for `github.com/pocketbase/pocketbase@v0.40.6`.

## The packages

| Package       | Contents                                            |
| ------------- | --------------------------------------------------- |
| `core`        | The data model, the queries, the hooks, the migrations |
| `apis`        | The REST API layer (optional for a library user)     |
| `tools/dbx`   | The query builder                                    |
| `tools/search`| The filter grammar, the sort syntax, the provider    |
| `tools/store` | The runtime key/value store                          |
| `tools/hook`  | The hook (middleware) system                         |
| `tools/list`  | Generic slice helpers                                |
| `tools/types` | The `DateTime`, `GeoPoint` and `JSONRaw` types       |
| `migrations`  | The system migrations                                |

## Creating the app

```go
package pocketbase

func New() *PocketBase
func NewWithConfig(config Config) *PocketBase

type Config struct {
    HideStartBanner      bool
    DefaultDev           bool
    DefaultDataDir       string
    DefaultEncryptionEnv string
    DefaultQueryTimeout  time.Duration
    DataMaxOpenConns     int
    DataMaxIdleConns     int
    AuxMaxOpenConns      int
    AuxMaxIdleConns      int
    DBConnect            core.DBConnectFunc
}
```

## The `core.App` interface (the useful subset)

### Lifecycle

```go
Bootstrap() error
ClearBootstrap() error
ResetBootstrapState() error   // deprecated
IsBootstrapped() bool
IsTransactional() bool
DataDir() string
EncryptionEnv() string
IsDev() bool
Logger() *slog.Logger
Settings() *Settings
Store() *store.Store[string, any]
Cron() *cron.Cron
SubscriptionsBroker() *subscriptions.Broker
```

### Migrations

```go
RunSystemMigrations() error
RunAppMigrations() error
RunAllMigrations() error
```

### The databases

```go
DB() dbx.Builder
ConcurrentDB() dbx.Builder
NonconcurrentDB() dbx.Builder
AuxDB() dbx.Builder
AuxConcurrentDB() dbx.Builder
AuxNonconcurrentDB() dbx.Builder
HasTable(tableName string) bool
TableColumns(tableName string) ([]string, error)
```

### The models

```go
Save(model Model) error
SaveWithContext(ctx context.Context, model Model) error
SaveNoValidate(model Model) error
Delete(model Model) error
DeleteWithContext(ctx context.Context, model Model) error
Validate(model Model) error
ValidateWithContext(ctx context.Context, model Model) error
RunInTransaction(fn func(txApp App) error) error
```

### The collections

```go
FindAllCollections(collectionTypes ...string) ([]*Collection, error)
FindCollectionByNameOrId(nameOrId string) (*Collection, error)
FindCachedCollectionByNameOrId(nameOrId string) (*Collection, error)
ReloadCachedCollections() error
CollectionQuery() *dbx.SelectQuery
```

### The records

```go
RecordQuery(collectionModelOrIdentifier any) *dbx.SelectQuery
FindAllRecords(collectionModelOrIdentifier any, exprs ...dbx.Expression) ([]*Record, error)
FindRecordById(collectionModelOrIdentifier any, recordId string, optFilters ...func(q *dbx.SelectQuery) error) (*Record, error)
FindRecordsByIds(collectionModelOrIdentifier any, recordIds []string, optFilters ...func(q *dbx.SelectQuery) error) ([]*Record, error)
FindRecordsByFilter(collectionModelOrIdentifier any, filter string, sort string, limit int, offset int, params ...dbx.Params) ([]*Record, error)
FindFirstRecordByFilter(collectionModelOrIdentifier any, filter string, params ...dbx.Params) (*Record, error)
ExpandRecord(record *Record, expands []string, optFetchFunc ExpandFetchFunc) map[string]error
ExpandRecords(records []*Record, expands []string, optFetchFunc ExpandFetchFunc) map[string]error
```

### The hooks (a subset)

```go
OnRecordValidate(tags ...string) *hook.TaggedHook[*RecordEvent]
OnRecordCreate(tags ...string) *hook.TaggedHook[*RecordEvent]
OnRecordCreateExecute(tags ...string) *hook.TaggedHook[*RecordEvent]
OnRecordAfterCreateSuccess(tags ...string) *hook.TaggedHook[*RecordEvent]
OnRecordAfterCreateError(tags ...string) *hook.TaggedHook[*RecordErrorEvent]
OnRecordUpdate(tags ...string) *hook.TaggedHook[*RecordEvent]
OnRecordUpdateExecute(tags ...string) *hook.TaggedHook[*RecordEvent]
OnRecordAfterUpdateSuccess(tags ...string) *hook.TaggedHook[*RecordEvent]
OnRecordAfterUpdateError(tags ...string) *hook.TaggedHook[*RecordErrorEvent]
OnRecordDelete(tags ...string) *hook.TaggedHook[*RecordEvent]
OnRecordDeleteExecute(tags ...string) *hook.TaggedHook[*RecordEvent]
OnRecordAfterDeleteSuccess(tags ...string) *hook.TaggedHook[*RecordEvent]
OnRecordAfterDeleteError(tags ...string) *hook.TaggedHook[*RecordErrorEvent]
OnRecordEnrich(tags ...string) *hook.TaggedHook[*RecordEnrichEvent]
OnModelCreate(tags ...string) *hook.TaggedHook[*ModelEvent]
OnModelUpdate(tags ...string) *hook.TaggedHook[*ModelEvent]
OnModelDelete(tags ...string) *hook.TaggedHook[*ModelEvent]
OnModelValidate(tags ...string) *hook.TaggedHook[*ModelEvent]
OnBootstrap() *hook.Hook[*BootstrapEvent]
OnTerminate() *hook.Hook[*TerminateEvent]
```

## The `core.Collection`

```go
type Collection struct {
    baseCollection
    collectionAuthOptions
    collectionViewOptions
}

type baseCollection struct {
    BaseModel

    ListRule   *string
    ViewRule   *string
    CreateRule *string
    UpdateRule *string
    DeleteRule *string

    Name    string
    Type    string
    Fields  FieldsList
    Indexes types.JSONArray[string]
    Created types.DateTime
    Updated types.DateTime

    System bool
}
```

### The constructors

```go
func NewCollection(typ, name string, optId ...string) *Collection
func NewBaseCollection(name string, optId ...string) *Collection
func NewAuthCollection(name string, optId ...string) *Collection
func NewViewCollection(name string, optId ...string) *Collection
```

### The type constants

```go
CollectionTypeBase = "base"
CollectionTypeAuth = "auth"
CollectionTypeView = "view"
```

## The `core.Record`

```go
type Record struct {
    collection       *Collection
    originalData     map[string]any
    customVisibility *store.Store[string, bool]
    data             *store.Store[string, any]
    expand           *store.Store[string, any]

    BaseModel

    exportCustomData      bool
    ignoreEmailVisibility bool
    ignoreUnchangedFields bool
}
```

### The constructors and the accessors

```go
func NewRecord(collection *Collection) *Record

func (m *Record) Load(data map[string]any)
func (m *Record) Set(key string, value any)
func (m *Record) SetRaw(key string, value any)
func (m *Record) SetIfFieldExists(key string, value any) Field
func (m *Record) SetExpand(expand map[string]any)

func (m *Record) Get(key string) any
func (m *Record) GetRaw(key string) any
func (m *Record) GetString(key string) string
func (m *Record) GetInt(key string) int
func (m *Record) GetInt64(key string) int64
func (m *Record) GetFloat(key string) float64
func (m *Record) GetBool(key string) bool
func (m *Record) GetStringSlice(key string) []string
func (m *Record) GetDateTime(key string) types.DateTime
func (m *Record) GetGeoPoint(key string) types.GeoPoint

func (m *Record) FieldsData() map[string]any
func (m *Record) CustomData() map[string]any
func (m *Record) PublicExport() map[string]any
func (m *Record) Expand() map[string]any
func (m *Record) Collection() *Collection
func (m *Record) IsNew() bool
func (m *Record) WithCustomData(state bool) *Record
func (m *Record) IgnoreEmailVisibility(state bool) *Record
func (m *Record) Fresh() *Record
func (m *Record) Clone() *Record
```

### The system field names

```go
FieldNameId              = "id"
FieldNameCollectionId    = "collectionId"
FieldNameCollectionName  = "collectionName"
FieldNameExpand          = "expand"
FieldNameEmail           = "email"
FieldNameEmailVisibility = "emailVisibility"
FieldNameVerified        = "verified"
FieldNameTokenKey        = "tokenKey"
FieldNamePassword        = "password"
```

⚠️ **There is no `FieldNameCreated` or `FieldNameUpdated`.** Those are
`AutodateField` instances, not exported constants.

## The field types

```go
type TextField struct { Name, Id string; System, Hidden, Presentable bool; Help string; Min, Max int; Pattern, AutogeneratePattern string; Required, PrimaryKey bool }
type BoolField struct { Name, Id string; System, Hidden bool; Help string; Required bool }
type NumberField struct { Name, Id string; System, Hidden bool; Help string; Min, Max float64; OnlyInt bool; Required bool }
type DateField struct { Name, Id string; System, Hidden bool; Help string; Required bool }
type SelectField struct { Name, Id string; System, Hidden bool; Help string; Values []string; MaxSelect int; Required bool }
type JSONField struct { Name, Id string; System, Hidden bool; Help string; MaxSize int; Required bool }
type EditorField struct { Name, Id string; System, Hidden bool; Help string; MaxSize int; Required bool }
type FileField struct { Name, Id string; System, Hidden bool; Help string; MaxSelect, MaxSize int; MimeTypes, Thumbs []string; Required, Presentable bool }
type RelationField struct { Name, Id string; System, Hidden bool; Help string; CollectionId string; CascadeDelete bool; MinSelect, MaxSelect, MaxSize int; Required bool }
type GeoPointField struct { Name, Id string; System, Hidden bool; Help string; Required bool }
type PasswordField struct { Name, Id string; System, Hidden bool; Help string; Min, Max int; Cost int; Required bool }
type EmailField struct { Name, Id string; System, Hidden bool; Help string; ExceptDomains, OnlyDomains []string; Required bool }
type AutodateField struct { Name, Id string; System, Hidden bool; Help string; OnCreate, OnUpdate bool }
```

### The `Field` interface

```go
type Field interface {
    GetId() string
    GetName() string
    GetType() string
    GetRequired() bool
    GetHidden() bool
    GetSystem() bool
    GetOptions() map[string]any
    SetId(id string)
    SetName(name string)
    SetRequired(required bool)
    SetHidden(hidden bool)
    SetSystem(system bool)
    SetOptions(options map[string]any)
}
```

### The `FieldsList`

```go
func (l *FieldsList) Add(fields ...Field)
func (l *FieldsList) AddAt(pos int, fields ...Field)
func (l *FieldsList) RemoveById(fieldId string)
func (l *FieldsList) RemoveByName(fieldName string)
func (l *FieldsList) GetById(fieldId string) (Field, error)
func (l *FieldsList) GetByName(fieldName string) (Field, error)
```

## The `dbx` query builder

```go
// Expressions.
func NewExp(e string, params ...Params) Expression
func HashExp(p Params) Expression
func In(col string, values ...interface{}) Expression
func NotIn(col string, values ...interface{}) Expression
func And(exps ...Expression) Expression
func Or(exps ...Expression) Expression
func Exists(exp Expression) Expression

// The query builder.
func Select(cols ...string) *SelectQuery
func (s *SelectQuery) From(tables ...string) *SelectQuery
func (s *SelectQuery) Where(e Expression) *SelectQuery
func (s *SelectQuery) AndWhere(e Expression) *SelectQuery
func (s *SelectQuery) OrWhere(e Expression) *SelectQuery
func (s *SelectQuery) Join(typ, table string, on Expression) *SelectQuery
func (s *SelectQuery) InnerJoin(table string, on Expression) *SelectQuery
func (s *SelectQuery) LeftJoin(table string, on Expression) *SelectQuery
func (s *SelectQuery) OrderBy(cols ...string) *SelectQuery
func (s *SelectQuery) AndOrderBy(cols ...string) *SelectQuery
func (s *SelectQuery) GroupBy(cols ...string) *SelectQuery
func (s *SelectQuery) Limit(limit int64) *SelectQuery
func (s *SelectQuery) Offset(offset int64) *SelectQuery
func (s *SelectQuery) All(dest interface{}) error
func (s *SelectQuery) One(dest interface{}) error
func (s *SelectQuery) Row(dest interface{}) error
```

⚠️ **Use `[[column]]` for the identifiers and `{:param}` for the
values.**

## The `search` package

```go
// The filter.
type FilterData string
func (f FilterData) BuildExpr(fieldResolver FieldResolver, placeholderReplacements ...dbx.Params) (dbx.Expression, error)
func (f FilterData) BuildExprWithLimit(fieldResolver FieldResolver, maxExpressions int, placeholderReplacements ...dbx.Params) (dbx.Expression, error)

// The sort.
type SortField struct {
    Name      string `json:"name"`
    Direction string `json:"direction"`
}
func ParseSortFromString(str string) (fields []SortField)

// The provider.
func NewProvider(fieldResolver FieldResolver) *Provider
func (s *Provider) Query(query *dbx.SelectQuery) *Provider
func (s *Provider) Page(page int) *Provider
func (s *Provider) PerPage(perPage int) *Provider
func (s *Provider) SkipTotal(skipTotal bool) *Provider
func (s *Provider) CountCol(name string) *Provider
func (s *Provider) Sort(sort []SortField) *Provider
func (s *Provider) AddSort(field SortField) *Provider
func (s *Provider) Filter(filter []FilterData) *Provider
func (s *Provider) AddFilter(filter FilterData) *Provider
func (s *Provider) MaxFilterExprLimit(max int) *Provider
func (s *Provider) MaxSortExprLimit(max int) *Provider
func (s *Provider) Parse(urlQuery string) error
func (s *Provider) Exec(items any) (*Result, error)
func (s *Provider) ParseAndExec(urlQuery string, modelsSlice any) (*Result, error)

type Result struct {
    Items      any `json:"items"`
    Page       int `json:"page"`
    PerPage    int `json:"perPage"`
    TotalItems int `json:"totalItems"`
    TotalPages int `json:"totalPages"`
}

// The resolvers.
type FieldResolver interface {
    UpdateQuery(query *dbx.SelectQuery) error
    Resolve(field string) (*ResolverResult, error)
}
func NewSimpleFieldResolver(allowedFields ...string) *SimpleFieldResolver

// The constants.
const (
    DefaultPerPage         = 30
    MaxPerPage             = 1000
    DefaultFilterExprLimit = 200
    DefaultSortExprLimit   = 8
    MaxFilterLength        = 3500
    MaxSortFieldLength     = 255
    SortAsc                = "ASC"
    SortDesc               = "DESC"
)

// The URL parameter names.
const (
    PageQueryParam      = "page"
    PerPageQueryParam   = "perPage"
    SortQueryParam      = "sort"
    FilterQueryParam    = "filter"
    SkipTotalQueryParam = "skipTotal"
)
```

## The `store` package

```go
func New[K comparable, T any](data map[K]T) *Store[K, T]

func (s *Store[K, T]) Reset(newData map[K]T)
func (s *Store[K, T]) RemoveAll()
func (s *Store[K, T]) Remove(key K)
func (s *Store[K, T]) Has(key K) bool
func (s *Store[K, T]) Get(key K) T
func (s *Store[K, T]) GetOk(key K) (T, bool)
func (s *Store[K, T]) GetAll() map[K]T
func (s *Store[K, T]) Keys() []K
func (s *Store[K, T]) Values() []T
func (s *Store[K, T]) Set(key K, value T)
func (s *Store[K, T]) SetFunc(key K, fn func(old T) T)
func (s *Store[K, T]) GetOrSet(key K, setFunc func() T) T
func (s *Store[K, T]) SetIfLessThanLimit(key K, value T, maxAllowedElements int) bool
```

## The `hook` package

```go
type Handler[T any] struct {
    Id   string
    Func func(e T) error
    ...
}

func (h *Hook[T]) Bind(handler *Handler[T]) string
func (h *Hook[T]) BindFunc(fn func(e T) error) string
func (h *Hook[T]) Unbind(idsToRemove ...string)
func (h *Hook[T]) UnbindAll()
func (h *Hook[T]) Length() int
func (h *Hook[T]) Trigger(event T, oneOffHandlerFuncs ...func(T) error) error
```

## The migrations

```go
type Migration struct {
    Up               func(txApp App) error
    Down             func(txApp App) error
    File             string
    ReapplyCondition func(txApp App, runner *MigrationsRunner, fileName string) (bool, error)
}

type MigrationsList struct { ... }
func (l *MigrationsList) Register(up func(txApp App) error, down func(txApp App) error, optFilename ...string)
func (l *MigrationsList) Add(m *Migration)
func (l *MigrationsList) Copy(list MigrationsList)
func (l *MigrationsList) Items() []*Migration
func (l *MigrationsList) Item(index int) *Migration

var SystemMigrations MigrationsList
var AppMigrations MigrationsList
```

## The REST API

| Method   | Path                                            |
| -------- | ----------------------------------------------- |
| `GET`    | `/api/health`                                   |
| `GET`    | `/api/collections`                              |
| `GET`    | `/api/collections/{collection}`                 |
| `GET`    | `/api/collections/{collection}/records`         |
| `GET`    | `/api/collections/{collection}/records/{id}`     |
| `POST`   | `/api/collections/{collection}/records`         |
| `PATCH`  | `/api/collections/{collection}/records/{id}`     |
| `DELETE` | `/api/collections/{collection}/records/{id}`     |

### The query parameters

```
page, perPage, filter, sort, expand, fields, skipTotal
```

### The authentication

```
Authorization: <token>
```
