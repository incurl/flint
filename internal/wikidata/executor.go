package wikidata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	rdflibgo "github.com/tggo/goRDFlib"
	"github.com/tggo/goRDFlib/sparql"

	"github.com/incurl/flint/internal/pbstore"
)

// The names of the collections the agent writes to.
const (
	// LogCollectionName is the collection that holds the runs.
	LogCollectionName = "agent_log"
)

// The fields of the collections the agent writes to.
var (
	// LogFields are the fields of the agent log.
	LogFields = []pbstore.CollectionField{
		{Name: "action", Type: pbstore.FieldTypeText, Required: true, Max: 64},
		{Name: "collection", Type: pbstore.FieldTypeText, Max: 64},
		{Name: "query", Type: pbstore.FieldTypeText, Max: 4000},
		{Name: "records", Type: pbstore.FieldTypeNumber},
		{Name: "status", Type: pbstore.FieldTypeText, Max: 32},
		{Name: "error", Type: pbstore.FieldTypeText, Max: 2000},
		{Name: "started_at", Type: pbstore.FieldTypeDate},
	}
	// FactFields are the fields of the enriched collection.
	//
	// The wikidata_id is the join key: it is the identifier of the entity
	// the fact belongs to, and the analysis joins on it.
	FactFields = []pbstore.CollectionField{
		{Name: "wikidata_id", Type: pbstore.FieldTypeText, Required: true, Max: 32},
		{Name: "label", Type: pbstore.FieldTypeText, Max: 500},
		{Name: "description", Type: pbstore.FieldTypeText, Max: 1000},
		{Name: "property", Type: pbstore.FieldTypeText, Max: 64},
		{Name: "value", Type: pbstore.FieldTypeText, Max: 1000},
		{Name: "source_record_id", Type: pbstore.FieldTypeText, Max: 15},
		{Name: "facts", Type: pbstore.FieldTypeJSON},
	}
)

// Outcome is the result of an execution.
type Outcome struct {
	// Action is the action that was carried out.
	Action Action
	// Collection is the collection that was enriched.
	Collection string
	// Queried is the number of records that were asked about.
	Queried int
	// Written is the number of facts that were written.
	Written int
	// Failed is the number of records that could not be enriched.
	Failed int
	// Queries holds the queries that were run.
	Queries []string
}

// Executor performs a planned action.
type Executor interface {
	// Execute carries the action out.
	Execute(ctx context.Context, action Action) (Outcome, error)
}

// AgentExecutor enriches a collection with the facts of Wikidata.
//
// The executor writes through the store, and it writes idempotently: a
// fact that is already there is updated and not written twice. The store
// has no transaction, so a run that fails half way leaves the facts it
// wrote — and a second run completes the work without duplicating it.
type AgentExecutor struct {
	// store is the data layer.
	store pbstore.Store
	// client talks to the SPARQL endpoint.
	client *Client
	// target is the collection to enrich.
	target string
	// entityType is the type of the entities of the collection.
	entityType string
	// sampleSize is the number of records that are asked about.
	sampleSize int
	// logger receives the decisions and the failures.
	logger *slog.Logger
	// dryRun reports what would happen, and writes nothing.
	dryRun bool
}

// ExecutorOptions are the settings of an [AgentExecutor].
type ExecutorOptions struct {
	// Target is the collection to enrich.
	Target string
	// EntityType is the type of the entities of the collection.
	//
	// It is empty when the type has to be inferred.
	EntityType string
	// SampleSize is the number of records that are asked about.
	//
	// Zero uses [DefaultSampleSize].
	SampleSize int
	// Logger receives the decisions and the failures.
	Logger *slog.Logger
	// DryRun reports what would happen, and writes nothing.
	//
	// A dry run does not create the collection, does not write the
	// facts, and does not leave a record in the log.
	DryRun bool
}

// The bounds of a run.
//
// A run asks the endpoint about the records of a collection, and every
// record is a query. The number of the records is therefore a bound on
// the number of the queries, and a bound on the time of the run.
const (
	// DefaultSampleSize is the number of records that are asked about
	// by default.
	DefaultSampleSize = 20
	// MaxSampleSize is the largest number of records a run may ask
	// about.
	//
	// The bound is the one of the listing tools: a model that asks
	// for a million records must not be able to exhaust the memory of
	// the process, and a model that asks for ten thousand queries
	// must not be able to hold the endpoint for a day.
	MaxSampleSize = 200
)

// NewAgentExecutor returns an executor that enriches a collection.
func NewAgentExecutor(store pbstore.Store, client *Client, opts ExecutorOptions) *AgentExecutor {
	sampleSize := opts.SampleSize
	if sampleSize <= 0 {
		sampleSize = DefaultSampleSize
	}

	if sampleSize > MaxSampleSize {
		sampleSize = MaxSampleSize
	}

	return &AgentExecutor{
		store:      store,
		client:     client,
		target:     opts.Target,
		entityType: opts.EntityType,
		sampleSize: sampleSize,
		logger:     opts.Logger,
		dryRun:     opts.DryRun,
	}
}

// SampleSize returns the number of records the executor reads.
//
// It is the bound of a run: the executor reads at most this many records,
// and the queries it runs are the ones of those records.
func (e *AgentExecutor) SampleSize() int {
	return e.sampleSize
}

// Execute implements [Executor].
//
// The action decides what happens: a creation makes the collection, and
// every action but a stop enriches it.
func (e *AgentExecutor) Execute(ctx context.Context, action Action) (Outcome, error) {
	outcome := Outcome{Action: action, Collection: e.target}

	if e.store == nil {
		return outcome, fmt.Errorf("wikidata: no store configured")
	}

	if e.client == nil {
		return outcome, fmt.Errorf("wikidata: no client configured")
	}

	if action == ActionStop {
		return outcome, nil
	}

	started := time.Now().UTC()

	// A dry run changes nothing at all: it does not create the
	// collection, it does not write the facts, and it does not leave a
	// record in the log.
	if e.dryRun {
		facts, queries, queried, failed, err := e.collect(ctx)
		outcome.Queried = queried
		outcome.Failed = failed
		outcome.Queries = queries

		if err != nil {
			return outcome, err
		}

		if e.logger != nil {
			e.logger.InfoContext(ctx, "the run is a dry one, nothing was written",
				"facts", len(facts), "collection", EnrichedCollectionName(e.target))
		}

		return outcome, nil
	}

	if action == ActionCreate {
		if err := e.ensureCollection(ctx); err != nil {
			return outcome, err
		}
	}

	e.ensureLog(ctx)

	facts, queries, queried, failed, err := e.collect(ctx)
	outcome.Queried = queried
	outcome.Failed = failed
	outcome.Queries = queries

	if err != nil {
		e.writeLog(ctx, outcome, started, err)

		return outcome, err
	}

	written, err := e.persist(ctx, facts)
	outcome.Written = written

	if err != nil {
		e.writeLog(ctx, outcome, started, err)

		return outcome, err
	}

	e.writeLog(ctx, outcome, started, nil)

	return outcome, nil
}

// ensureCollection creates the enriched collection.
//
// The call is idempotent: a collection that is already there is returned
// as it is, and a second run does not fail on it.
//
// The remote backend cannot create a collection. Its error says so, and
// it is returned as it is: a run that pretends to have created the
// collection is a run that lies.
func (e *AgentExecutor) ensureCollection(ctx context.Context) error {
	_, err := e.store.EnsureCollection(ctx, EnrichedCollectionName(e.target), FactFields)
	if err != nil {
		return fmt.Errorf("wikidata: create the collection %q: %w", EnrichedCollectionName(e.target), err)
	}

	return nil
}

// ensureLog creates the agent log.
//
// The log is a convenience: a run that cannot write it still enriches, so
// the error is only logged and the run carries on.
func (e *AgentExecutor) ensureLog(ctx context.Context) {
	_, err := e.store.EnsureCollection(ctx, LogCollectionName, LogFields)
	if err != nil && e.logger != nil {
		e.logger.WarnContext(ctx, "the agent log cannot be created", "error", err)
	}
}

// fact is one property of one entity.
type fact struct {
	// qid is the identifier of the entity.
	qid string
	// label is the name of the entity.
	label string
	// property is the property, in its prefixed form.
	property string
	// value is the value of the property.
	value string
	// valueLabel is the readable form of the value.
	valueLabel string
	// sourceRecordID is the record the fact was read from.
	sourceRecordID string
}

// collect asks the endpoint about the records of the collection.
//
// A record that fails does not stop the run: one bad record must not
// take the whole enrichment with it.
func (e *AgentExecutor) collect(ctx context.Context) ([]fact, []string, int, int, error) {
	records, err := e.store.List(ctx, pbstore.Query{
		Collection: e.target,
		Page:       1,
		PerPage:    e.sampleSize,
	})
	if err != nil {
		return nil, nil, 0, 0, fmt.Errorf("wikidata: read the collection %q: %w", e.target, err)
	}

	queries := make([]string, 0, len(records.Records))

	facts := make([]fact, 0, len(records.Records))

	failed := 0

	for _, record := range records.Records {
		if err := ctx.Err(); err != nil {
			return facts, queries, len(records.Records), failed, err
		}

		query, err := e.buildQuery(ctx, record)
		if err != nil {
			failed++

			if e.logger != nil {
				e.logger.ErrorContext(ctx, "the query cannot be built",
					"record", record.ID, "error", err)
			}

			continue
		}

		queries = append(queries, query)

		rows, err := e.client.QueryRows(ctx, query)
		if err != nil {
			failed++

			e.client.LogQuery(ctx, query, 0, err)

			continue
		}

		e.client.LogQuery(ctx, query, len(rows.Bindings), nil)

		facts = append(facts, e.toFacts(record, rows)...)
	}

	return facts, queries, len(records.Records), failed, nil
}

// buildQuery builds the query for a record.
//
// The query is built from the fields of the record, and the fields of a
// PocketBase record are not the fields of a core record: the two are
// different shapes, so the query is built from a core record that is
// built from the fields of the stored one.
func (e *AgentExecutor) buildQuery(ctx context.Context, record *pbstore.Record) (string, error) {
	if record == nil {
		return "", fmt.Errorf("wikidata: no record")
	}

	coreRecord, err := e.coreRecord(ctx, record)
	if err != nil {
		return "", err
	}

	entityType := e.entityType
	if entityType == "" {
		entityType = InferEntityType(e.target, recordFieldNames(coreRecord))
		if entityType == "" {
			return "", fmt.Errorf("%w: the collection %q", ErrUnknownEntityType, e.target)
		}
	}

	return BuildQueryFromPocketBase(coreRecord, entityType)
}

// persist writes the facts into the enriched collection.
//
// The write is idempotent: a fact that is already there is updated. The
// natural key of a fact is the entity, the property and the record it
// was read from, and that is what the lookup uses.
func (e *AgentExecutor) persist(ctx context.Context, facts []fact) (int, error) {
	if len(facts) == 0 {
		return 0, nil
	}

	written := 0

	for _, item := range facts {
		if err := ctx.Err(); err != nil {
			return written, err
		}

		data := map[string]any{
			"wikidata_id":      item.qid,
			"label":            item.label,
			"property":         item.property,
			"value":            item.value,
			"source_record_id": item.sourceRecordID,
		}

		if item.valueLabel != "" {
			data["description"] = item.valueLabel
		}

		existing, err := e.findFact(ctx, item)
		if err != nil {
			return written, err
		}

		if existing != nil {
			if _, err := e.store.Update(ctx, EnrichedCollectionName(e.target), existing.ID, data); err != nil {
				return written, fmt.Errorf("wikidata: update the fact: %w", err)
			}

			written++

			continue
		}

		if _, err := e.store.Create(ctx, EnrichedCollectionName(e.target), data); err != nil {
			return written, fmt.Errorf("wikidata: write the fact: %w", err)
		}

		written++
	}

	return written, nil
}

// findFact looks a fact up by its natural key.
//
// It returns nil when there is no such fact, and the error of the store
// when the lookup itself failed.
func (e *AgentExecutor) findFact(ctx context.Context, item fact) (*pbstore.Record, error) {
	// The filter uses the native PocketBase grammar: a comparison is a
	// field, a sign and a value. The values are quoted, so that a value
	// holding a quote does not end the literal.
	filter := "wikidata_id = " + pbstore.QuoteFilterValue(item.qid) +
		" && property = " + pbstore.QuoteFilterValue(item.property) +
		" && source_record_id = " + pbstore.QuoteFilterValue(item.sourceRecordID)

	record, err := e.store.FindFirst(ctx, EnrichedCollectionName(e.target), filter)
	if err != nil {
		if errors.Is(err, pbstore.ErrNotFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("wikidata: look the fact up: %w", err)
	}

	return record, nil
}

// toFacts turns the rows of a result into facts.
func (e *AgentExecutor) toFacts(record *pbstore.Record, rows *sparql.Result) []fact {
	if rows == nil || len(rows.Bindings) == 0 {
		return nil
	}

	out := make([]fact, 0, len(rows.Bindings))

	for _, binding := range rows.Bindings {
		property := propertyIRIShort(bindingValue(binding["property"]))
		value := bindingValue(binding["value"])

		if property == "" || value == "" {
			continue
		}

		item := fact{
			qid:            entityQID(bindingValue(binding["entity"])),
			property:       property,
			value:          value,
			valueLabel:     bindingValue(binding["valueLabel"]),
			sourceRecordID: record.ID,
		}

		if item.qid == "" {
			// The query binds the entity, but a query that does not
			// would leave it empty: the identifier of the record is
			// the next best thing.
			item.qid = recordQIDFromFields(record)
		}

		if item.qid == "" {
			continue
		}

		if item.label == "" {
			item.label = recordNameFromFields(record)
		}

		out = append(out, item)
	}

	return out
}

// writeLog records a run in the agent log.
//
// The log is append-only: a run that is repeated writes a second record,
// because a log of the runs is a list of the runs. The facts are the ones
// that must not be written twice.
func (e *AgentExecutor) writeLog(ctx context.Context, outcome Outcome, started time.Time, runErr error) {
	status := "ok"
	message := ""

	if runErr != nil {
		status = "failed"
		message = runErr.Error()
	}

	query := ""
	if len(outcome.Queries) > 0 {
		query = strings.Join(outcome.Queries, "\n---\n")
	}

	data := map[string]any{
		"action":     outcome.Action.String(),
		"collection": outcome.Collection,
		"query":      query,
		"records":    outcome.Written,
		"status":     status,
		"error":      message,
		"started_at": started.Format(time.RFC3339),
	}

	if _, err := e.store.Create(ctx, LogCollectionName, data); err != nil && e.logger != nil {
		e.logger.WarnContext(ctx, "the run cannot be logged", "error", err)
	}
}

// coreRecord builds a core record out of a stored one.
//
// The query builder works on a core record, and the store hands out its
// own view of one. The record is rebuilt with the fields of the stored
// one, so that the query builder sees the fields of the collection.
func (e *AgentExecutor) coreRecord(ctx context.Context, record *pbstore.Record) (*core.Record, error) {
	schema, err := e.store.CollectionSchema(ctx, e.target)
	if err != nil {
		return nil, fmt.Errorf("wikidata: read the schema of %q: %w", e.target, err)
	}

	collection := core.NewBaseCollection(e.target)

	for _, field := range schema {
		collection.Fields.Add(&core.TextField{Name: field.Name, Max: maxFieldSize(field.Max)})
	}

	built := core.NewRecord(collection)

	for name, value := range record.Fields {
		built.Set(name, value)
	}

	return built, nil
}

// maxFieldSize returns the maximum length of a text field.
//
// The schema of a collection does not always carry a maximum, and a text
// field needs one.
func maxFieldSize(max int) int {
	if max <= 0 {
		return 500
	}

	return max
}

// bindingValue renders the value of a term.
//
// A URI is reduced to the identifier of the entity, so that the value of
// a fact is readable: "Q42" and not the whole URI.
func bindingValue(term rdflibgo.Term) string {
	if term == nil {
		return ""
	}

	switch typed := term.(type) {
	case rdflibgo.URIRef:
		return typed.Value()
	case rdflibgo.Literal:
		return fmt.Sprintf("%v", typed.Value())
	default:
		return term.String()
	}
}

// entityQID reduces the entity of a row to its identifier.
//
// The endpoint answers with the whole URI of the entity, and the
// identifier is what the facts are joined on: "Q42" and not
// "http://www.wikidata.org/entity/Q42".
func entityQID(value string) string {
	if value == "" {
		return ""
	}

	if after, ok := strings.CutPrefix(value, PrefixWD); ok {
		return after
	}

	if after, ok := strings.CutPrefix(value, "http://www.wikidata.org/entity/"); ok {
		return after
	}

	return value
}

// propertyIRIShort reduces a property to its prefixed form.
//
// The property of a fact is "wdt:P569": that is what the report names and
// what an operator reads.
func propertyIRIShort(value string) string {
	if value == "" {
		return ""
	}

	if after, ok := strings.CutPrefix(value, PrefixWDT); ok {
		return "wdt:" + after
	}

	return value
}
