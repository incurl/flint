# Skill: add a command

**Goal:** add a key action or a background operation to the app.

**Time:** ~10 minutes.

---

## What a "command" means here

In Bubble Tea, a `tea.Cmd` is a function that runs **outside** the
render loop and returns a message:

```go
type Cmd func() Msg
```

Two rules follow from that:

1. **A command never returns an error.** It returns a message that
   carries an `Err` field. The error is handled in `Update`, where the
   UI can react to it.
2. **A command captures values, not the model.** The model mutates
   while the command runs. Capturing `p` and reading `p.runtime` later
   is a data race.

---

## Recipe A — a local action

An action that does not need the store: open a panel, toggle a mode,
copy to the clipboard.

```go
case tea.KeyPressMsg:
	if msg.String() == "c" {
		return p, func() tea.Msg {
			return CopyMsg{Text: p.SelectedRecord().StringField("title")}
		}
	}
```

The command closure reads `p.SelectedRecord()` **at call time**, which
is before the goroutine starts — that is safe. What is not safe is
reading `p` *inside* the closure after it has started.

The safe form captures first:

```go
case tea.KeyPressMsg:
	if msg.String() == "c" {
		rec := p.SelectedRecord()
		if rec == nil {
			return p, nil
		}

		text := rec.StringField("title")

		return p, func() tea.Msg {
			return CopyMsg{Text: text}
		}
	}
```

Then handle the message:

```go
case CopyMsg:
	return p, p.clipboard.Copy(msg.Text)
```

---

## Recipe B — a store operation

An action that reads or writes data.

**Step 1 — define the message.** Put it next to the page that produces
it, so the page does not have to import `app`:

```go
// DeleteMsg is emitted when the user deletes a record.
type DeleteMsg struct {
	// ID is the identifier of the deleted record.
	ID string
	// Err holds the error, when the deletion failed.
	Err error
}
```

**Step 2 — produce it from a command.** Capture the values first:

```go
case tea.KeyPressMsg:
	if msg.String() == "x" {
		rec := p.SelectedRecord()
		if rec == nil {
			return p, nil
		}

		id := rec.ID
		store := p.store
		collection := p.runtime.Collection

		return p, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := store.Delete(ctx, collection, id); err != nil {
				return DeleteMsg{ID: id, Err: err}
			}

			return DeleteMsg{ID: id}
		}
	}
```

**Step 3 — handle it in `Update`.**

```go
case DeleteMsg:
	if msg.Err != nil {
		return p, nil
	}

	// Reload the list so that the deleted row disappears.
	return p, p.Refresh()
```

---

## Recipe C — a global hotkey

A hotkey that works on every page belongs to the root model, in
`handleGlobalKey`:

```go
func (m *Model) handleGlobalKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	// While a text input has the focus the global layer only handles
	// the interrupt key and the page switch digits.
	if m.inputFocused() {
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return tea.Quit, true
		// … the existing cases
		}

		return nil, false
	}

	switch msg.String() {
	// … the existing cases
	case "g":
		return m.reloadActivePage(), true
	}
}
```

**Check `m.inputFocused()` first.** A global hotkey that fires while
the user is typing in the filter box inserts a character instead of
running the action.

A *modal* editor is stricter than a focused text input: it takes every
key but `ctrl+c`. That is what `m.modalInputFocused()` guards, and it is
why the detail page can be edited as raw JSON: `q` and the digits are
part of the document, not navigation.

Add the hint to the status bar so the user knows the key exists. The
hints live in `hints()` in `internal/app/app.go`:

```go
// in hints()
case state.PageRecords:
	return components.FormatHints([][2]string{
		{"/", "filter"}, {"↑↓", "move"}, {"enter", "open"}, {"←→", "page"},
		{"g", "reload"}, // ← add yours
	})
```

---

## Recipe D — a periodic background task

Something that runs on a timer: a health check, a clock, a progress
indicator.

```go
// healthTickCmd asks for the next health check.
func healthTickCmd() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg {
		return healthTickMsg{}
	})
}

type healthTickMsg struct{}
```

Handle it and schedule the next one:

```go
case healthTickMsg:
	return p, tea.Batch(p.checkHealth(), healthTickCmd())
```

**Always schedule the next tick**, or the task stops after one round.

The spinner already uses this pattern — see `spinner.Tick` in
`internal/pages/records.go`.

---

## Recipe E — a command that needs the root model

A page cannot import `app`. When a page needs something only the root
model can do, emit a message and let the root handle it:

```go
// pages/common.go
type NavigateMsg struct {
	Page state.Page
}
```

The root model already handles `NavigateMsg`. For something new, add a
case in `app.Model.Update`:

```go
case pages.SyncRequestedMsg:
	return m, m.triggerSync()
```

---

## Anti-patterns

| Anti-pattern                                     | Why it is wrong                                          |
| ------------------------------------------------ | -------------------------------------------------------- |
| Returning an error from a command                 | The error is discarded. Return a message with `Err`.      |
| Reading `p.something` inside the closure          | The model mutates concurrently. Capture it first.        |
| Calling the store in `Update`                     | Blocks the render loop.                                  |
| Using `fmt.Println` for debugging in production   | It corrupts the TUI. Use `slog` to the stderr.           |
| Spawning a goroutine that writes to the model     | Data race. Send a message instead.                       |
| A timer that is not re-scheduled                   | The task runs once and stops.                            |

---

## Verifying

A command that is created but never returned never runs. To check that
a command works, test the **message handler**, not the command:

```go
// TestDeleteMsg verifies that a successful delete refreshes the list.
func TestDeleteMsg(t *testing.T) {
	page := pages.NewRecordsPage(store, newRuntime(), theme.Default())

	updated, cmd := page.Update(pages.DeleteMsg{ID: "rec1"})
	page = updated.(*pages.RecordsPage)

	if cmd == nil {
		t.Error("Update() returned no command after a delete")
	}
}
```

To exercise the command itself, call it and inspect the message:

```go
msg := page.Load()() // runs the command synchronously
```

That works because the command is a plain function.

---

## Verify the whole change

```bash
gofmt -w ./internal
go build ./... && go test ./...
golangci-lint run
```
