# Chapter 28 — Enriching with Wikidata: The Agent as a Researcher

Chapter 22 explained the agentic layer. Chapter 23 catalogued the
extension points. This chapter explains one that is **built in** and
that changes what the agent is *for*: the agent that goes and finds
things.

The template ships three tools that talk to Wikidata:

| Tool | What it does | Gated |
| - | - | - |
| `wikidata_enrich` | Enriches a collection with the facts of Wikidata | **yes** |
| `wikidata_analyze` | Reports what the enrichment achieved | no |
| `wikidata_generate` | Writes a Go project that reads the enriched data | **yes** |

Together they are a pipeline:

```
read the state → plan → query Wikidata → persist → analyse → generate
```

And every step of it is reachable through one surface: the agent. There
is no command of its own, and there does not need to be — the agent
*is* the interface, and the user is watching.

---

## 28.1 The idea

Here is a collection the user already has:

```
people
├── name        text
├── birth_date  text
└── country     text
```

It is fine, and it is thin. It holds what the user typed. What the user
*wants* is more: when was this person born, where were they born, what
is their country of citizenship, what do they do?

That information exists. Wikidata holds it, and it holds it for about a
hundred million entities, in a form that is queryable and free.

The enrichment asks Wikidata about the entities of the collection and
writes what it learns into a second collection:

```
wikidata_people
├── wikidata_id       text    Q42
├── label             text    Douglas Adams
├── property          text    wdt:P569
├── value             text    1956-05-12
└── source_record_id  text    the id of the record it came from
```

One record per fact. The `wikidata_id` is the join key: it is the
identifier of the *entity*, and it is what lets the analysis join the
two collections back together.

This is a knowledge graph, in the shape chapter 26 described, with a
source that is the whole of Wikidata.

---

## 28.2 Talking to a SPARQL endpoint

Wikidata is queried with SPARQL. The client is in
`internal/wikidata/client.go`:

```go
client := wikidata.NewClient(
    "https://query.wikidata.org/sparql",
    wikidata.WithDelay(100*time.Millisecond),
    wikidata.WithMaxSolutions(100_000),
    wikidata.WithLogger(logger),
)

rows, err := client.QueryRows(ctx, query)
```

### A query is bounded twice

```go
func (c *Client) QueryRows(ctx context.Context, query string) (*sparql.Result, error) {
    c.wait(ctx)

    ctx = sparql.WithMaxSolutions(ctx, c.maxSolutions)
    ctx, cancel := context.WithTimeout(ctx, c.timeout)
    defer cancel()

    return sparql.QueryContext(ctx, c.graph, query)
}
```

**A deadline bounds the time. A budget of solutions bounds the memory.**

Only the first one is obvious, and only the first one is enough for a
trusted query. A query that builds a million solutions in ten seconds is
still a million solutions in memory, and a public endpoint that is asked
a question like that is a public endpoint that is under attack. The
budget is what stops it, and a query that spends the budget **fails**:

```go
var (
    // ErrTooManySolutions is returned when a query builds more solutions
    // than the budget allows.
    //
    // The result is not a partial answer: the query failed.
    ErrTooManySolutions = errors.New("wikidata: the solution budget was spent")
)
```

A partial answer is the worst of the two: the caller cannot tell the
difference between "there are three facts" and "there are three facts
and then the query ran out of room".

### The delay is not politeness

The public endpoint of Wikidata asks its clients to wait between two
queries. The client holds a mutex across the wait, so two tools that
run in parallel are serialised:

```go
func (c *Client) wait(ctx context.Context) {
    c.mu.Lock()
    defer c.mu.Unlock()

    if !c.lastQuery.IsZero() {
        elapsed := time.Since(c.lastQuery)
        if elapsed < c.delay {
            timer := time.NewTimer(c.delay - elapsed)
            defer timer.Stop()

            select {
            case <-ctx.Done():
            case <-timer.C:
            }
        }
    }

    c.lastQuery = time.Now()
}
```

The delay is measured from the *start* of the previous query, not from
its end: two queries that each take a second and are sent back to back
are still two queries in the same second.

### The signature that cannot hold the answer

The task this started from asked for:

```go
func (c *Client) Query(ctx context.Context, query string) (*graph.Graph, error)
```

and that signature cannot return a SELECT. A `*graph.Graph` holds
*triples*; a SELECT produces *bindings* — a set of variables and a table
of rows. The enrichment needs the variables and the rows: the variable
is the property, the row is the fact.

So the package has both:

```go
// Query executes a SPARQL query and returns the result graph.
//
// It is the CONSTRUCT-shaped answer: a graph holds triples, and a SELECT
// is answered by QueryRows.
func (c *Client) Query(ctx context.Context, query string) (*graph.Graph, error)

// QueryRows executes a SELECT and returns its bindings and its variable
// names.
func (c *Client) QueryRows(ctx context.Context, query string) (*sparql.Result, error)
```

`Query` is kept because it is the right shape for a CONSTRUCT, and
because a caller that asks for a graph deserves one. But the
enrichment uses `QueryRows`, and the difference is the whole difference
between "the facts" and "the facts, labelled".

---

## 28.3 The query builder

A query is built from a record and an entity type:

```go
query, err := wikidata.BuildQueryFromPocketBase(record, "person")
```

### The type selects the properties

The entity type is `person`, `place` or `organisation`, and it decides
*what* to ask for:

```
person        wdt:P31  instance of
              wdt:P569 date of birth
              wdt:P19  place of birth
              wdt:P27  country of citizenship
              wdt:P106 occupation

place         wdt:P17  country
              wdt:P131 located in the administrative territorial entity
              wdt:P31  instance of
              wdt:P1082 population

organisation  wdt:P17  country
              wdt:P571 inception
              wdt:P31  instance of
              wdt:P159 headquarters location
```

### The record selects the entity

And the fields of the record decide *who* to ask about:

- a record that holds a `wikidata_id` (or a `qid`) is asked about
  directly — the identifier is data, and it is never hardcoded;
- a record that only holds a name is searched for by that name.

The identifier path is the good one. The name path is a guess, and a
guess needs care.

### Every value that goes into the query is escaped

A SPARQL literal is delimited by double quotes. A value that holds a
double quote ends the literal and changes the meaning of the query:

```go
// QuoteLiteral renders a value as a SPARQL string literal.
//
// A SPARQL literal is delimited by double quotes, and a value that holds a
// double quote would end the literal and change the meaning of the query.
// The backslash is escaped first, so that the escape of the quote is not
// escaped in turn.
func QuoteLiteral(value string) string {
    replacer := strings.NewReplacer(
        `\`, `\\`,
        `"`, `\"`,
        "\n", `\n`,
        "\r", `\r`,
        "\t", `\t`,
    )

    return `"` + replacer.Replace(value) + `"`
}
```

This is the same class of bug as the one fixed in
`internal/pbstore/queries.go`, and it has the same shape of fix. The
test parses the generated query with `sparql.Parse` and fails when the
parser rejects it — which is what a parser that did not reject it would
have cost you.

### The language tag is outside the quotes

There is a second, quieter version of the same bug. Wikidata stores the
label of an entity in every language, and a query that looks for the
label has to ask for two forms:

```go
queryByLabel = `  {
    SELECT ?entity WHERE {
      { ?entity rdfs:label %[1]s }
      UNION
      { ?entity rdfs:label %[2]s }
      ?entity wikibase:sitelinks ?sitelinks .
    }
    ORDER BY DESC(?sitelinks)
    LIMIT 1
  }
  VALUES ?property { %[3]s }
  ?entity ?property ?value .
  OPTIONAL { ?value rdfs:label ?valueLabel }
}`
```

with `%[1]s` the plain literal and `%[2]s` the one with the tag:

```
"Douglas Adams"          ← the untagged label
"Douglas Adams"@en       ← the English one
```

**The tag is outside the quotes.** `"Douglas Adams@en"` is a label that
ends with the letters `@en`, and it matches nothing. The two forms are
built by two different functions for exactly this reason:

```go
QuoteLiteral(label)               // "Douglas Adams"
QuoteLanguageLiteral(label, "en") // "Douglas Adams"@en
```

### The subquery picks the right entity

Two entities can share a name. The subquery sorts the candidates by
their **sitelinks** — the number of Wikipedia pages that link to them —
and takes the first. The popular one is the one the record is about.

The order of the clauses matters too. The `?entity` is resolved **in a
subquery**, and the properties are fetched for that one entity:

```sparql
{ SELECT ?entity WHERE { ... ORDER BY DESC(?sitelinks) LIMIT 1 } }
VALUES ?property { wdt:P569 wdt:P19 }
?entity ?property ?value .
```

A single flat query that joins the label match with the property fetch
asks for the properties of **every** entity with that name. The
subquery asks for the properties of the one that was chosen.

### The `?entity` is projected

The projection is `SELECT ?entity ?property ?value ?valueLabel`, and
the `?entity` is not decoration:

```go
// The entity is projected as well, because it is the join key: a
// record that is searched for by its name has no identifier of its
// own, and the one the endpoint resolved is the identifier of the
// fact.
```

A record that is searched for by its name comes back with **no** QID of
its own. The identifier of the fact is the one the endpoint resolved. If
it is not projected, the fact has no join key and is dropped — silently,
which is the worst way to drop it.

---

## 28.4 The property registry

The properties are **data**:

```go
var propertySets = map[string]PropertySet{
    "person": {
        EntityType:  "person",
        Description: "A human being.",
        Properties: []Property{
            {IRI: "wdt:P569", Label: "date of birth", FullIRI: PrefixWDT + "P569"},
            // ...
        },
    },
}
```

But a map that nothing can reach is not a registry, it is a constant
with a map type. The package exports the way in:

```go
// RegisterPropertySet registers a property set under an entity type.
//
// It is the extension point of the package: a downstream project
// registers the properties of an entity type of its own, and the query
// builder picks them up.
//
// A set that is already registered is replaced, and the properties of the
// two are merged: a registration that adds a property to a known entity
// type does not drop the others.
func RegisterPropertySet(set PropertySet) error

// RegisterFieldProperty registers the property that a field name
// suggests.
//
// It is the second half of the extension point: a collection that holds a
// field the template does not know about can say what that field is
// worth.
func RegisterFieldProperty(field, property string) error
```

A downstream project that has a `species` collection says so:

```go
wikidata.RegisterPropertySet(wikidata.PropertySet{
    EntityType:  "species",
    Description: "A species of animal.",
    Properties: []wikidata.Property{
        {IRI: "wdt:P141", Label: "conservation status", FullIRI: wikidata.PrefixWDT + "P141"},
        {IRI: "wdt:P171", Label: "parent taxon", FullIRI: wikidata.PrefixWDT + "P171"},
    },
})
```

And a collection that holds a field the template does not know about:

```go
wikidata.RegisterFieldProperty("first_published", "wdt:P577")
```

### The registry is guarded

The tools of a step may run in parallel, and a registration may happen
while a query is being built. The registry is behind a `sync.RWMutex`,
and every reader takes the lock:

```go
registryMu.RLock()
defer registryMu.RUnlock()
```

A map that is written while it is read is a crash that happens once a
week, on a Friday, for a user who did nothing wrong.

---

## 28.5 The inference

The entity type is inferred from the name of the collection and from the
fields it holds:

```go
func InferEntityType(collection string, fields []string) string
```

The name is the first hint — a collection called `people` holds people.
The fields are the second, and they are the stronger one:

```go
for _, field := range fields {
    switch normaliseFieldName(field) {
    case "birthdate", "dateofbirth", "birthyear":
        return "person"
    case "population":
        return "place"
    case "inception", "founded", "foundingdate":
        return "organisation"
    }
}
```

A collection called `records` that holds a `birth_date` holds people
whatever it is called.

**The fields matter, and it is easy to forget them.** The tool that the
model calls infers the type from the schema of the collection:

```go
schema, err := tool.store.CollectionSchema(ctx, params.Collection)
if err != nil {
    return "", fmt.Errorf("agent: read the schema of %q: %w", params.Collection, err)
}

fields := make([]string, 0, len(schema))
for _, field := range schema {
    fields = append(fields, field.Name)
}

entityType, err := wikidata.InferEntityTypeFor(params.Collection, fields, params.EntityType)
```

Leave the `fields` out and a collection called `writers` — which holds
people — is refused with *"unknown entity type"*, while the executor
below it would have worked perfectly well. The description of the tool
promises that the type is inferred "from the collection and its fields",
and the fields have to actually be passed.

### The inference of the properties

The same way: the fields of the record decide what else is worth asking
for.

```go
var fieldProperties = map[string]string{
    "birth_date": "wdt:P569",
    "country":    "wdt:P17",
    "inception":  "wdt:P571",
}
```

`InferProperties` returns the union of the properties of the entity type
and the properties the fields stand for. A field name that is not in the
table contributes nothing, and it does not break the inference.

This is the "agentic" part of the enrichment, and it is worth being
precise about what that means. It does **not** mean the model decides
what to ask for. It means the *decision* — should this collection be
asked about the country? — is made from the data at hand, rather than
from a table that is fixed at compile time.

---

## 28.6 The planner and the executor

The enrichment is a planner and an executor:

```go
type Planner interface {
    Plan(ctx context.Context, state State) (Action, error)
}

type Executor interface {
    Execute(ctx context.Context, action Action) (Outcome, error)
}
```

### The planner decides

The planner reads the state of the store and decides what to do:

| Situation | Action |
| - | - |
| No `wikidata_<target>` collection exists | `ActionCreate` |
| The collection exists but is empty | `ActionEnrich` |
| It is populated and the last run is too old | `ActionRefresh` |
| It is populated and was just enriched | `ActionStop` |

The planner **decides**, it does not follow a script. The table above is
the rule, and the state is what it reads:

```go
func decideAction(state State) Action {
    switch {
    case !state.EnrichedExists:
        return ActionCreate
    case state.EnrichedRecords == 0:
        return ActionEnrich
    case state.RefreshInterval > 0 && !state.LastRun.IsZero() &&
        time.Since(state.LastRun) > state.RefreshInterval:
        return ActionRefresh
    default:
        return ActionStop
    }
}
```

`decideAction` is a pure function, so the decision can be tested
without a store at all.

### The executor writes idempotently

The store has no transaction API. A batch is a loop, and a loop that
fails half way leaves what it wrote. The write is therefore idempotent:
the fact is looked up by its natural key — the entity, the property and
the record it was read from — and updated rather than written twice.

```go
filter := "wikidata_id = " + pbstore.QuoteFilterValue(item.qid) +
    " && property = " + pbstore.QuoteFilterValue(item.property) +
    " && source_record_id = " + pbstore.QuoteFilterValue(item.sourceRecordID)

existing, err := e.store.FindFirst(ctx, EnrichedCollectionName(e.target), filter)
if err != nil {
    if errors.Is(err, pbstore.ErrNotFound) {
        return nil, nil
    }

    return nil, fmt.Errorf("wikidata: look the fact up: %w", err)
}
```

A second run completes the work without duplicating it. **The facts**
must not be written twice. The agent log is the opposite: it is
append-only, because a log of the runs is a list of the runs.

The filter is in the native PocketBase grammar — a comparison is a
field, a sign and a value — and the values are quoted, so that a value
holding a quote does not end the literal.

### A bad record does not stop the run

```go
rows, err := e.client.QueryRows(ctx, query)
if err != nil {
    failed++

    e.client.LogQuery(ctx, query, 0, err)

    continue
}
```

One record that cannot be enriched — a name the endpoint does not know,
a timeout, a malformed value — must not take the whole enrichment with
it. The count of the failures is in the outcome, and it is reported.

### The dry run changes nothing

A dry run does not create the collection, does not write the facts, and
does not leave a record in the log:

```go
if e.dryRun {
    facts, queries, queried, failed, err := e.collect(ctx)
    // ...
    return outcome, nil
}
```

This is worth stating carefully, because the first version of the dry
run did create the collection — the check was in `persist` and in
`writeLog`, and the collection was created before both of them. A dry
run that changes the shape of the store is not a dry run.

---

## 28.7 The analysis

The analysis joins the collection with its `wikidata_*` collection and
reports what the join holds.

```go
type AnalysisReport struct {
    OriginalCollection  string         `json:"original_collection"`
    EnrichedCollection  string         `json:"enriched_collection"`
    OriginalRecords     int            `json:"original_records"`
    EnrichedRecords     int            `json:"enriched_records"`
    MatchedRecords      int            `json:"matched_records"`
    UnmatchedRecords    int            `json:"unmatched_records"`
    ByProperty          map[string]int `json:"by_property"`
    FuzzyMatches        int            `json:"fuzzy_matches"`
    ConfidenceThreshold float64        `json:"confidence_threshold"`
}
```

### The exact join first, the fuzzy one second

A record that holds a Wikidata identifier is joined on it. A record that
does not is joined by name, with `github.com/sahilm/fuzzy`.

**The score of the fuzzy library is not a confidence.** It is an
unbounded number that grows with the length of the name and with the
length of the run of the matching characters. A perfect match of
`"Douglas Adams"` scores 1,328,630; a partial one scores 442,892. There
is no threshold on that number that means "close enough".

The confidence is the score divided by the score of the name against
itself:

```go
func bestMatch(name string, candidates []string) (string, float64) {
    self := fuzzy.Find(name, []string{name})
    if len(self) == 0 || self[0].Score <= 0 {
        return "", 0
    }

    found := fuzzy.Find(name, candidates)
    if len(found) == 0 {
        return "", 0
    }

    best := found[0]

    return best.Str, float64(best.Score) / float64(self[0].Score)
}
```

A name that matches itself exactly has a confidence of one. A candidate
that holds every character of the name scores close to it.

**A guess is not a fact.** A match below the threshold is not counted,
and the threshold is in the report, so that a reader of the report knows
what was counted and what was not.

### The candidates have to be reachable

There is a subtlety here that is easy to get wrong, and that makes the
whole fuzzy path dead if you do.

The obvious implementation indexes the labels of the facts by their
normalised form, and looks the name up in that map:

```go
// WRONG: the fuzzy matcher is never reached.
candidates := i.labels[normaliseFieldName(name)]
```

The lookup is *exact*. If it finds something, the fuzzy matcher has
nothing to do — the names are already equal. If it finds nothing, the
fuzzy matcher is never called. The fuzzy path is unreachable, the
confidence is never computed, and `fuzzy_matches` is always zero.

The index holds **every** label, and the candidates are the ones that
are worth comparing:

```go
// candidates returns the labels that begin with the same letter as the
// name.
//
// The first letter is the cheap filter: a label that does not begin with
// it cannot hold the whole name, and the confidence of the match would be
// below the threshold anyway.
func (i *factIndex) candidates(name string) []labelFact {
    normalised := normaliseFieldName(name)
    if normalised == "" {
        return nil
    }

    first := normalised[0]

    out := make([]labelFact, 0, 8)

    for _, candidate := range i.labels {
        if normaliseFieldName(candidate.label)[0] != first {
            continue
        }

        out = append(out, candidate)
    }

    return out
}
```

### A collection that was never enriched is not an error

The model may well ask "what did you find?" before it asks "go and
find something". The collection of the facts does not exist yet, and
`pbstore.List` reports a missing collection with a raw SQL error:

```
wikidata: read the collection "wikidata_people": pbstore: resolve collection "wikidata_people": sql: no rows in result set
```

That is not an answer the model can do anything with. The analysis
treats it as what it is — a collection that was never enriched — and
reports zero enriched records:

```go
if isMissingCollection(err) {
    logReport(ctx, opts.Logger, report)

    return report, nil
}
```

The two backends report a missing collection differently: the embedded
one fails to resolve it, and the remote one gets an empty list from the
server. `isMissingCollection` knows both.

> **A better place to fix this is `pbstore`.** The honest fix is for
> `Embedded.List` and `Remote.List` to return `ErrNotFound` for a
> missing collection, and for the analysis to use `errors.Is`. The
> message check is a workaround for a contract that is not quite right
> yet, and it is fragile: a future version of PocketBase could change
> the wording.

### The report is JSON

```go
logger.InfoContext(ctx, "the analysis ran", "report", string(encoded))
```

The report is logged as JSON so that a downstream consumer can parse
the line. A report that only a human can read is a report that a
pipeline cannot use.

---

## 28.8 The generator

The last step writes a Go project that reads the enriched schema.

```go
err := generator.Generate(ctx, store, generator.Options{
    OriginalCollection: "people",
    EnrichedCollection: "wikidata_people",
    Kind:               generator.KindCLI,
    OutputDir:          "demo",
    Root:               cfg.CodeRoot(),
    ModulePath:         "example.com/demo",
})
```

### The schema comes from the store

The generator reads the schema through `pbstore.Store`:

```go
func Generate(ctx context.Context, store pbstore.Store, opts Options) error
```

not through the PocketBase app. The store is the only thing that knows
whether the data is local or remote, and a generator that reads the app
can only generate from the local data. With the store, the generation
works against **a remote server** as well — which the first version of
this could not do.

### The root is the boundary

The output directory is relative to a root, and a path that escapes it
is refused:

```go
func (o Options) resolveOutput() (string, error) {
    root := o.Root
    if root == "" {
        return "", fmt.Errorf("%w: the root is required", ErrOutsideRoot)
    }

    candidate := o.OutputDir
    if !filepath.IsAbs(candidate) {
        candidate = filepath.Join(root, candidate)
    }

    abs, err := filepath.Abs(candidate)
    if err != nil {
        return "", fmt.Errorf("generator: the output directory %q: %w", o.OutputDir, err)
    }

    if !withinRoot(root, abs) {
        return "", fmt.Errorf("%w: %s", ErrOutsideRoot, o.OutputDir)
    }

    return abs, nil
}
```

The check is **lexical**, exactly like the one of the code tools: a
symbolic link inside the root that points outside of it is not followed.
The model chooses the directory, and the root is where the model does
not get to go.

### The project is not overwritten

The files of a generated project belong to the user once they are
written. A run that replaced them would take the edits with it:

```go
if !opts.Overwrite {
    if _, err := os.Stat(path); err == nil {
        return fmt.Errorf("%w: %q", ErrProjectExists, path)
    }
}
```

The overwrite is explicit, and the error names the remedy:

```
generator: the project already exists: "…/flint_code/generated/go.mod"; ask for the overwrite to replace it
```

### The generated project has no requirement

The generated project reads the records over the REST API of PocketBase,
with the standard library alone. Its `go.mod` therefore holds **no
requirement** at all:

```
module example.com/demo

go 1.27.0
```

A requirement the project does not use is one that `go mod tidy`
removes, and a `go.mod` that a tidy changes was not right. The test
that checks this is worth having, because the build test does not catch
it: an unused requirement is not a build error.

### The generated code compiles

The test builds every kind of the project it writes:

```go
cmd := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(t.TempDir(), "generated"), "./...")
cmd.Dir = dir

cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
```

A generated project that does not compile is worse than no generated
project, and the cost of finding that out is the person who runs it.

---

## 28.9 The tools

### The tool names are a contract

```go
const (
    // ToolWikidataEnrich enriches a collection with the facts of
    // Wikidata.
    ToolWikidataEnrich = "wikidata_enrich"
    // ToolWikidataAnalyze reports what the enrichment achieved.
    ToolWikidataAnalyze = "wikidata_analyze"
    // ToolWikidataGenerate writes a Go project that reads the enriched
    // schema.
    ToolWikidataGenerate = "wikidata_generate"
)
```

The name is what the model calls. It is lowercase and snake_case, and it
does not change once it ships: a model that learned `wikidata_enrich`
cannot unlearn it.

> The name keeps the American spelling, and the repository's linter is
> configured with the UK locale. The name is a contract with a model,
> not a word of a sentence, and the rule is the same one the repository
> already applies to `Color` and to `Initialize`.

### The description is the tool

The description is the only thing that tells the model when to call the
tool. A vague description produces a model that calls the wrong tool.

```go
wikidataEnrichDescription = "Enrich a PocketBase collection with the facts of Wikidata. " +
    "Use it when the user asks to add Wikidata data to a collection: the dates, the places, " +
    "the countries and the other properties of the entities the collection holds. " +
    "The facts are written into a new collection named wikidata_<collection>, one record per fact, " +
    "and the run is idempotent: a fact that is already there is updated, not duplicated. " +
    "Give the collection to enrich. The entity type is inferred from the collection and its fields " +
    "unless it is given; the known types are person, place and organisation. " +
    "A record that holds a wikidata_id or a qid field is asked about directly, and a record that " +
    "only holds a name is searched for by that name."
```

Every sentence in it is there because the model needs it: what the tool
does, when to use it, where the results go, and what the arguments mean.

### The classification

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
does. The generation writes files, exactly like the code tools do. The
analysis only reads, so the planning mode may use it: a plan for an
enrichment needs to know what the enrichment achieved.

### The tools are off by default

```toml
[wikidata]
enabled = true
endpoint = "https://query.wikidata.org/sparql"
delay_ms = 100
sample_size = 20
max_solutions = 100000
```

They reach a public endpoint, and an application that never asks about
Wikidata must not open a connection to it.

---

## 28.10 Talking to the agent

Now the part that makes it a feature rather than a library. Here is
what the user can say.

### The first one: what do you know about these people?

> **Enrich the `people` collection with the facts from Wikidata.**

The model reads the schema, infers that the collection holds people, and
runs the enrichment. The permission gate asks before the facts are
written:

```
Enrich a collection with the facts of Wikidata.
The facts are written into a new collection named wikidata_<collection>.
```

The user presses `y`. The run continues, and the answer is a sentence:

```
enriched "people" with 14 facts from 3 records (0 could not be enriched);
the facts are in the collection "wikidata_people"
```

### The second one: what did you find?

> **How many of the people did Wikidata know about?**

The analysis answers with the report, as JSON:

```json
{
  "original_collection": "people",
  "enriched_collection": "wikidata_people",
  "original_records": 3,
  "enriched_records": 14,
  "matched_records": 3,
  "unmatched_records": 0,
  "by_property": {
    "wdt:P569": 3,
    "wdt:P19": 3,
    "wdt:P27": 3,
    "wdt:P31": 3
  },
  "fuzzy_matches": 1,
  "confidence_threshold": 0.8
}
```

The `fuzzy_matches` is the interesting one: it says that one of the
three was matched by name and not by identifier, and the threshold says
how confident that match had to be.

### The third one: what would you find?

> **Check what Wikidata would tell us about the `writers` collection
> before you change anything.**

The dry run asks the endpoint and writes nothing:

```
the run is a dry one, nothing was written
```

### The fourth one: build me the thing

> **Generate a small API that serves the people and their Wikidata
> facts.**

The generation writes the project inside the code directory:

```
the api project is written in "…/flint_code/generated";
it reads "people" and "wikidata_people"
```

And when the model runs it a second time, because the user wants the
schema of the collection to change:

> **Regenerate it — the `people` collection has a new field.**

```
generator: the project already exists: "…/flint_code/generated/go.mod";
ask for the overwrite to replace it
```

The model asks for the overwrite, the user approves it, and the
project is replaced. The user is told what is about to happen.

### The fifth one: something the template does not know

> **Enrich the `species` collection too — it has a `conservation_status`
> field.**

The model cannot do this with the tools as they are, because `species`
is not a known entity type. It is a moment where the **user** extends
the template — and the way to do it is a registration, not a change to
the query builder:

```go
wikidata.RegisterPropertySet(wikidata.PropertySet{
    EntityType:  "species",
    Description: "A species of animal.",
    Properties: []wikidata.Property{
        {IRI: "wdt:P141", Label: "conservation status", FullIRI: wikidata.PrefixWDT + "P141"},
    },
})
```

### The sixth one: the plan first

> **Enrich the `places` collection with the facts from Wikidata.**

In the plan mode, the enrichment is **not in the tool list**. The model
cannot call it — not even by accident, and not even if it is asked to.
It reads the schema, writes a plan, and the plan waits for the user:

```
FLINT_PLAN_READY
Records affected
  - collection "places", filter ""
  - collection "wikidata_places", 12 new records
FLINT_PLAN_READY
```

The user approves the plan, the mode switches to act, and the
enrichment runs.

---

## 28.11 What this teaches

The enrichment is three things at once, and it is worth naming them.

**It is an extension of the data layer.** The facts go into
collections, and they go there through `pbstore.Store`. The remote
backend cannot *create* the collection of the facts — the error says so
— but once the collection exists, the enrichment works against a server
exactly as it works against a local database.

**It is an extension of the agent.** The tools are ordinary tools: a
name, a description, a schema, and a `Run`. The gate, the plan mode and
the conversations are the ones the template already had.

**It is an extension of the generator.** The project it writes reads
the enriched schema, and the generator is the one that already knew how
to read a schema and write a project.

What it is not is a new surface. There is no command, no second API, no
second set of concepts. The user talks to the agent, the agent uses the
tools, and the tools use the packages.

That is the shape of a feature that belongs to a template: **it extends
what is already there, and it adds no surface of its own.**

---

## 28.12 Exercises

1. **Add an entity type.** Register a `species` property set, and enrich
   a collection that holds species. Which fields of the collection would
   the inference need to see?

2. **Break the escaping on purpose.** Remove the escape of the double
   quote from `QuoteLiteral`, and run the tests. What is the first
   failure, and what does it say about the parser?

3. **Make the fuzzy path unreachable.** Change the lookup in
   `factIndex.match` to an exact map access, and watch the
   `fuzzy_matches` counter go to zero. What else went wrong that the
   counter was hiding?

4. **Break the dry run.** Move the check in `Execute` back into
   `persist`, and see what a dry run does to the shape of the store.

5. **Write the report as a table.** The report is JSON so that a
   pipeline can read it. What would a human want instead, and where
   would the two live?

6. **Extend the generator.** The `dashboard` kind serves a table. Add a
   kind that serves the facts joined to the records. What does the join
   look like in the generated code?

7. **Make the analysis work against a remote server.** The
   `isMissingCollection` check is a workaround. Make `pbstore.List`
   return `ErrNotFound` for a missing collection, and replace the check
   with `errors.Is`. What else breaks?

---

## 28.13 What you learned

- The **property registry** is data, and a registry that nothing can
  reach is a constant with a map type. `RegisterPropertySet` is the
  extension point.
- The **inference** is the part that is agentic: the decision to ask
  for a property is made from the record at hand, and it needs the
  *fields* of the collection to do it.
- A **query is bounded twice**: by a deadline and by a budget of
  solutions. The second one is the one that is easy to forget.
- Every **value that goes into a query text is escaped**, and the
  language tag of a literal is *outside* the quotes.
- A **write that runs twice does not write twice**. The natural key of
  a fact is the entity, the property and the record it came from.
- A **fuzzy score is not a confidence**. The score is divided by the
  score of the name against itself.
- A **collection that was never enriched is a fact**, not an error.
- A **generated project is not overwritten**, and it has **no
  requirement** it does not use.
- The **tools are the surface**. There is no command, and the pipeline
  is reachable through the agent that was already there.

---

## 28.14 Further reading

- [`docs/agents/add-wikidata-tools.md`](../docs/agents/add-wikidata-tools.md)
  — the recipe, and the rules that are not optional.
- [`docs/agents/REFERENCE.md`](../docs/agents/REFERENCE.md) — the exact
  API of the three packages.
- [Chapter 22](ch22-agentic-layer.md) — the agentic layer the tools
  live in.
- [Chapter 23](ch23-extension-points.md) — the extension points, and
  where this one sits.
- [Chapter 26](ch26-domain-patterns.md) — the knowledge graph shape, and
  why one record per fact is the right one.
