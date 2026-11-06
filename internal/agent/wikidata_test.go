package agent_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/wikidata"
)

// TestTheWikidataToolsAreClassified verifies that the enrichment is
// withheld from the planning mode.
//
// The enrichment writes the facts of Wikidata into the data of the user,
// exactly like a record does: the plan proposes it, the act mode carries
// it out. The analysis only reads, so the plan may use it.
func TestTheWikidataToolsAreClassified(t *testing.T) {
	if !agent.IsMutatingTool(agent.ToolWikidataEnrich) {
		t.Error("the enrichment is not classified as mutating")
	}

	if agent.IsMutatingTool(agent.ToolWikidataAnalyze) {
		t.Error("the analysis is classified as mutating")
	}

	// The generation writes files: it is a change of the same kind as
	// the one the code tools make.
	if !agent.IsMutatingTool(agent.ToolWikidataGenerate) {
		t.Error("the generation is not classified as mutating")
	}

	registry := agent.NewRegistry()
	registry.Register(&recordingTool{name: agent.ToolWikidataEnrich})
	registry.Register(&recordingTool{name: agent.ToolWikidataAnalyze})
	registry.Register(&recordingTool{name: agent.ToolWikidataGenerate})

	plan := toolNames(registry.AgentToolsFor(agent.ModePlan))
	act := toolNames(registry.AgentToolsFor(agent.ModeAct))

	if contains(plan, agent.ToolWikidataEnrich) {
		t.Error("the planning mode offers the enrichment")
	}

	if !contains(act, agent.ToolWikidataEnrich) {
		t.Error("the acting mode does not offer the enrichment")
	}

	if contains(plan, agent.ToolWikidataGenerate) {
		t.Error("the planning mode offers the generation")
	}

	if !contains(act, agent.ToolWikidataGenerate) {
		t.Error("the acting mode does not offer the generation")
	}

	if !contains(plan, agent.ToolWikidataAnalyze) {
		t.Error("the planning mode does not offer the analysis")
	}
}

// TestTheGenerateToolWritesInsideTheRoot is the test of the boundary of
// the generation.
//
// The model chooses the directory, and a directory outside of the root
// is refused: the root is the boundary, and the model does not get to
// move it.
func TestTheGenerateToolWritesInsideTheRoot(t *testing.T) {
	root := t.TempDir()

	store := &schemaStore{collections: map[string][]pbstore.CollectionField{
		"people": {
			{Name: "name", Type: pbstore.FieldTypeText, Max: 200},
		},
		"wikidata_people": {
			{Name: "wikidata_id", Type: pbstore.FieldTypeText, Max: 32},
			{Name: "property", Type: pbstore.FieldTypeText, Max: 64},
		},
	}}

	tool := agent.NewWikidataGenerateTool(store, root, nil)

	out, err := tool.Run(context.Background(), `{"collection":"people","output_dir":"app"}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out, "app") {
		t.Errorf("the answer does not name the directory: %s", out)
	}

	if _, err := os.Stat(filepath.Join(root, "app", "go.mod")); err != nil {
		t.Errorf("the project was not written: %v", err)
	}

	// A path that escapes the root is refused.
	_, err = tool.Run(context.Background(), `{"collection":"people","output_dir":"../escaped"}`)
	if err == nil {
		t.Error("the tool wrote outside of the root")
	}

	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escaped")); err == nil {
		t.Error("the tool wrote outside of the root")
	}
}

// TestTheWikidataToolsDeclareTheirArguments verifies that the tools
// declare the schema of their arguments.
//
// The schema is what the model reads to call the tool: a tool that does
// not declare one is called with whatever the model invents.
func TestTheWikidataToolsDeclareTheirArguments(t *testing.T) {
	tools := []agent.Tool{
		agent.NewWikidataEnrichTool(nil, wikidata.NewClient("http://127.0.0.1:1/query"), nil),
		agent.NewWikidataAnalyzeTool(nil, wikidata.NewClient("http://127.0.0.1:1/query"), nil),
		agent.NewWikidataGenerateTool(nil, t.TempDir(), nil),
	}

	for _, tool := range tools {
		schemer, ok := tool.(agent.Schemer)
		if !ok {
			t.Errorf("the tool %q does not declare a schema", tool.Name())

			continue
		}

		properties, required := schemer.Schema()

		if len(properties) == 0 {
			t.Errorf("the tool %q declares no property", tool.Name())
		}

		if len(required) == 0 {
			t.Errorf("the tool %q declares no mandatory argument", tool.Name())
		}
	}
}

// TestTheWikidataToolsHaveADescription verifies that the tools describe
// themselves.
//
// The description is the only thing that tells the model when to call
// the tool, and a tool without one is a tool the model calls at random.
func TestTheWikidataToolsHaveADescription(t *testing.T) {
	tools := []agent.Tool{
		agent.NewWikidataEnrichTool(nil, wikidata.NewClient("http://127.0.0.1:1/query"), nil),
		agent.NewWikidataAnalyzeTool(nil, wikidata.NewClient("http://127.0.0.1:1/query"), nil),
		agent.NewWikidataGenerateTool(nil, t.TempDir(), nil),
	}

	for _, tool := range tools {
		description := tool.Description()

		if len(description) < 80 {
			t.Errorf("the description of %q is too short: %q", tool.Name(), description)
		}

		if !strings.Contains(strings.ToLower(description), "wikidata") {
			t.Errorf("the description of %q does not mention Wikidata", tool.Name())
		}
	}
}

// TestTheWikidataToolsRejectAMalformedInput verifies that a malformed
// argument is an error and not a crash.
//
// A tool that panics on a malformed input takes the whole run down.
func TestTheWikidataToolsRejectAMalformedInput(t *testing.T) {
	client := wikidata.NewClient("http://127.0.0.1:1/query", wikidata.WithLogger(slog.New(slog.DiscardHandler)))

	tools := []agent.Tool{
		agent.NewWikidataEnrichTool(nil, client, nil),
		agent.NewWikidataAnalyzeTool(nil, client, nil),
		agent.NewWikidataGenerateTool(nil, t.TempDir(), nil),
	}

	for _, tool := range tools {
		if _, err := tool.Run(context.Background(), "{not json"); err == nil {
			t.Errorf("the tool %q accepted a malformed input", tool.Name())
		}
	}
}

// TestTheWikidataToolsNeedACollection verifies that the collection is
// checked before the call reaches the endpoint.
func TestTheWikidataToolsNeedACollection(t *testing.T) {
	client := wikidata.NewClient("http://127.0.0.1:1/query", wikidata.WithLogger(slog.New(slog.DiscardHandler)))

	tools := []agent.Tool{
		agent.NewWikidataEnrichTool(nil, client, nil),
		agent.NewWikidataAnalyzeTool(nil, client, nil),
		agent.NewWikidataGenerateTool(nil, t.TempDir(), nil),
	}

	for _, tool := range tools {
		_, err := tool.Run(context.Background(), `{"collection":""}`)
		if err == nil {
			t.Errorf("the tool %q accepted an empty collection", tool.Name())
		}
	}
}

// TestTheWikidataAnalyzeToolAnswersWithAReport verifies that the
// analysis returns the report as JSON.
//
// The report is what a caller reads, and it is JSON so that a pipeline
// can parse it.
func TestTheWikidataAnalyzeToolAnswersWithAReport(t *testing.T) {
	store := &countingStore{}

	client := wikidata.NewClient("http://127.0.0.1:1/query", wikidata.WithLogger(slog.New(slog.DiscardHandler)))

	tool := agent.NewWikidataAnalyzeTool(store, client, nil)

	out, err := tool.Run(context.Background(), `{"collection":"people"}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var report map[string]any

	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("the answer is not JSON: %v\n%s", err, out)
	}

	if report["original_collection"] != "people" {
		t.Errorf("the report names %v, want %q", report["original_collection"], "people")
	}
}

// countingStore is a store that holds no record.
//
// It is the smallest store the analysis accepts: the analysis reads the
// two sides of the join, and an empty store is a join with nothing.
type countingStore struct {
	pbstore.Store
}

// List implements the part of the store the analysis uses.
func (s *countingStore) List(context.Context, pbstore.Query) (*pbstore.PageResult, error) {
	return &pbstore.PageResult{}, nil
}

// schemaStore answers the schema of the collections it holds.
type schemaStore struct {
	pbstore.Store

	// collections are the schemas, keyed by the name of the collection.
	collections map[string][]pbstore.CollectionField
}

// CollectionSchema implements the part of the store the generator uses.
func (s *schemaStore) CollectionSchema(_ context.Context, name string) ([]pbstore.CollectionField, error) {
	fields, ok := s.collections[name]
	if !ok {
		return nil, pbstore.ErrNotFound
	}

	return fields, nil
}

// Collections implements the part of the store the planner uses.
func (s *schemaStore) Collections(context.Context) ([]pbstore.Collection, error) {
	out := make([]pbstore.Collection, 0, len(s.collections))

	for name := range s.collections {
		out = append(out, pbstore.Collection{Name: name})
	}

	return out, nil
}

// Count implements the part of the store the planner uses.
func (s *schemaStore) Count(context.Context, string, string) (int, error) {
	return 0, nil
}

// List implements the part of the store the executor uses.
func (s *schemaStore) List(context.Context, pbstore.Query) (*pbstore.PageResult, error) {
	return &pbstore.PageResult{}, nil
}

// EnsureCollection implements the part of the store the executor uses.
func (s *schemaStore) EnsureCollection(_ context.Context, name string, _ []pbstore.CollectionField) (pbstore.Collection, error) {
	s.collections[name] = nil

	return pbstore.Collection{Name: name}, nil
}

// FindFirst implements the part of the store the executor uses.
func (s *schemaStore) FindFirst(context.Context, string, string) (*pbstore.Record, error) {
	return nil, pbstore.ErrNotFound
}

// TestTheEnrichToolInfersTheTypeFromTheFields checks that the tool
// infers the entity type of a collection whose name does not identify
// it.
//
// The description of the tool says that the type is inferred from the
// collection and its fields. A collection named "writers" holds people,
// and the fields are what says so.
func TestTheEnrichToolInfersTheTypeFromTheFields(t *testing.T) {
	ctx := context.Background()

	store := newEmbeddedStore(t)

	// A collection whose name does not identify its type, but whose
	// fields do.
	_, err := store.EnsureCollection(ctx, "writers", []pbstore.CollectionField{
		{Name: "name", Type: pbstore.FieldTypeText, Max: 200},
		{Name: "birth_date", Type: pbstore.FieldTypeText, Max: 32},
	})
	if err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}

	client := wikidata.NewClient("http://127.0.0.1:1/query")

	tool := agent.NewWikidataEnrichTool(store, client, nil)

	// The tool does not refuse the collection: the type is inferred
	// from the fields, and the run fails on the endpoint, which is
	// not reachable in the test.
	_, err = tool.Run(ctx, `{"collection":"writers"}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
}

// TestTheGenerateToolKeepsTheEditsOfTheUser checks that a generation
// does not destroy what the user did with the project.
//
// The files of a generated project belong to the user once they are
// written, and a run that overwrites them takes the edits with it.
func TestTheGenerateToolKeepsTheEditsOfTheUser(t *testing.T) {
	ctx := context.Background()

	store := &schemaStore{collections: map[string][]pbstore.CollectionField{
		"people": {
			{Name: "name", Type: pbstore.FieldTypeText, Max: 200},
		},
		"wikidata_people": {
			{Name: "wikidata_id", Type: pbstore.FieldTypeText, Max: 32},
			{Name: "property", Type: pbstore.FieldTypeText, Max: 64},
		},
	}}

	root := t.TempDir()

	tool := agent.NewWikidataGenerateTool(store, root, nil)

	if _, err := tool.Run(ctx, `{"collection":"people","output_dir":"app"}`); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// The user edits the generated project.
	main := filepath.Join(root, "app", "main.go")

	content, err := os.ReadFile(main)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	edited := make([]byte, 0, len(content)+16)
	edited = append(edited, content...)
	edited = append(edited, []byte("\n// MY EDIT\n")...)

	if err := os.WriteFile(main, edited, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// A second run is refused: the project is there.
	_, err = tool.Run(ctx, `{"collection":"people","output_dir":"app"}`)
	if err == nil {
		t.Fatal("the second run overwrote the project")
	}

	// The edit is still there.
	after, err := os.ReadFile(main)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !strings.Contains(string(after), "MY EDIT") {
		t.Error("the edit of the user was destroyed")
	}

	// The overwrite is allowed when it is asked for.
	if _, err := tool.Run(ctx, `{"collection":"people","output_dir":"app","overwrite":true}`); err != nil {
		t.Fatalf("Run() with the overwrite error = %v", err)
	}

	final, err := os.ReadFile(main)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if strings.Contains(string(final), "MY EDIT") {
		t.Error("the overwrite did not replace the file")
	}
}
