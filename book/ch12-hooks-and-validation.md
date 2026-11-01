# Chapter 12 — Hooks, Validation, and the Record Life Cycle

## 12.1 The two layers of validation

PocketBase validates a record in two places, and they are easy to
confuse.

### The schema validation

This is the *declarative* part: the rules you wrote in the field
definitions.

```go
&core.TextField{
    Name:     "email",
    Required: true,
    Max:      150,
    Pattern:  `^\S+@\S+\.\S+$`,
}
```

It is enforced by the field's own `ValidateValue` method, driven by
`app.Validate(model)`.

### The hook validation

This is the *imperative* part: the rules you wrote in Go.

```go
app.OnRecordValidate().BindFunc(func(e *core.RecordEvent) error {
    if e.Record.GetString("status") == "invalid" {
        return validation.NewError("validation_invalid_status", "invalid status")
    }
    return e.Next()
})
```

Both run during `app.Save(record)`.

⚠️ **`SaveNoValidate` skips both.** If you use it, you have opted out
of the schema rules *and* your own hooks.

## 12.2 The hook system

The hook package implements a **tagged middleware chain**.

```go
// Hook defines a generic hook around a specific event.
type Hook[T any] struct {
    handlers []*Handler[T]
    ...
}

// Handler is a function that handles a specific event.
type Handler[T any] struct {
    Id   string
    Func func(e T) error
    ...
}
```

### Binding a handler

```go
app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{
    Id: "audit-logger",
    Func: func(e *core.RecordEvent) error {
        log.Printf("created %s in %s", e.Record.Id, e.Record.Collection().Name)
        return e.Next()
    },
})
```

or the shorthand:

```go
app.OnRecordCreate().BindFunc(func(e *core.RecordEvent) error {
    log.Printf("created %s", e.Record.Id)
    return e.Next()
})
```

### The `e.Next()` chain

This is the crucial part. The chain works like a middleware stack:

```go
app.OnRecordValidate().BindFunc(func(e *core.RecordEvent) error {
    // BEFORE the rest of the chain.
    if bad {
        return err     // stop here, the chain is aborted
    }

    err := e.Next()    // run the next handler (and the validation)

    // AFTER the rest of the chain.
    return err
})
```

⚠️ **Forgetting `e.Next()` silently breaks everything downstream.**

This is the single most common hook bug. If you write:

```go
app.OnRecordValidate().BindFunc(func(e *core.RecordEvent) error {
    log.Println("validating")
    // oops, no e.Next()
    return nil
})
```

then:

- the framework's own validation never runs;
- every other validate hook never runs;
- **invalid records are saved**.

The record is still validated by the *schema* (which runs in
`app.Validate`, outside the hook chain), but your custom rules and
any other plugin's rules are skipped.

⚠️ **Always end a handler with `return e.Next()`**, unless you are
deliberately aborting.

### Unbinding

```go
app.OnRecordCreate().Unbind("audit-logger")
app.OnRecordCreate().UnbindAll()
```

The `Id` is how you remove a handler. Use stable, meaningful ids.

### Tags

Hooks accept tags, which lets you target a subset of the events:

```go
app.OnRecordCreate("important").BindFunc(...)
```

The tag filters which *model* the hook applies to. A tagged hook only
fires for the models whose id matches.

## 12.3 The record hooks

There are many, grouped by phase.

### The validation phase

```go
app.OnRecordValidate()
```

Fires before a create or an update, with the record in its
post-validation state. Return an error to abort.

### The create phase

```go
app.OnRecordCreate()                // before the insert
app.OnRecordCreateExecute()         // around the actual insert
app.OnRecordAfterCreateSuccess()    // after a successful insert
app.OnRecordAfterCreateError()      // after a failed insert
```

### The update phase

```go
app.OnRecordUpdate()
app.OnRecordUpdateExecute()
app.OnRecordAfterUpdateSuccess()
app.OnRecordAfterUpdateError()
```

### The delete phase

```go
app.OnRecordDelete()
app.OnRecordDeleteExecute()
app.OnRecordAfterDeleteSuccess()
app.OnRecordAfterDeleteError()
```

### The enrichment phase

```go
app.OnRecordEnrich()
```

Fires when a record is about to be serialised, so you can add computed
fields.

⚠️ **`OnRecordEnrich` is how you add a virtual field.** A field that
is not in the schema but that you want in the output:

```go
app.OnRecordEnrich().BindFunc(func(e *core.RecordEnrichEvent) error {
    e.Record.Set("displayName", computeDisplayName(e.Record))
    return e.Next()
})
```

⚠️ **But `PublicExport()` only returns the *schema* fields.** A
virtual field set with `Set` on an unknown key becomes *custom data*,
which is excluded from `PublicExport` unless `WithCustomData(true)`.

This is why the template merges `FieldsData()` and `CustomData()`
rather than relying on `PublicExport()` alone.

### The request phase

These fire in the REST API layer, not in `core`:

```go
app.OnRecordsListRequest()
app.OnRecordViewRequest()
app.OnRecordCreateRequest()
app.OnRecordUpdateRequest()
app.OnRecordDeleteRequest()
```

⚠️ **These do not fire in an embedded application**, because there is
no HTTP request. If you write a hook on `OnRecordsListRequest` and
test it in embedded mode, it will never run.

For embedded applications, use `OnRecordValidate` / `OnRecordCreate` /
`OnRecordUpdate`, which fire on `app.Save`.

## 12.4 A practical hook: normalising a field

```go
// Normalise the label: trim it and collapse the internal whitespace.
app.OnRecordValidate().BindFunc(func(e *core.RecordEvent) error {
    if e.Record.Collection().Name != "vocab" {
        return e.Next()
    }

    label := strings.Join(strings.Fields(e.Record.GetString("label")), " ")
    e.Record.Set("label", label)

    return e.Next()
})
```

⚠️ **Check the collection name.** A validate hook fires for *every*
collection. Guard it, or your rule leaks into unrelated collections.

You can also filter by the model id:

```go
app.OnRecordValidate(vocabCollectionId).BindFunc(...)
```

## 12.5 A practical hook: derived fields

```go
// Keep a lowercase search key in sync with the label.
app.OnRecordAfterCreateSuccess().BindFunc(func(e *core.RecordEvent) error {
    if e.Record.Collection().Name != "vocab" {
        return e.Next()
    }

    // Update the search key without re-triggering the create hooks.
    e.Record.Set("searchKey", strings.ToLower(e.Record.GetString("label")))

    return e.Next()
})
```

⚠️ **Saving a record from within a hook can recurse.** The
`AfterCreateSuccess` hook fires after the insert, so saving again there
would trigger `OnRecordUpdate`, and if that hook also saves, you have
an infinite loop.

The safe pattern is to modify the record in memory and save it with the
validation disabled, or to use `app.SaveNoValidate` — but carefully,
because that skips the hooks.

## 12.6 The record life cycle

Putting it together, the full sequence for `app.Save(record)` on a new
record:

```
app.Save(record)
  │
  ├─ 1. app.Validate(record)
  │      the schema rules: Required, Max, Pattern, select values, ...
  │      a failure returns an error and nothing is written
  │
  ├─ 2. OnRecordValidate
  │      your custom rules
  │      a failure returns an error and nothing is written
  │
  ├─ 3. OnRecordCreate
  │      "about to create"
  │      a failure aborts the whole operation
  │
  ├─ 4. OnRecordCreateExecute
  │      the actual INSERT
  │      (inside a transaction)
  │
  ├─ 5. OnRecordAfterCreateSuccess   (or OnRecordAfterCreateError)
  │
  └─ 6. the transaction commits
```

For an update, steps 3–5 become `OnRecordUpdate`,
`OnRecordUpdateExecute` and `OnRecordAfterUpdateSuccess` /
`OnRecordAfterUpdateError`.

⚠️ **Steps 1 and 2 run for both create and update.** So a validate
hook must handle both cases. A rule like "the label is required" works
for both; a rule like "the label cannot change" only makes sense on an
update.

You can distinguish them:

```go
app.OnRecordValidate().BindFunc(func(e *core.RecordEvent) error {
    if e.Record.IsNew() {
        // create
    } else {
        // update
    }
    return e.Next()
})
```

## 12.7 The event types

The hooks are generic over the event type. The record events:

```go
type RecordEvent struct {
    App   App
    Model Model
    Record *Record
    Tags  []string
}

type RecordErrorEvent struct {
    RecordEvent
    Error error
}

type RecordEnrichEvent struct {
    RecordEvent
    RequestInfo *RequestInfo
}

type RecordsListRequestEvent struct {
    RequestEvent
    Collection *Collection
    Records    []*Record
    Result     *search.Result
}
```

The `RecordEvent` embeds the `Model`, so `e.Model` gives you the
underlying record, and `e.Record` the typed one.

## 12.8 Hooks and the TUI

Now the interesting question: how do hooks interact with a TUI?

### The problem

A hook fires inside `app.Save()`, which the template calls from a
command goroutine. If the hook wants to update the UI, it needs a way
to send a message — but it has no reference to the model.

### The solutions

**Option 1: collect the events and deliver them later.**

```go
// A buffer that collects the hook events.
type eventBuffer struct {
    mu     sync.Mutex
    events []string
}

func (b *eventBuffer) record(msg string) {
    b.mu.Lock()
    defer b.mu.Unlock()
    b.events = append(b.events, msg)
}

func (b *eventBuffer) drain() []string {
    b.mu.Lock()
    defer b.mu.Unlock()
    out := b.events
    b.events = nil
    return out
}
```

```go
app.OnRecordAfterCreateSuccess().BindFunc(func(e *core.RecordEvent) error {
    buffer.record("created " + e.Record.Id)
    return e.Next()
})
```

Then a command drains the buffer and returns it as a message:

```go
func (m *Model) load() tea.Cmd {
    buffer := m.buffer
    return func() tea.Msg {
        records, err := m.store.List(ctx, query)
        // ...
        return loadedMsg{records: records, events: buffer.drain()}
    }
}
```

⚠️ **The buffer needs a mutex**, because the hook runs on the command
goroutine and the drain also happens there — but different commands may
run concurrently.

**Option 2: use `program.Send` from the hook.**

```go
app.OnRecordAfterCreateSuccess().BindFunc(func(e *core.RecordEvent) error {
    program.Send(recordCreatedMsg{id: e.Record.Id})
    return e.Next()
})
```

⚠️ **This is the escape hatch.** Chapter 3 recommends against using
`program.Send` for internal communication, but a hook is genuinely
external to the model — it has no other way to reach it.

If you use it, hold the `*tea.Program` in a struct that the hook can
reach, and make sure the program is not nil (it is nil until the TUI
starts, and the migrations run before that).

### The template's approach

The Flint template does **not** use hooks for UI events. The hooks it
uses are for data integrity, and the UI learns about the outcome
through the normal command → message → state path.

This is the cleaner design, and it is worth understanding why:

> **A hook that reports to the UI is a second, parallel communication
> channel. Prefer the command's return value, which already carries
> the result.**

Use hooks for rules that must hold *regardless* of who writes the
record. Use commands for the UI flow.

## 12.9 Validating before the store

The template validates the JSON in the editor before sending it to the
store:

```go
func (e *JSONEditor) Validate() (map[string]any, error) {
    raw := strings.TrimSpace(e.textarea.Value())
    if raw == "" {
        return nil, errors.New("the document is empty")
    }

    var doc map[string]any
    if err := json.Unmarshal([]byte(raw), &doc); err != nil {
        return nil, err
    }
    return doc, nil
}
```

```go
func (p *DetailPage) save() tea.Cmd {
    // ...
    doc, err := p.editor.Validate()
    if err != nil {
        return func() tea.Msg {
            return components.ToastMsg{Level: components.ToastError, Text: "invalid JSON: " + err.Error()}
        }
    }

    return func() tea.Msg {
        // ...
        if _, err := store.Update(ctx, collection, id, doc); err != nil {
            return components.ToastMsg{Level: components.ToastError, Text: err.Error()}
        }
        return components.ToastMsg{Level: components.ToastSuccess, Text: "record saved"}
    }
}
```

This is a good pattern: **validate locally for a fast, precise error;
validate on the server for the authority.**

The local check is a UX optimisation. The server check (the schema
validation and the hooks) is the real one.

## 12.10 Exercises

1. Write a validate hook that rejects a record whose "key" field
   contains uppercase characters. Test it with `app.Save`.

2. Write a validate hook that does **not** call `e.Next()`. Show that
   a record violating the schema `Required` rule is now saved. This
   proves the point about the chain.

3. Write a hook that fires for every collection and count how many
   times it is called when you save records in two collections. Then
   add the collection guard and re-measure.

4. Write an `OnRecordEnrich` hook that adds a "displayName" field.
   Read the record with `PublicExport()` and with
   `FieldsData()` + `CustomData()`. Explain the difference.

5. Implement the event-buffer pattern from §12.8 and verify that the
   events arrive in the UI.

6. Write a hook that saves the record from within
   `OnRecordAfterCreateSuccess`. Observe the recursion. Fix it.

7. Write a validate hook that distinguishes a create from an update
   using `e.Record.IsNew()`, and make it enforce "the key is
   immutable after creation".

## 12.11 What you learned

- Validation happens in **two layers**: the declarative schema rules
  and the imperative hooks.
- Hooks are a **tagged middleware chain**; `e.Next()` continues it.
- **Forgetting `e.Next()` silently disables** the framework's
  validation and every other hook.
- The record life cycle is: **validate → create hook → execute → after
  hook → commit**.
- `OnRecordValidate` fires for **both** creates and updates.
- `OnRecordEnrich` adds virtual fields, but `PublicExport()` only
  returns the schema fields.
- The **request hooks do not fire in embedded mode** — there is no
  HTTP request.
- Saving from within a hook can **recurse**; use the `After*` hooks
  carefully.
- A hook that reports to the UI needs a **second communication
  channel** (a buffer or `program.Send`); prefer the command's return
  value when you can.
- **Validate locally for a good error, on the server for the truth.**

## 12.12 Further reading

- `tools/hook/hook.go` — the chain implementation.
- `core/base.go` — the `OnRecord*` methods.
- `core/record_model.go` — the event types.
- `core/db.go` — the `save` function, which orchestrates the life
  cycle.
- `internal/components/jsoneditor.go` — the local validation.

Next: [Chapter 13 — Migrations](ch13-migrations.md).
