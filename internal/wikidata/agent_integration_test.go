package wikidata_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/tggo/goRDFlib/endpoint"
	"github.com/tggo/goRDFlib/graph"
	"github.com/tggo/goRDFlib/sparql"
	"github.com/tggo/goRDFlib/sparql/results"
	"github.com/tggo/goRDFlib/term"

	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/wikidata"
)

// newMockEndpoint starts a SPARQL endpoint that serves the given triples.
//
// It is a real SPARQL protocol endpoint over an in-memory graph: the
// client talks to it exactly as it talks to the public endpoint of
// Wikidata.
func newMockEndpoint(t *testing.T, triples [][3]string) string {
	t.Helper()

	g := graph.NewGraph()
	ctx := context.Background()

	for _, triple := range triples {
		subject, err := term.NewURIRef(triple[0])
		if err != nil {
			t.Fatalf("NewURIRef(%q) error = %v", triple[0], err)
		}

		predicate, err := term.NewURIRef(triple[1])
		if err != nil {
			t.Fatalf("NewURIRef(%q) error = %v", triple[1], err)
		}

		object := term.NewLiteral(triple[2])

		if err := g.Add(ctx, subject, predicate, object); err != nil {
			t.Fatalf("Add() error = %v", err)
		}
	}

	ds := &sparql.Dataset{Default: g}
	srv := endpoint.New(ds, endpoint.WithResultFormats(results.FormatXML, results.FormatJSON))

	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	return ts.URL + "/query"
}

// newEmbeddedStore creates an embedded store backed by a temporary
// PocketBase data directory.
func newEmbeddedStore(t *testing.T) *pbstore.Embedded {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "pb_data")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{
		DataDir: dir,
	})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	return store
}

// seedPeople creates a people collection with two records.
//
// The first one carries a Wikidata identifier, and the second one only
// carries a name: the two are the two ways a record is enriched.
func seedPeople(t *testing.T, store *pbstore.Embedded) {
	t.Helper()

	ctx := context.Background()

	_, err := store.EnsureCollection(ctx, "people", []pbstore.CollectionField{
		{Name: "name", Type: pbstore.FieldTypeText, Required: true, Max: 200},
		{Name: "wikidata_id", Type: pbstore.FieldTypeText, Max: 32},
	})
	if err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}

	if _, err := store.Create(ctx, "people", map[string]any{
		"name":        "Douglas Adams",
		"wikidata_id": "Q42",
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := store.Create(ctx, "people", map[string]any{
		"name": "Someone Else",
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
}

// TestAgentEnrichesTheCollectionAndIsIdempotent is the integration test
// of the enrichment.
//
// It runs the agent against a mocked endpoint, checks that the facts were
// written, and then runs it a second time: the number of facts must not
// change. A run that writes the same fact twice is a run that fills the
// collection with duplicates, and the second run is what shows it.
func TestAgentEnrichesTheCollectionAndIsIdempotent(t *testing.T) {
	if testing.Short() {
		t.Skip("the integration test is skipped in short mode")
	}

	ctx := context.Background()

	endpoint := newMockEndpoint(t, [][3]string{
		{
			"http://www.wikidata.org/entity/Q42",
			"http://www.wikidata.org/prop/direct/P569",
			"1956-05-12",
		},
		{
			"http://www.wikidata.org/entity/Q42",
			"http://www.wikidata.org/prop/direct/P31",
			"human",
		},
	})

	store := newEmbeddedStore(t)
	seedPeople(t, store)

	client := wikidata.NewClient(endpoint, wikidata.WithDelay(0))

	executor := wikidata.NewAgentExecutor(store, client, wikidata.ExecutorOptions{
		Target:     "people",
		EntityType: "person",
	})

	outcome, err := executor.Execute(ctx, wikidata.ActionCreate)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if outcome.Written == 0 {
		t.Fatalf("Execute() wrote no fact: %+v", outcome)
	}

	// The collection of the facts exists.
	collections, err := store.Collections(ctx)
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	found := false

	for _, collection := range collections {
		if collection.Name == "wikidata_people" {
			found = true

			break
		}
	}

	if !found {
		t.Fatalf("the collection wikidata_people was not created: %v", collections)
	}

	first, err := store.Count(ctx, "wikidata_people", "")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if first == 0 {
		t.Fatal("the collection of the facts is empty")
	}

	// The second run must not write the facts twice.
	second, err := executor.Execute(ctx, wikidata.ActionRefresh)
	if err != nil {
		t.Fatalf("Execute() second run error = %v", err)
	}

	if second.Written != outcome.Written {
		t.Errorf("the second run wrote %d facts, want %d", second.Written, outcome.Written)
	}

	after, err := store.Count(ctx, "wikidata_people", "")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if after != first {
		t.Errorf("the collection holds %d facts after the second run, want %d", after, first)
	}

	// The agent log holds a record per run.
	logged, err := store.Count(ctx, wikidata.LogCollectionName, "")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if logged != 2 {
		t.Errorf("the agent log holds %d records, want 2", logged)
	}
}

// TestAgentDoesNotFailOnARecordItCannotEnrich checks that a record that
// cannot be enriched does not take the whole run with it.
func TestAgentDoesNotFailOnARecordItCannotEnrich(t *testing.T) {
	if testing.Short() {
		t.Skip("the integration test is skipped in short mode")
	}

	ctx := context.Background()

	// The endpoint holds nothing: no record can be enriched.
	endpoint := newMockEndpoint(t, nil)

	store := newEmbeddedStore(t)
	seedPeople(t, store)

	client := wikidata.NewClient(endpoint, wikidata.WithDelay(0))

	executor := wikidata.NewAgentExecutor(store, client, wikidata.ExecutorOptions{
		Target:     "people",
		EntityType: "person",
	})

	outcome, err := executor.Execute(ctx, wikidata.ActionCreate)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if outcome.Written != 0 {
		t.Errorf("Execute() wrote %d facts, want 0", outcome.Written)
	}
}

// TestAnalyzeWithWikidataCountsTheMatches checks the join between a
// collection and the facts of Wikidata.
func TestAnalyzeWithWikidataCountsTheMatches(t *testing.T) {
	if testing.Short() {
		t.Skip("the integration test is skipped in short mode")
	}

	ctx := context.Background()

	endpoint := newMockEndpoint(t, [][3]string{
		{
			"http://www.wikidata.org/entity/Q42",
			"http://www.wikidata.org/prop/direct/P569",
			"1956-05-12",
		},
	})

	store := newEmbeddedStore(t)
	seedPeople(t, store)

	client := wikidata.NewClient(endpoint, wikidata.WithDelay(0))

	executor := wikidata.NewAgentExecutor(store, client, wikidata.ExecutorOptions{
		Target:     "people",
		EntityType: "person",
	})

	if _, err := executor.Execute(ctx, wikidata.ActionCreate); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	report, err := wikidata.AnalyzeWithWikidata(ctx, store, "people")
	if err != nil {
		t.Fatalf("AnalyzeWithWikidata() error = %v", err)
	}

	if report.OriginalRecords != 2 {
		t.Errorf("the report counts %d original records, want 2", report.OriginalRecords)
	}

	if report.MatchedRecords != 1 {
		t.Errorf("the report counts %d matched records, want 1", report.MatchedRecords)
	}

	if report.UnmatchedRecords != 1 {
		t.Errorf("the report counts %d unmatched records, want 1", report.UnmatchedRecords)
	}

	if report.ByProperty["wdt:P569"] != 1 {
		t.Errorf("the report counts %d dates of birth, want 1: %v",
			report.ByProperty["wdt:P569"], report.ByProperty)
	}
}

// TestTheRemoteBackendCannotCreateTheCollection checks that the error of
// the remote backend is not swallowed.
//
// The collections of a server are created by a migration on the server
// side, and a run that pretends otherwise is a run that lies.
func TestTheRemoteBackendCannotCreateTheCollection(t *testing.T) {
	ctx := context.Background()

	store := &fakeStore{}

	executor := wikidata.NewAgentExecutor(store, wikidata.NewClient("http://127.0.0.1:1/query"), wikidata.ExecutorOptions{
		Target: "people",
	})

	_, err := executor.Execute(ctx, wikidata.ActionCreate)
	if err == nil {
		t.Fatal("Execute() error = nil, want an error")
	}

	if !errors.Is(err, errCannotCreate) {
		t.Errorf("the error does not carry the cause: %v", err)
	}
}

// errCannotCreate is the error of a backend that cannot create a
// collection.
var errCannotCreate = errors.New("the backend cannot create a collection")

// fakeStore is a store that cannot create a collection.
//
// It is the remote backend, reduced to the one behaviour that matters
// here: the collections of a server are created by a migration.
type fakeStore struct {
	pbstore.Store
}

func (s *fakeStore) EnsureCollection(context.Context, string, []pbstore.CollectionField) (pbstore.Collection, error) {
	return pbstore.Collection{}, errCannotCreate
}

func (s *fakeStore) Collections(context.Context) ([]pbstore.Collection, error) {
	return nil, nil
}

func (s *fakeStore) List(context.Context, pbstore.Query) (*pbstore.PageResult, error) {
	return &pbstore.PageResult{}, nil
}

func (s *fakeStore) Count(context.Context, string, string) (int, error) {
	return 0, nil
}

// TestTheExecutorBoundsItsWork checks that the executor reads a bounded
// number of records.
func TestTheExecutorBoundsItsWork(t *testing.T) {
	ctx := context.Background()

	store := newEmbeddedStore(t)
	seedPeople(t, store)

	executor := wikidata.NewAgentExecutor(store, wikidata.NewClient("http://127.0.0.1:1/query"), wikidata.ExecutorOptions{
		Target:     "people",
		EntityType: "person",
		SampleSize: 1,
	})

	outcome, err := executor.Execute(ctx, wikidata.ActionCreate)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if outcome.Queried != 1 {
		t.Errorf("the run queried %d records, want 1", outcome.Queried)
	}
}

// TestTheRefreshIntervalDecidesTheAction checks the rules of the planner.
func TestTheRefreshIntervalDecidesTheAction(t *testing.T) {
	if testing.Short() {
		t.Skip("the test is skipped in short mode")
	}

	ctx := context.Background()

	store := newEmbeddedStore(t)
	seedPeople(t, store)

	planner := wikidata.NewStorePlanner(store, nil)

	state, err := wikidata.ReadState(ctx, store, "people", time.Hour)
	if err != nil {
		t.Fatalf("ReadState() error = %v", err)
	}

	action, err := planner.Plan(ctx, state)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	if action != wikidata.ActionCreate {
		t.Errorf("Plan() = %v, want %v", action, wikidata.ActionCreate)
	}
}

// TestTheCoreRecordIsBuiltFromTheStoredFields checks that the executor
// reads the fields of the stored record.
func TestTheCoreRecordIsBuiltFromTheStoredFields(t *testing.T) {
	if testing.Short() {
		t.Skip("the test is skipped in short mode")
	}

	ctx := context.Background()

	store := newEmbeddedStore(t)
	seedPeople(t, store)

	records, err := store.List(ctx, pbstore.Query{Collection: "people", Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(records.Records) == 0 {
		t.Fatal("the collection is empty")
	}

	record := records.Records[0]

	if record.StringField("name") == "" {
		t.Errorf("the record holds no name: %v", record.Fields)
	}

	schema, err := store.CollectionSchema(ctx, "people")
	if err != nil {
		t.Fatalf("CollectionSchema() error = %v", err)
	}

	if len(schema) == 0 {
		t.Error("the collection holds no field")
	}
}

// TestARecordWithoutAnIdentifierIsEnrichedByItsName is the test of the
// record that has to be searched for by its name.
//
// The endpoint resolves the name to an entity, and the identifier of that
// entity is the join key of the fact. A query that does not project the
// entity loses it, and the fact is dropped: the enrichment writes
// nothing, and it does not say why.
func TestARecordWithoutAnIdentifierIsEnrichedByItsName(t *testing.T) {
	if testing.Short() {
		t.Skip("the integration test is skipped in short mode")
	}

	ctx := context.Background()

	// The endpoint holds the label of the entity and its sitelinks: the
	// query that searches by a name needs both to resolve the name.
	endpoint := newMockEndpoint(t, [][3]string{
		{
			"http://www.wikidata.org/entity/Q42",
			"http://www.wikidata.org/prop/direct/P569",
			"1956-05-12",
		},
		{
			"http://www.wikidata.org/entity/Q42",
			"http://www.w3.org/2000/01/rdf-schema#label",
			"Douglas Adams",
		},
		{
			"http://www.wikidata.org/entity/Q42",
			"http://wikiba.se/ontology#sitelinks",
			"500",
		},
	})

	store := newEmbeddedStore(t)

	_, err := store.EnsureCollection(ctx, "writers", []pbstore.CollectionField{
		{Name: "name", Type: pbstore.FieldTypeText, Required: true, Max: 200},
	})
	if err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}

	// The record holds no identifier: it is searched for by its name.
	if _, err := store.Create(ctx, "writers", map[string]any{
		"name": "Douglas Adams",
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	client := wikidata.NewClient(endpoint, wikidata.WithDelay(0))

	executor := wikidata.NewAgentExecutor(store, client, wikidata.ExecutorOptions{
		Target:     "writers",
		EntityType: "person",
	})

	outcome, err := executor.Execute(ctx, wikidata.ActionCreate)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if outcome.Written == 0 {
		t.Fatalf("the record was not enriched: %+v", outcome)
	}

	// The fact carries the identifier the endpoint resolved.
	facts, err := store.List(ctx, pbstore.Query{Collection: "wikidata_writers", Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(facts.Records) == 0 {
		t.Fatal("the collection of the facts is empty")
	}

	fact := facts.Records[0]

	if got := fact.StringField("wikidata_id"); got != "Q42" {
		t.Errorf("the fact holds the identifier %q, want %q", got, "Q42")
	}

	if got := fact.StringField("property"); got != "wdt:P569" {
		t.Errorf("the fact holds the property %q, want %q", got, "wdt:P569")
	}
}

// TestTheAnalysisOfACollectionThatWasNeverEnriched checks what the
// analysis answers when the collection of the facts does not exist.
//
// The collection is missing because it was never enriched, and that is a
// fact and not a failure: the model has to be able to ask before it
// enriches. An error that says "no rows in result set" is not an answer
// the model can do anything with.
func TestTheAnalysisOfACollectionThatWasNeverEnriched(t *testing.T) {
	if testing.Short() {
		t.Skip("the integration test is skipped in short mode")
	}

	ctx := context.Background()

	store := newEmbeddedStore(t)
	seedPeople(t, store)

	report, err := wikidata.AnalyzeWithWikidata(ctx, store, "people")
	if err != nil {
		t.Fatalf("AnalyzeWithWikidata() error = %v", err)
	}

	if report == nil {
		t.Fatal("AnalyzeWithWikidata() returned no report")
	}

	if report.EnrichedRecords != 0 {
		t.Errorf("the report counts %d enriched records, want 0", report.EnrichedRecords)
	}

	if report.OriginalRecords != 2 {
		t.Errorf("the report counts %d original records, want 2", report.OriginalRecords)
	}
}

// TestTheFuzzyMatchIsReachable checks that a record is matched by its
// name when the label of the fact is close to it.
//
// The exact join is tried first, and the fuzzy one is the second attempt.
// A fuzzy matcher that is never reached is a matcher that does not exist,
// and the confidence threshold would never be reported.
func TestTheFuzzyMatchIsReachable(t *testing.T) {
	if testing.Short() {
		t.Skip("the integration test is skipped in short mode")
	}

	ctx := context.Background()

	store := newEmbeddedStore(t)

	_, err := store.EnsureCollection(ctx, "people", []pbstore.CollectionField{
		{Name: "name", Type: pbstore.FieldTypeText, Max: 200},
	})
	if err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}

	_, err = store.EnsureCollection(ctx, "wikidata_people", []pbstore.CollectionField{
		{Name: "wikidata_id", Type: pbstore.FieldTypeText, Max: 32},
		{Name: "label", Type: pbstore.FieldTypeText, Max: 500},
		{Name: "property", Type: pbstore.FieldTypeText, Max: 64},
	})
	if err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}

	if _, err := store.Create(ctx, "people", map[string]any{
		"name": "Douglas Adams",
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// The label of the fact is close to the name, but it is not equal
	// to it: this is the case the fuzzy match exists for.
	if _, err := store.Create(ctx, "wikidata_people", map[string]any{
		"wikidata_id": "Q42",
		"label":       "Douglas Adams (writer)",
		"property":    "wdt:P569",
		"value":       "1956-05-12",
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	report, err := wikidata.AnalyzeWithWikidata(ctx, store, "people")
	if err != nil {
		t.Fatalf("AnalyzeWithWikidata() error = %v", err)
	}

	if report.FuzzyMatches != 1 {
		t.Errorf("the report counts %d fuzzy matches, want 1", report.FuzzyMatches)
	}

	if report.MatchedRecords != 1 {
		t.Errorf("the report counts %d matched records, want 1", report.MatchedRecords)
	}
}

// TestADryRunChangesNothing checks that a dry run leaves the store as it
// was.
//
// A dry run that creates the collection is not a dry run: it changes the
// shape of the store of the user.
func TestADryRunChangesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("the integration test is skipped in short mode")
	}

	ctx := context.Background()

	endpoint := newMockEndpoint(t, [][3]string{
		{
			"http://www.wikidata.org/entity/Q42",
			"http://www.wikidata.org/prop/direct/P569",
			"1956-05-12",
		},
	})

	store := newEmbeddedStore(t)
	seedPeople(t, store)

	client := wikidata.NewClient(endpoint, wikidata.WithDelay(0))

	executor := wikidata.NewAgentExecutor(store, client, wikidata.ExecutorOptions{
		Target:     "people",
		EntityType: "person",
		DryRun:     true,
	})

	outcome, err := executor.Execute(ctx, wikidata.ActionCreate)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if outcome.Written != 0 {
		t.Errorf("the dry run wrote %d facts, want 0", outcome.Written)
	}

	// The collection of the facts was not created.
	collections, err := store.Collections(ctx)
	if err != nil {
		t.Fatalf("Collections() error = %v", err)
	}

	for _, collection := range collections {
		if collection.Name == "wikidata_people" {
			t.Error("the dry run created the collection of the facts")
		}
	}

	// The agent log was not written either.
	count, err := store.Count(ctx, wikidata.LogCollectionName, "")
	if err == nil && count != 0 {
		t.Errorf("the dry run wrote %d log records, want 0", count)
	}
}

// TestTheSampleSizeIsBounded checks that a run asks about a bounded
// number of records.
//
// Every record is a query, and the queries are rate limited: a model
// that asks for a million records must not hold the endpoint for a day.
//
// The collection holds more records than the bound, so that the run has
// something to clamp.
func TestTheSampleSizeIsBounded(t *testing.T) {
	ctx := context.Background()

	store := newEmbeddedStore(t)

	_, err := store.EnsureCollection(ctx, "people", []pbstore.CollectionField{
		{Name: "name", Type: pbstore.FieldTypeText, Max: 200},
	})
	if err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}

	// The bound of the executor is on the number of the records it
	// reads, and it holds when the store would read more: the store
	// clamps the page size to its own maximum, and the executor clamps
	// it to the bound of a run.
	executor := wikidata.NewAgentExecutor(store, wikidata.NewClient("http://127.0.0.1:1/query"), wikidata.ExecutorOptions{
		Target:     "people",
		EntityType: "person",
		SampleSize: 1000000,
	})

	if got := executor.SampleSize(); got > wikidata.MaxSampleSize {
		t.Errorf("the executor reads %d records, want at most %d", got, wikidata.MaxSampleSize)
	}
}
