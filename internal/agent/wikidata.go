package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/incurl/flint/internal/generator"
	"github.com/incurl/flint/internal/pbstore"
	"github.com/incurl/flint/internal/wikidata"
)

// The names of the Wikidata tools.
//
// They are the identifiers the model uses to call the tools, so they are
// part of the contract with the model and they must stay stable.
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

// The descriptions of the tools.
//
// They are the prose the model reads to decide what to call, so they are
// written for the model and not for the user of the template.
const (
	wikidataEnrichDescription = "Enrich a PocketBase collection with the facts of Wikidata. " +
		"Use it when the user asks to add Wikidata data to a collection: the dates, the places, " +
		"the countries and the other properties of the entities the collection holds. " +
		"The facts are written into a new collection named wikidata_<collection>, one record per fact, " +
		"and the run is idempotent: a fact that is already there is updated, not duplicated. " +
		"Give the collection to enrich. The entity type is inferred from the collection and its fields " +
		"unless it is given; the known types are person, place and organisation. " +
		"A record that holds a wikidata_id or a qid field is asked about directly, and a record that " +
		"only holds a name is searched for by that name."

	wikidataAnalyzeDescription = "Report what the Wikidata enrichment of a collection achieved. " +
		"Use it after an enrichment, or when the user asks how many records of a collection were " +
		"matched with Wikidata. It joins the collection with its wikidata_ collection and returns " +
		"the number of records that were matched, the number that were not, the number of facts per " +
		"property, and how many of the matches were made by name instead of by identifier."

	wikidataGenerateDescription = "Write a Go project that reads an enriched PocketBase collection " +
		"and the Wikidata facts joined to it. " +
		"Use it when the user asks for an application, a command, an API or a dashboard that reads " +
		"the enriched data. " +
		"The project is written into a directory inside the code directory of the application, and it " +
		"holds a go.mod, the models of the two collections and a main file. " +
		"Give the collection to build the project around: its facts are read from the collection " +
		"named wikidata_<collection>. " +
		"The kind decides the shape of the application: cli prints a summary, api serves the records " +
		"as JSON, and dashboard serves a table. " +
		"A project that is already there is not replaced unless the overwrite is asked for: the files " +
		"belong to the user once they are written."
)

// The wikidataTimeout bounds a call of the Wikidata tools.
//
// It is longer than the timeout of the other tools: a call to a public
// endpoint waits for the endpoint, and the endpoint is not the machine
// of the user.
const wikidataTimeout = 2 * time.Minute

// WikidataEnrichParams are the arguments of the wikidata_enrich tool.
type WikidataEnrichParams struct {
	// Collection is the collection to enrich.
	Collection string `json:"collection" description:"The name of the PocketBase collection to enrich with the facts of Wikidata."`
	// EntityType is the type of the entities of the collection.
	//
	// It is inferred from the collection and its fields when it is
	// empty. The known types are person, place and organisation.
	EntityType string `json:"entity_type,omitempty" description:"The type of the entities of the collection: person, place or organisation. Leave empty to infer it from the collection and its fields."`
	// SampleSize is the number of records that are asked about.
	SampleSize int `json:"sample_size,omitempty" description:"How many records of the collection to ask Wikidata about. Leave empty for the default of 20. It is capped at 200."`
	// DryRun reports what would happen, and changes nothing.
	DryRun bool `json:"dry_run,omitempty" description:"Ask Wikidata what it would find, and write nothing. Use it to check a collection before enriching it."`
}

// WikidataAnalyzeParams are the arguments of the wikidata_analyze tool.
//
// The name of the tool is the contract with the model, and it keeps the
// American spelling: it is a name, not a word of a sentence.
type WikidataAnalyzeParams struct {
	// Collection is the collection to analyse.
	Collection string `json:"collection" description:"The name of the PocketBase collection that was enriched with the facts of Wikidata."`
}

// WikidataGenerateParams are the arguments of the wikidata_generate tool.
type WikidataGenerateParams struct {
	// Collection is the collection the project is built around.
	Collection string `json:"collection" description:"The name of the PocketBase collection the project is built around. Its facts are read from the collection named wikidata_<collection>."`
	// Kind is the shape of the application.
	//
	// It is "cli" when it is not given.
	Kind string `json:"kind,omitempty" description:"The shape of the generated application: cli, api or dashboard. Leave empty for cli."`
	// OutputDir is where the project is written.
	//
	// It is relative to the code directory of the application, and a
	// path that escapes it is refused.
	OutputDir string `json:"output_dir,omitempty" description:"The directory the project is written in, relative to the code directory of the application. Leave empty for the default."`
	// ModulePath is the module path of the generated project.
	ModulePath string `json:"module_path,omitempty" description:"The module path of the generated project, as it appears in its go.mod. Leave empty for the default."`
	// Overwrite replaces a project that is already there.
	//
	// It is false by default: the files of a generated project belong
	// to the user once they are written.
	Overwrite bool `json:"overwrite,omitempty" description:"Replace the files of a project that is already there. Leave it false to keep the edits of the user."`
}

// wikidataTool is a tool that talks to Wikidata.
//
// The arguments are decoded from the JSON the model produced, and the
// values are checked before they reach the store.
type wikidataTool[P any] struct {
	// store is the data layer.
	store pbstore.Store
	// client talks to the SPARQL endpoint.
	client *wikidata.Client
	// root is the directory the generated project may be written in.
	//
	// It is the boundary of the generation, exactly like the root is
	// the boundary of the code tools.
	root string
	// logger receives the decisions and the failures.
	logger *slog.Logger
	// name is the tool name.
	name string
	// description is the prose the model reads.
	description string
	// run executes the tool with the decoded arguments.
	run func(ctx context.Context, tool *wikidataTool[P], params *P) (string, error)
}

// Name implements [Tool].
func (t *wikidataTool[P]) Name() string { return t.name }

// Description implements [Tool].
func (t *wikidataTool[P]) Description() string { return t.description }

// Schema implements [Schemer].
func (t *wikidataTool[P]) Schema() (map[string]any, []string) {
	return schemaOf[P]()
}

// Run implements [Tool].
//
// The arguments are decoded into the parameter type of the tool. A
// malformed argument is an error the model can correct, not a crash.
func (t *wikidataTool[P]) Run(ctx context.Context, input string) (string, error) {
	if t.store == nil {
		return "", fmt.Errorf("agent: no store configured")
	}

	// The generation does not talk to Wikidata: it reads the schema of
	// the store. The other two tools need the client.
	if t.client == nil && t.name != ToolWikidataGenerate {
		return "", fmt.Errorf("agent: no Wikidata client configured")
	}

	params := new(P)

	if strings.TrimSpace(input) != "" {
		if err := json.Unmarshal([]byte(input), params); err != nil {
			return "", fmt.Errorf("%w: %w", ErrInvalidToolInput, err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, wikidataTimeout)
	defer cancel()

	return t.run(ctx, t, params)
}

// RegisterWikidataTools registers the tools that talk to Wikidata.
//
// The enrichment writes to the data of the user, so it goes behind the
// permission gate. The analysis only reads, so it does not.
//
// The tools are not registered when there is no client: a tool that
// cannot work must not be offered to the model.
func RegisterWikidataTools(
	registry *Registry,
	store pbstore.Store,
	client *wikidata.Client,
	gate *Gate,
	root string,
	logger *slog.Logger,
) {
	if registry == nil || store == nil || client == nil {
		return
	}

	registry.Register(NewWikidataAnalyzeTool(store, client, logger))
	registry.Register(NewGuardedTool(
		NewWikidataEnrichTool(store, client, logger),
		gate,
		wikidataEnrichApproval,
	))
	registry.Register(NewGuardedTool(
		NewWikidataGenerateTool(store, root, logger),
		gate,
		wikidataGenerateApproval,
	))
}

// The approvals the user reads.
const wikidataEnrichApproval = "Enrich a collection with the facts of Wikidata. " +
	"The facts are written into a new collection named wikidata_<collection>."

const wikidataGenerateApproval = "Write a Go project that reads the enriched collection. " +
	"It is written into a directory of the code directory of the application."

// NewWikidataEnrichTool returns the tool that enriches a collection.
func NewWikidataEnrichTool(store pbstore.Store, client *wikidata.Client, logger *slog.Logger) Tool {
	return &wikidataTool[WikidataEnrichParams]{
		store:       store,
		client:      client,
		logger:      logger,
		name:        ToolWikidataEnrich,
		description: wikidataEnrichDescription,
		run:         runWikidataEnrich,
	}
}

// NewWikidataAnalyzeTool returns the tool that reports what an
// enrichment achieved.
func NewWikidataAnalyzeTool(store pbstore.Store, client *wikidata.Client, logger *slog.Logger) Tool {
	return &wikidataTool[WikidataAnalyzeParams]{
		store:       store,
		client:      client,
		logger:      logger,
		name:        ToolWikidataAnalyze,
		description: wikidataAnalyzeDescription,
		run:         runWikidataAnalyze,
	}
}

// NewWikidataGenerateTool returns the tool that writes the Go project.
//
// The root is the boundary of the generation: a project that is written
// outside of it is refused.
func NewWikidataGenerateTool(store pbstore.Store, root string, logger *slog.Logger) Tool {
	return &wikidataTool[WikidataGenerateParams]{
		store:       store,
		root:        root,
		logger:      logger,
		name:        ToolWikidataGenerate,
		description: wikidataGenerateDescription,
		run:         runWikidataGenerate,
	}
}

// runWikidataEnrich enriches a collection with the facts of Wikidata.
//
// The planner decides what to do: the collection is created when it does
// not exist yet, and it is enriched when it does.
func runWikidataEnrich(
	ctx context.Context,
	tool *wikidataTool[WikidataEnrichParams],
	params *WikidataEnrichParams,
) (string, error) {
	if strings.TrimSpace(params.Collection) == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	// The type is inferred from the name of the collection and from the
	// fields it holds: a collection named "writers" holds people, and
	// the fields are what says so.
	schema, err := tool.store.CollectionSchema(ctx, params.Collection)
	if err != nil {
		return "", fmt.Errorf("agent: read the schema of %q: %w", params.Collection, err)
	}

	fields := make([]string, 0, len(schema))
	for _, field := range schema {
		fields = append(fields, field.Name)
	}

	entityType, err := wikidata.InferEntityTypeFor(
		params.Collection,
		fields,
		params.EntityType,
	)
	if err != nil {
		return "", fmt.Errorf("agent: %w", err)
	}

	planner := wikidata.NewStorePlanner(tool.store, tool.logger)

	state, err := wikidata.ReadState(ctx, tool.store, params.Collection, 0)
	if err != nil {
		return "", err
	}

	action, err := planner.Plan(ctx, state)
	if err != nil {
		return "", err
	}

	if action == wikidata.ActionStop {
		return fmt.Sprintf(
			"the collection %q is already enriched with %d facts; nothing to do",
			params.Collection, state.EnrichedRecords), nil
	}

	executor := wikidata.NewAgentExecutor(tool.store, tool.client, wikidata.ExecutorOptions{
		Target:     params.Collection,
		EntityType: entityType,
		SampleSize: params.SampleSize,
		Logger:     tool.logger,
		DryRun:     params.DryRun,
	})

	outcome, err := executor.Execute(ctx, action)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"enriched %q with %d facts from %d records (%d could not be enriched); "+
			"the facts are in the collection %q",
		params.Collection, outcome.Written, outcome.Queried, outcome.Failed,
		wikidata.EnrichedCollectionName(params.Collection)), nil
}

// runWikidataGenerate writes the Go project that reads the enriched
// collection.
//
// The project is written inside the root, and a path that escapes it is
// refused: the root is the boundary, and the model does not get to move
// it.
func runWikidataGenerate(
	ctx context.Context,
	tool *wikidataTool[WikidataGenerateParams],
	params *WikidataGenerateParams,
) (string, error) {
	if strings.TrimSpace(params.Collection) == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	if strings.TrimSpace(tool.root) == "" {
		return "", fmt.Errorf("agent: the code directory is required")
	}

	kind := strings.TrimSpace(params.Kind)
	if kind == "" {
		kind = generator.KindCLI
	}

	outputDir := strings.TrimSpace(params.OutputDir)
	if outputDir == "" {
		outputDir = "generated"
	}

	err := generator.Generate(ctx, tool.store, generator.Options{
		OriginalCollection: params.Collection,
		EnrichedCollection: wikidata.EnrichedCollectionName(params.Collection),
		Kind:               kind,
		OutputDir:          outputDir,
		Root:               tool.root,
		ModulePath:         params.ModulePath,
		Overwrite:          params.Overwrite,
	})
	if err != nil {
		if errors.Is(err, generator.ErrProjectExists) {
			return "", fmt.Errorf("%w; ask for the overwrite to replace it", err)
		}

		return "", err
	}

	return fmt.Sprintf(
		"the %s project is written in %q; it reads %q and %q",
		kind, filepath.Join(tool.root, outputDir),
		params.Collection,
		wikidata.EnrichedCollectionName(params.Collection)), nil
}

// runWikidataAnalyze reports what the enrichment of a collection
// achieved.
func runWikidataAnalyze(
	ctx context.Context,
	tool *wikidataTool[WikidataAnalyzeParams],
	params *WikidataAnalyzeParams,
) (string, error) {
	if strings.TrimSpace(params.Collection) == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	report, err := wikidata.AnalyzeWithWikidataConfig(ctx, tool.store, params.Collection, wikidata.AnalysisOptions{
		Logger: tool.logger,
	})
	if err != nil {
		return "", err
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		return "", fmt.Errorf("agent: encode the report: %w", err)
	}

	return string(encoded), nil
}
