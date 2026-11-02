# Building Terminal Applications with Bubble Tea and PocketBase

**A companion book for the Flint template.**

This book teaches you to build production-grade terminal applications
with **Bubble Tea** (the TUI framework) and **PocketBase** (used as an
embedded Go library, not just as a server).

It is written for the developer who has just clicked *Use this template*
on the Flint repository and wants to understand not just *how* to use the
code, but *why* it is shaped the way it is — well enough to change it
confidently.

---

## Who this is for

| You are…                                    | Start at…              |
| ------------------------------------------- | ---------------------- |
| New to Go                                   | Chapter 1              |
| New to TUI programming                       | Part I                 |
| Comfortable with Bubble Tea                 | Part II                |
| Comfortable with PocketBase as a server      | Part II, Chapter 11    |
| A Go developer who has never embedded a DB    | Chapter 10             |
| **Ready to build your own application**      | **Part IV**            |
| **Looking for something to build**           | **Chapter 25**         |
| **Want to run hooks on a PocketBase server**  | **Chapter 26**         |

## How to read it

The book is **linear** but each chapter stands alone. If you already know
Bubble Tea, skim Part I and start at Part II. If you already know
PocketBase, read Chapters 1–7 and 16–20.

**Part IV is the "now what?" part.** It is for the moment when you have
read the rest and want to build *your* application:

- **Chapter 21** catalogues the extension points, with their costs.
- **Chapter 22** is the pattern library — the recurring problems and
  their solutions.
- **Chapter 23** covers the advanced patterns: background workers,
  modals, command palettes, undo, virtualised lists, imports.
- **Chapter 24** is about the *data*: the four domain shapes and how to
  model them.
- **Chapter 25** is the playground: twenty-odd ideas you could build on
  top of the template, from a diff viewer to a graph explorer.

Every chapter ends with:

- **What you learned** — a summary you can skip if you skimmed;
- **Exercises** — small tasks with a specific answer;
- **Further reading** — the exact source files to open next.

## Conventions

```go
// This is the "good" version — it is shown in full.
func Good() tea.Cmd {
    return tea.Quit
}
```

Comments marked ⚠️ describe a mistake that is easy to make and hard to
debug. They are the parts worth reading even if you skip the rest.

## The code

The book uses the Flint template throughout. Every snippet is either
taken verbatim from the template or is a small variation on it. The
template lives at `github.com/incurl/flint`.

| Path                    | What it holds                                  |
| ----------------------- | ---------------------------------------------- |
| `cmd/tui/`              | The entry point                                |
| `internal/app/`         | The root Bubble Tea model                      |
| `internal/pages/`       | The screens                                    |
| `internal/components/`  | The reusable widgets                           |
| `internal/pbstore/`     | The data layer                                 |
| `internal/theme/`       | The styles                                     |
| `internal/state/`       | The runtime state                              |
| `config/`               | The configuration loader                       |
| `examples/wikidata/`    | A complete domain adapter                      |

## Building the code

```bash
git clone https://github.com/incurl/flint
cd flint
make build      # -> ./bin/flint
make test       # the test suite
make run        # start the TUI
```

## Table of contents

### Part I — Bubble Tea

1. [The Terminal Is a Program Target](ch01-the-terminal.md)
2. [The Elm Architecture](ch02-elm-architecture.md)
3. [Messages, Commands, and the Event Loop](ch03-messages-and-commands.md)
4. [Keys, Hotkeys, and the Routing Problem](ch04-keys-and-hotkeys.md)
5. [Rendering: lipgloss, Layout, and the Frame](ch05-rendering.md)
6. [Bubbles: Composition over Reinvention](ch06-bubbles.md)
7. [Concurrency: The Purity of Update](ch07-concurrency.md)

### Part II — PocketBase as a Library

8. [PocketBase's Mental Model](ch08-pocketbase-mental-model.md)
9. [The App: Collections, Fields, Records](ch09-app-collections-records.md)
10. [Querying: The Native Filter Grammar](ch10-querying.md)
11. [App.Store(): What It Really Is](ch11-app-store.md)
12. [Hooks, Validation, and the Record Life Cycle](ch12-hooks-and-validation.md)
13. [Migrations](ch13-migrations.md)
14. [Embedded or Server: One Library, Two Deployments](ch14-embedded-vs-server.md)

### Part III — Putting Them Together

15. [Architecture for TUI + Database Applications](ch15-architecture.md)
16. [The pbstore Pattern: One Interface, Two Backends](ch16-pbstore-pattern.md)
17. [Sync, Caching, and the Offline-First Problem](ch17-sync-and-caching.md)
18. [Testing Terminal Applications](ch18-testing.md)
19. [Performance](ch19-performance.md)
20. [Shipping: Configuration, Shutdown, Packaging](ch20-shipping.md)

### Part IV — Extending the Template

21. [Extension Points: A Catalogue](ch21-extension-points.md)
22. [Patterns and Practices](ch22-patterns-and-practices.md)
23. [Advanced Patterns](ch23-advanced-patterns.md)
24. [Domain Patterns](ch24-domain-patterns.md)
25. [Creative Ideas and Geek Playgrounds](ch25-creative-ideas.md)

### Part V — The JavaScript Extension

26. [The JavaScript Extension](ch26-javascript-extensions.md)

### Appendices

- [A. Bubble Tea API Reference](appendix-a-bubbletea-api.md)
- [B. PocketBase API Reference](appendix-b-pocketbase-api.md)
- [C. The Complete Filter Grammar](appendix-c-filter-grammar.md)
- [D. Troubleshooting](appendix-d-troubleshooting.md)
- [E. Further Reading](appendix-e-further-reading.md)

---

## License

MIT.
