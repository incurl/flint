# Skill: add the Wikidata tools

**Goal:** let the model enrich a collection with the facts of Wikidata.

**Time:** ~30 minutes. The packages are already there: this skill is
about the wiring, not about the implementation.

---

## What the tools do

| Tool                | What it does                                     | Gated? |
| ------------------- | ------------------------------------------------ | ------ |
| `wikidata_enrich`   | Enriches a collection with the facts of Wikidata | **yes** |
| `wikidata_analyze`  | Reports what the enrichment achieved            | no     |
| `wikidata_generate` | Writes a Go project that reads the enriched data | **yes** |

The enrichment asks a SPARQL endpoint about the entities of a
collection, and writes what it learns into a `wikidata_<collection>`
collection: one record per fact, with the identifier of the entity, the
property, and the value.

The analysis joins the two collections and reports what the join holds.

---

## The pieces

| Piece                        | Where                          |
| ---------------------------- | ------------------------------ |
| The SPARQL client            | `internal/wikidata/client.go`  |
| The query builder            | `internal/wikidata/query.go`   |
| The property registry        | `internal/wikidata/properties.go` |
| The entity type inference    | `internal/wikidata/entity.go`  |
| The planner and the executor | `internal/wikidata/planner.go`, `internal/wikidata/executor.go` |
| The analysis                 | `internal/wikidata/analysis.go` |
| The tools                    | `internal/agent/wikidata.go`   |

---

## Turning them on

The tools reach a public endpoint, so they are **off by default**. An
application that never asks about Wikidata must not open a connection to
it.

```toml
# flint.toml
[wikidata]
enabled = true
endpoint = "https://query.wikidata.org/sparql"
delay = "100ms"
sample_size = 20
```

```bash
# Or with the environment.
FLINT_WIKIDATA=1

# Or with the flag.
--wikidata
```

The wiring is in `cmd/tui/agent.go`:

```go
if cfg.Wikidata.Enabled {
    client := wikidata.NewClient(
        cfg.Wikidata.Endpoint,
        wikidata.WithDelay(cfg.Wikidata.Delay),
        wikidata.WithMaxSolutions(cfg.Wikidata.MaxSolutions),
        wikidata.WithLogger(logger),
    )

    agent.RegisterWikidataTools(
        registry, store, client,
        agent.NewGate(permissionMode(cfg), broker),
        cfg.CodeRoot(), logger,
    )
}
```

The **root** is the code directory of the application: the generated
project is written inside it, and a path that escapes it is refused. It
is the same boundary as the one of the code tools.

---

## The classification

Every new tool has to be classified in `internal/agent/mode.go`:

```go
var readOnlyTools = []string{
    // ...
    ToolWikidataAnalyze,
}

var mutatingTools = []string{
    // ...
    ToolWikidataEnrich,
    ToolWikidataGenerate,
}
```

The enrichment writes to the data of the user, exactly like a record
does: the plan proposes it, the act mode carries it out. The generation
writes files, so it is gated like the code tools. The analysis only
reads, so the plan may use it.

A tool that is in neither list is **withheld** from the plan mode. That
is the safe default for a downstream project, and it is why the
classification is not optional.

---

## The properties

The properties to ask for are **data**, and the registry is the
extension point:

```go
wikidata.RegisterPropertySet(wikidata.PropertySet{
    EntityType:  "species",
    Description: "A species of animal.",
    Properties: []wikidata.Property{
        {IRI: "wdt:P141", Label: "conservation status", FullIRI: wikidata.PrefixWDT + "P141"},
    },
})
```

A new entity type is a registration. A new property is an entry in a
registration. The query builder does not change.

A registration that adds a property to a known entity type **merges**:
the properties of the template are kept.

The inference is a table as well — a field name to a property:

```go
wikidata.RegisterFieldProperty("first_published", "wdt:P577")
```

The *decision* to ask for a property is made from the record at hand, and
the table says which property a field name stands for.

The registry is guarded by a mutex: the tools of a step may run in
parallel, and a registration may happen while a query is being built.

---

## The rules that are not optional

1. **Every value that goes into a query text is escaped.** A literal is
   delimited by quotes, and a value that holds a quote ends the literal
   and changes the meaning of the query. Use `wikidata.QuoteLiteral`.

2. **The identifier of an entity is data.** It comes from a field of the
   record — `wikidata_id` or `qid` — and it is never hardcoded.

3. **The write is idempotent.** The store has no transaction API, so a
   batch is a loop. A fact is looked up by its natural key with
   `store.FindFirst` and updated, not written twice.

4. **A query is bounded twice.** A deadline bounds the time, and
   `sparql.WithMaxSolutions` bounds the memory.

5. **The remote backend cannot create the collection.** The enrichment
   needs the `wikidata_<collection>` collection, and only the embedded
   backend can create one. The error of the remote backend says so, and
   it is not swallowed.

   The **generation** works with both: it reads the schema through the
   store, and the store reads a remote schema over REST.

6. **A guess is not a fact.** A fuzzy match below the threshold is not
   counted, and the threshold is part of the report.

7. **The generated project stays inside the root.** The model chooses
   the directory, and a path that escapes the root is refused. The check
   is lexical, exactly like the one of the code tools.

8. **The generated project is not overwritten.** The files of a
   generated project belong to the user once they are written. The
   overwrite is explicit.

9. **A run is bounded.** It reads at most `wikidata.MaxSampleSize`
   records, and every record is a query.

10. **A dry run changes nothing.** It does not create the collection, it
    does not write the facts, and it does not leave a record in the log.

---

## The pipeline

The three tools are the pipeline: read the state → plan → execute →
analyse → generate. The model calls them, and the user approves the two
that write.

There is no command of its own: the tools are the one surface, and a
project that wants to run the pipeline without a model calls the
packages of `internal/wikidata` and `internal/generator` directly.

---

## What the user says

The tools are the vocabulary the user's sentences are expressed in. The
sentences are not parsed — they are *understood* — and the description
of a tool is what the model matches them against.

> **Enrich the `people` collection with the facts from Wikidata.**

The model calls `wikidata_enrich` with `{"collection": "people"}`. The
facts are written into `wikidata_people`, one record per fact, and the
run is idempotent: a fact that is already there is updated, not written
twice.

> **How many of the people did Wikidata know about?**

The model calls `wikidata_analyze` with `{"collection": "people"}`. The
answer is the report as JSON: the number of matched records, the number
of unmatched ones, the facts per property, and the number of matches
that were made by name rather than by identifier.

> **Check what Wikidata would tell us about the `writers` collection
> before you change anything.**

The model calls `wikidata_enrich` with `{"dry_run": true}`. The endpoint
is asked and nothing is written — not the collection, not the facts, not
the log.

> **Generate a small API that serves the people and their Wikidata
> facts.**

The model calls `wikidata_generate` with
`{"collection": "people", "kind": "api"}`. The project is written inside
the code directory of the application, and a path that escapes it is
refused.

> **Regenerate it — the `people` collection has a new field.**

The model calls `wikidata_generate` with `{"overwrite": true}`. Without
it, the call fails with *"the project already exists"*, and the error
names the remedy.

> **Enrich the `species` collection too — it has a
> `conservation_status` field.**

`species` is not a known entity type, and the tools cannot do it. This
is a moment where the **user** extends the template — and the way to do
it is a registration, not a change to the query builder:

```go
wikidata.RegisterPropertySet(wikidata.PropertySet{
    EntityType:  "species",
    Description: "A species of animal.",
    Properties: []wikidata.Property{
        {IRI: "wdt:P141", Label: "conservation status", FullIRI: wikidata.PrefixWDT + "P141"},
    },
})
```

> **Enrich the `places` collection with the facts from Wikidata.**

In the plan mode, the enrichment is **not in the tool list**. The model
cannot call it — not even by accident, and not even if it is asked to.
It reads the schema, writes a plan, and the plan waits for the user.

---

## The checks

```bash
gofmt -l ./cmd ./config ./internal
go vet ./...
go test ./...
go test -race ./...
golangci-lint run
```

The integration test needs no network: it starts a real SPARQL endpoint
over an in-memory graph, and it points the client at it.

```bash
go test ./internal/wikidata/ -run TestAgentEnriches
```

The generator test **builds** the generated project: a generated project
that does not compile is worse than no generated project.

```bash
go test ./internal/generator/ -run TestGenerateProduces
```

See [`REFERENCE.md`](REFERENCE.md) for the full API of the two packages.
