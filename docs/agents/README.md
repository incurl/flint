# Agent documentation

Documentation written for AI coding agents working on this template.

**Start with [`../../AGENTS.md`](../../AGENTS.md)`** — it is the map:
the architecture, the invariants, the gotchas and the verification loop.

---

## The skills

Each file is a self-contained recipe: the goal, the files to touch, the
code, and the checks to run.

| Skill                                                     | Use it when                                              |
| --------------------------------------------------------- | -------------------------------------------------------- |
| [`start-a-project.md`](start-a-project.md)               | You are turning the template into your own app.           |
| [`add-a-page.md`](add-a-page.md)                         | You need a new screen.                                    |
| [`add-a-collection.md`](add-a-collection.md)             | You need a new PocketBase collection and its migration.   |
| [`add-a-record-renderer.md`](add-a-record-renderer.md)   | You want to change how records are displayed.             |
| [`add-a-command.md`](add-a-command.md)                   | You want a key action or a background operation.          |
| [`add-a-config-option.md`](add-a-config-option.md)       | You need a new setting.                                   |
| [`add-a-sync.md`](add-a-sync.md)                         | You want to pull remote data into the local cache.        |
| [`debug-a-tui.md`](debug-a-tui.md)                       | The UI is frozen, blank or wrong.                          |
| [`verify.md`](verify.md)                                 | Before you commit. Run this.                              |

## The reference

[`REFERENCE.md`](REFERENCE.md) — the exact API surface of every package:
the `Store` interface, the `Query` and `PageResult` types, the theme,
the components, the config, and the PocketBase APIs the template uses.

**Use it instead of grepping.** It is verified against the code.

---

## Why this exists

A template is only useful if a developer — or an agent — can change it
safely without reading all of it first. These documents are the answer:
the orientation, the recipes and the reference.

Every code example here was compiled against the real packages before
being written down.
