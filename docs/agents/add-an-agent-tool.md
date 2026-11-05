# Skill: add an agent tool

**Goal:** give the language model a new capability.

**Time:** ~15 minutes.

---

## What an agent tool is here

A tool is a capability the model invokes. The template ships eight of
them, all backed by the PocketBase data layer:

| Tool | What it does |
| --- | --- |
| `list_records` | Lists the records of a collection |
| `get_record` | Fetches a single record by id |
| `search_records` | Searches the records for a text |
| `count_records` | Counts the records of a collection |
| `list_collections` | Lists the available collections |
| `create_record` | Creates a record — **gated** |
| `update_record` | Updates a record — **gated** |
| `delete_record` | Deletes a record — **gated** |

Twelve read and change the **source code** of the application,
through a language server. They are described in
[`add-code-tools.md`](add-code-tools.md):

| Tool | What it does |
| --- | --- |
| `code_symbols` | Lists the declarations of a file |
| `code_definition` | Finds where a symbol is defined |
| `code_references` | Finds every use of a symbol |
| `code_diagnostics` | Reports the errors, the warnings and the hints |
| `code_call_hierarchy` | Reports who calls a symbol, or what it calls |
| `code_search` | Searches the symbols of the whole workspace by name |
| `code_find_references` | Finds every use of a symbol, in every file |
| `code_rename` | Renames a symbol, everywhere — **gated** |
| `code_replace_symbol` | Replaces, inserts around, or deletes a declaration — **gated** |
| `code_create_file` | Creates an empty file — **gated** |
| `code_move_file` | Moves a file — **gated** |
| `code_delete_file` | Removes a file — **gated** |

The model loop is not ours: it belongs to `charm.land/fantasy`. What
this template adds is the tools, the events the UI consumes and the
permission gate in front of the writes.

---

## The three things a tool must have

### 1. A name the model calls it by

The name is part of the contract with the model. It is lowercase and
snake_case, and it does not change once it ships: a model that learned
`list_records` cannot unlearn it.

```go
const (
	// ToolListRecords lists the records of a collection.
	ToolListRecords = "list_records"
)
```

### 2. A description the model reads

The description is the **most important part of the tool**. It is the
only thing that tells the model when to call it. A vague description
produces a model that calls the wrong tool.

Write it for the model, not for the user of the template: say what the
tool does, when to use it, and what the arguments mean.

```go
const listRecordsDescription = "List the records of a PocketBase collection. " +
	"Use it to see what is stored before answering a question about the data. " +
	"The filter and the sort use the PocketBase grammar: " +
	"`status = \"active\"` for a filter, `-created,name` for a sort."
```

### 3. Arguments the model can fill in

The arguments are a struct whose tags carry the schema:

```go
type ListRecordsParams struct {
	Collection string `json:"collection" description:"The name of the collection to list the records of."`
	Filter     string `json:"filter,omitempty" description:"A PocketBase filter expression, for example: status = \"active\"."`
	PerPage    int    `json:"per_page,omitempty" description:"How many records to fetch per page."`
}
```

- `json` gives the argument its name in the call.
- `omitempty` makes it optional. Without it, the argument is required.
- `description` explains it. **Put the explanation in the tag**, not
  in a comment: the model never sees the comment.

The schema is generated from the struct, so the description of an
argument lives in exactly one place.

---

## Recipe — a tool that reads the store

Most tools are a `storeTool` with a parameter struct and a run
function.

**Step 1 — the parameter struct.** Put it in `internal/agent/tools.go`,
next to the others.

```go
// ArchiveRecordsParams are the arguments of the archive_records tool.
type ArchiveRecordsParams struct {
	// Collection is the name of the collection.
	Collection string `json:"collection" description:"The name of the collection."`
	// ID is the identifier of the record.
	ID string `json:"id" description:"The id of the record to archive."`
}
```

**Step 2 — the run function.** It receives the decoded arguments and
returns the text the model reads.

```go
func runArchiveRecord(ctx context.Context, store pbstore.Store, params *ArchiveRecordsParams) (string, error) {
	if params.Collection == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	if params.ID == "" {
		return "", fmt.Errorf("agent: the record id is required")
	}

	record, err := store.Update(ctx, params.Collection, params.ID, map[string]any{
		"archived": true,
	})
	if err != nil {
		return "", err
	}

	return "archived: " + describeRecord(record), nil
}
```

**Step 3 — the constructor.**

```go
// NewArchiveRecordTool returns the tool that archives a record.
func NewArchiveRecordTool(store pbstore.Store) Tool {
	return &storeTool[ArchiveRecordsParams]{
		store:       store,
		name:        ToolArchiveRecord,
		description: archiveRecordDescription,
		run:         runArchiveRecord,
	}
}
```

**Step 4 — register it.** Add it to `RegisterStoreTools`.

```go
registry.Register(NewArchiveRecordTool(store))
```

**Step 5 — write the test.** The tool tests run against a real embedded
PocketBase, so they assert what the model will actually see.

```go
func TestArchiveRecordTool(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	tool := agent.NewArchiveRecordTool(store)

	out, err := tool.Run(context.Background(), `{"collection":"vocab","id":"abc"}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out, "archived") {
		t.Errorf("Run() = %q, want it to report the archive", out)
	}
}
```

---

## Recipe — a tool that changes the data

A mutating tool goes behind the **permission gate**. The gate is not a
warning: a refused call never reaches the tool, so no write happens.

```go
registry.Register(NewGuardedTool(
	NewArchiveRecordTool(store),
	gate,
	"Archive a record. It can be unarchived.",
))
```

The third argument is what the **user** reads when they are asked to
approve, so it says what the call would do, in the user's terms.

---

## The rules a tool must not break

1. **A tool failure is a result, not a crash.** Return an error; the
   adapter turns it into an error response, and the model gets a chance
   to correct itself. A panic takes the whole run down.
2. **A tool validates before it writes.** A model that invents a field
   name gets an error it can correct, not a record with a phantom field.
3. **A tool is safe for concurrent use.** The model library may run the
   tools of one step in parallel.
4. **A tool never touches the UI.** It returns a string. The page
   decides what to draw.
5. **A tool is bounded.** The listing tools clamp the page size: a
   model that asks for a million records must not be able to exhaust
   the memory of the process.

---

## Adding a tool that is not about the store

The `Tool` interface is small enough for anything:

```go
type Tool interface {
	Name() string
	Description() string
	Run(ctx context.Context, input string) (string, error)
}
```

Implement it, register it, and the model can call it. Implement
`Schemer` as well if the tool takes arguments, so the model receives
the schema of them.

---

## The permission gate

The three mutating tools go behind the gate. It is not a warning: a
refused call never reaches the tool, so no write happens.

```go
registry.Register(NewGuardedTool(
	NewArchiveRecordTool(store),
	gate,
	"Archive a record. It can be unarchived.",
))
```

The gate asks the user through a **decision broker**, which is the
meeting point between the run and the UI:

- the run blocks on `broker.RequestPermission(...)`;
- the broker publishes the request to the UI;
- the UI shows it and the user presses `y` or `n`;
- the page calls `broker.Resolve(id, decision)`, which unblocks the run.

The broker owns the id of the request, so the answer always reaches the
run that asked for it. A request that is never answered ends when the
run is cancelled: the wait watches the context.

A gate with no requester **refuses** rather than approves:

```go
if requester == nil {
	return fmt.Errorf("%w: %s", ErrNoPermissionRequester, request.ToolName)
}
```

An agent that writes to the data of the user must not do it by
default.

---

## The conversations

The turns of a conversation live in PocketBase, in the collections of
the `internal/session` package. The runner reads the conversation
before a turn and writes it back after, so the model sees the previous
turns.

A tool that wants to be part of the conversation does not have to do
anything: the runner records the prompt and the answer. A tool call is
recorded by the page, through `history.AppendTool(...)`.

---

## Checks

```bash
gofmt -l ./internal
go vet ./...
go test ./internal/agent/
golangci-lint run
```

See `docs/agents/REFERENCE.md` for the full API of the agent package.
