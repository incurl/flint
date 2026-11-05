# Skill: add a code tool

**Goal:** give the language model a new view of the source code of the
application.

**Time:** ~30 minutes.

---

## What the code tools are

The agent reads and writes PocketBase records. The code tools are the
other half: they let it read and change the **source code** of the
application, through a language server.

| Tool | What it does | Mode |
| --- | --- | --- |
| `code_symbols` | Lists the declarations of a file | read |
| `code_definition` | Finds where a symbol is defined | read |
| `code_references` | Finds every use of a symbol | read |
| `code_rename` | Renames a symbol, everywhere | **gated** |

They live in `internal/code` — the language server — and in
`internal/agent/code.go` — the tools. The split is the same as the rest
of the template: the package knows about files and symbols, and knows
nothing about models and permissions.

---

## The three rules

### 1. The root is a boundary

The agent reads **one directory**. A path outside of it is refused,
both when it is read and when the edits are written.

```go
manager, err := code.NewManager(cfg.CodeRoot(), logger)
```

`CodeRoot()` defaults to a `flint_code` directory next to the data
directory. The check is lexical: a symbolic link inside the root that
points outside of it is not followed, and the server is started with
the root as its workspace root, so it cannot read it either.

**Do not widen the root.** The agent writing a file outside the
directory the user pointed it at is the one failure the user cannot
undo.

### 2. A rename is a write

The renaming tool is behind the permission gate, exactly like
`update_record`:

```go
registry.Register(NewGuardedTool(NewCodeRenameTool(manager), gate, codeRenameApproval))
```

And it is in `mutatingTools` in `internal/agent/mode.go`, so the plan
mode withholds it. A rename changes the code of the user: the plan
proposes it, the act mode carries it out, and the user approves it.

### 3. The edits are byte ranges

The language server answers a rename with **character ranges**, not
with lines. The rest of the line must survive:

```go
// "func Greet(g Greeter) string" → "func Salute(g Greeter) string"
```

A line-based implementation replaces the whole line and destroys the
code around the symbol. The conversion from the UTF-16 character
offsets of the protocol to the byte offsets of a Go string lives in
`internal/code/convert.go`, and `PositionToByteOffset` of the protocol
package does the hard part.

---

## The column is corrected

A model reads a file and knows the **line** of a symbol. The column is
often a guess, and a wrong column makes the server answer
`column is beyond end of line`.

`Manager.position` therefore corrects it: when the column does not
point at an identifier, the first identifier of the line is taken.

The error when the line holds no symbol at all says so — `the line 13
holds no symbol` — because a model that gets a clear message corrects
itself.

---

## The server

`gopls` is looked up on the `PATH` with `exec.LookPath`. When it is
missing, the error says so:

```
code: the language server is not installed: gopls is not on the PATH
```

The lookup is not repeated for thirty seconds: the PATH does not change
while the application runs.

The server is started **lazily**, on the first question. An
application whose agent never asks about the code never pays for it.

---

## Wiring it up

In `cmd/tui/agent.go`:

```go
if cfg.Code.Enabled {
	manager, err := code.NewManager(cfg.CodeRoot(), logger)
	if err != nil {
		return nil, err
	}

	agent.RegisterCodeTools(registry, manager, agent.NewGate(permissionMode(cfg), broker))
}
```

The tools are **on by default**: the language server is started lazily,
so an application whose agent never asks about the code never pays for
it.

Turn them off with `--no-code`, `FLINT_CODE=0`, or `code.enabled =
false` in the configuration file. `--code` turns them back on when the
environment or the file has turned them off.

---

## Testing

```bash
go test ./internal/code/ ./internal/agent/
```

The live tests need `gopls` on the `PATH` and are **skipped** without
it: the tests of the template must pass on a machine that does not have
the server installed.

The interesting ones:

- `TestLiveRename` — the rename changes every use, and the file still
  compiles. It is the test that caught the line-based bug.
- `TestResolveRefusesTheOutside` — the boundary holds against the
  paths a model produces, including `../`.
- `TestCodeRenameIsGated` — the gate is consulted before the tool
  runs.

---

## The gotchas

- **The tool names are a contract.** `code_symbols` and the others
  are what the model calls; they do not change once they ship.
- **The description is the most important part of the tool.** A vague
  description produces a model that calls the wrong tool.
- **The server is a child process.** It is killed when the manager is
  closed, not asked to leave: a server that ignores the request would
  outlive the application.
- **The files are written atomically.** A temporary file and a rename,
  so that a crash in the middle does not leave a truncated source file
  behind.
