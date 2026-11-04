package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"charm.land/fantasy/schema"

	"github.com/incurl/flint/internal/pbstore"
)

// The timeouts bound the store calls that the tools run.
//
// They are shorter than the ones of the pages: a tool runs inside a
// model turn, and a turn that waits for a slow query burns the token
// budget of the user for nothing.
const (
	// toolTimeout bounds a single tool call.
	toolTimeout = 30 * time.Second
)

// The tool names.
//
// They are the identifiers the model uses to call the tools, so they
// are part of the contract with the model and must stay stable.
const (
	// ToolListRecords lists the records of a collection.
	ToolListRecords = "list_records"
	// ToolGetRecord returns a single record.
	ToolGetRecord = "get_record"
	// ToolSearchRecords searches the records of a collection.
	ToolSearchRecords = "search_records"
	// ToolCountRecords counts the records of a collection.
	ToolCountRecords = "count_records"
	// ToolCreateRecord creates a record.
	ToolCreateRecord = "create_record"
	// ToolUpdateRecord updates a record.
	ToolUpdateRecord = "update_record"
	// ToolDeleteRecord deletes a record.
	ToolDeleteRecord = "delete_record"
	// ToolListCollections lists the available collections.
	ToolListCollections = "list_collections"
)

// storeTool is a tool backed by the PocketBase data layer.
//
// The arguments are decoded from the JSON the model produced, and the
// values are checked before they reach the store: a model that invents
// a field name must not be able to write it.
//
// The parameter type is a type parameter, so the decoding is typed and
// the schema of the tool can be generated from the same struct.
type storeTool[P any] struct {
	// store is the data layer.
	store pbstore.Store
	// name is the tool name.
	name string
	// description is the prose the model reads.
	description string
	// run executes the tool with the decoded arguments.
	run func(ctx context.Context, store pbstore.Store, params *P) (string, error)
}

// Name implements [Tool].
func (t *storeTool[P]) Name() string { return t.name }

// Description implements [Tool].
func (t *storeTool[P]) Description() string { return t.description }

// Schema implements [Schemer].
//
// The schema is generated from the parameter struct, so the description
// of every argument lives in one place: the struct tags.
func (t *storeTool[P]) Schema() (map[string]any, []string) {
	generated := schema.Generate(reflect.TypeOf(*new(P)))

	properties := make(map[string]any, len(generated.Properties))

	for name, property := range generated.Properties {
		properties[name] = property
	}

	return properties, generated.Required
}

// Run implements [Tool].
//
// The arguments are decoded into the parameter type of the tool. A
// malformed argument is an error the model can correct, not a crash.
func (t *storeTool[P]) Run(ctx context.Context, input string) (string, error) {
	if t.store == nil {
		return "", fmt.Errorf("agent: no store configured")
	}

	params := new(P)

	if strings.TrimSpace(input) != "" {
		if err := json.Unmarshal([]byte(input), params); err != nil {
			return "", fmt.Errorf("%w: %w", ErrInvalidToolInput, err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()

	return t.run(ctx, t.store, params)
}

// The parameter types of the tools.
//
// The `json` and `description` tags are read by the model library to
// generate the schema of the tool, so they are part of the contract
// with the model: a wrong description makes the model call the tool
// with the wrong arguments.

// ListRecordsParams are the arguments of the list_records tool.
type ListRecordsParams struct {
	// Collection is the name of the collection to list.
	Collection string `json:"collection" description:"The name of the collection to list the records of."`
	// Filter narrows the records, in the PocketBase filter grammar.
	Filter string `json:"filter,omitempty" description:"A PocketBase filter expression, for example: status = \"active\". Leave empty to list everything."`
	// Sort orders the records, in the PocketBase sort grammar.
	Sort string `json:"sort,omitempty" description:"A PocketBase sort expression, for example: -created,name. Leave empty for the default order."`
	// Page is the page number, starting at 1.
	Page int `json:"page,omitempty" description:"The page number to fetch, starting at 1."`
	// PerPage is the number of records per page.
	PerPage int `json:"per_page,omitempty" description:"How many records to fetch per page."`
}

// GetRecordParams are the arguments of the get_record tool.
type GetRecordParams struct {
	// Collection is the name of the collection.
	Collection string `json:"collection" description:"The name of the collection the record belongs to."`
	// ID is the identifier of the record.
	ID string `json:"id" description:"The id of the record to fetch."`
}

// SearchRecordsParams are the arguments of the search_records tool.
type SearchRecordsParams struct {
	// Collection is the name of the collection to search.
	Collection string `json:"collection" description:"The name of the collection to search."`
	// Term is the text to look for.
	Term string `json:"term" description:"The text to search for. It is matched against the text fields of the records."`
	// PerPage caps the number of results.
	PerPage int `json:"per_page,omitempty" description:"How many results to return at most."`
}

// CountRecordsParams are the arguments of the count_records tool.
type CountRecordsParams struct {
	// Collection is the name of the collection to count.
	Collection string `json:"collection" description:"The name of the collection to count the records of."`
	// Filter narrows the counted records.
	Filter string `json:"filter,omitempty" description:"A PocketBase filter expression, for example: status = \"active\". Leave empty to count everything."`
}

// CreateRecordParams are the arguments of the create_record tool.
type CreateRecordParams struct {
	// Collection is the name of the collection to create the record in.
	Collection string `json:"collection" description:"The name of the collection to create the record in."`
	// Fields holds the values of the record.
	Fields map[string]any `json:"fields" description:"The fields of the new record, as a JSON object of field names and values."`
}

// UpdateRecordParams are the arguments of the update_record tool.
type UpdateRecordParams struct {
	// Collection is the name of the collection.
	Collection string `json:"collection" description:"The name of the collection the record belongs to."`
	// ID is the identifier of the record.
	ID string `json:"id" description:"The id of the record to update."`
	// Fields holds the values to write.
	Fields map[string]any `json:"fields" description:"The fields to write, as a JSON object of field names and values. The fields that are not listed are left untouched."`
}

// DeleteRecordParams are the arguments of the delete_record tool.
type DeleteRecordParams struct {
	// Collection is the name of the collection.
	Collection string `json:"collection" description:"The name of the collection the record belongs to."`
	// ID is the identifier of the record.
	ID string `json:"id" description:"The id of the record to delete."`
}

// ListCollectionsParams are the arguments of the list_collections tool.
type ListCollectionsParams struct{}

// RegisterStoreTools registers the tools that speak to the store.
//
// The read-only tools come first, so that the model sees them before
// the mutating ones: the order of the tool list is the order the model
// reads, and it is part of what makes a model reach for the right tool.
//
// The mutating tools are wrapped in a permission gate. A nil gate means
// that they run unguarded, which is only appropriate for a tool that
// the caller trusts: the agent writes to the data of the user.
func RegisterStoreTools(registry *Registry, store pbstore.Store, gate *Gate) {
	if registry == nil {
		return
	}

	registry.Register(NewListRecordsTool(store))
	registry.Register(NewGetRecordTool(store))
	registry.Register(NewSearchRecordsTool(store))
	registry.Register(NewCountRecordsTool(store))
	registry.Register(NewListCollectionsTool(store))

	registry.Register(NewGuardedTool(NewCreateRecordTool(store), gate, createRecordApproval))
	registry.Register(NewGuardedTool(NewUpdateRecordTool(store), gate, updateRecordApproval))
	registry.Register(NewGuardedTool(NewDeleteRecordTool(store), gate, deleteRecordApproval))
}

// RegisterTodosTool registers the task list tool.
//
// The tool is not gated: it changes nothing of the data. It changes
// what the model is doing, and the user sees it.
func RegisterTodosTool(registry *Registry, runner *Runner) {
	if registry == nil || runner == nil {
		return
	}

	registry.Register(NewTodosTool(runner.todos))
}

// The approvals the user reads.
//
// They say what the call would do, because the user has to decide
// without reading the arguments.
const (
	createRecordApproval = "Create a new record in the collection."
	updateRecordApproval = "Change the fields of an existing record."
	deleteRecordApproval = "Delete a record. It cannot be undone."
)

// NewListRecordsTool returns the tool that lists the records of a
// collection.
func NewListRecordsTool(store pbstore.Store) Tool {
	return &storeTool[ListRecordsParams]{
		store:       store,
		name:        ToolListRecords,
		description: listRecordsDescription,
		run:         runListRecords,
	}
}

// NewGetRecordTool returns the tool that fetches a single record.
func NewGetRecordTool(store pbstore.Store) Tool {
	return &storeTool[GetRecordParams]{
		store:       store,
		name:        ToolGetRecord,
		description: getRecordDescription,
		run:         runGetRecord,
	}
}

// NewSearchRecordsTool returns the tool that searches the records of a
// collection.
func NewSearchRecordsTool(store pbstore.Store) Tool {
	return &storeTool[SearchRecordsParams]{
		store:       store,
		name:        ToolSearchRecords,
		description: searchRecordsDescription,
		run:         runSearchRecords,
	}
}

// NewCountRecordsTool returns the tool that counts the records of a
// collection.
func NewCountRecordsTool(store pbstore.Store) Tool {
	return &storeTool[CountRecordsParams]{
		store:       store,
		name:        ToolCountRecords,
		description: countRecordsDescription,
		run:         runCountRecords,
	}
}

// NewListCollectionsTool returns the tool that lists the collections.
func NewListCollectionsTool(store pbstore.Store) Tool {
	return &storeTool[ListCollectionsParams]{
		store:       store,
		name:        ToolListCollections,
		description: listCollectionsDescription,
		run:         runListCollections,
	}
}

// NewCreateRecordTool returns the tool that creates a record.
func NewCreateRecordTool(store pbstore.Store) Tool {
	return &storeTool[CreateRecordParams]{
		store:       store,
		name:        ToolCreateRecord,
		description: createRecordDescription,
		run:         runCreateRecord,
	}
}

// NewUpdateRecordTool returns the tool that updates a record.
func NewUpdateRecordTool(store pbstore.Store) Tool {
	return &storeTool[UpdateRecordParams]{
		store:       store,
		name:        ToolUpdateRecord,
		description: updateRecordDescription,
		run:         runUpdateRecord,
	}
}

// NewDeleteRecordTool returns the tool that deletes a record.
func NewDeleteRecordTool(store pbstore.Store) Tool {
	return &storeTool[DeleteRecordParams]{
		store:       store,
		name:        ToolDeleteRecord,
		description: deleteRecordDescription,
		run:         runDeleteRecord,
	}
}

// The bounds of the tools.
//
// A model that asks for a million records must not be able to exhaust
// the memory of the process: the tools clamp what the model asks for.
const (
	// defaultToolPerPage is the page size of a listing tool.
	defaultToolPerPage = 30
	// maxToolPerPage is the largest page a listing tool returns.
	maxToolPerPage = 100
)

// clampPerPage returns the page size to use.
func clampPerPage(perPage int) int {
	switch {
	case perPage <= 0:
		return defaultToolPerPage
	case perPage > maxToolPerPage:
		return maxToolPerPage
	default:
		return perPage
	}
}

// runListRecords lists the records of a collection.
func runListRecords(ctx context.Context, store pbstore.Store, params *ListRecordsParams) (string, error) {
	if params.Collection == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	page := params.Page
	if page <= 0 {
		page = 1
	}

	result, err := store.List(ctx, pbstore.Query{
		Collection: params.Collection,
		Filter:     params.Filter,
		Sort:       params.Sort,
		Page:       page,
		PerPage:    clampPerPage(params.PerPage),
	})
	if err != nil {
		return "", err
	}

	return describeRecords(result), nil
}

// runGetRecord fetches a single record.
func runGetRecord(ctx context.Context, store pbstore.Store, params *GetRecordParams) (string, error) {
	if params.Collection == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	if params.ID == "" {
		return "", fmt.Errorf("agent: the record id is required")
	}

	record, err := store.Get(ctx, params.Collection, params.ID)
	if err != nil {
		return "", err
	}

	return describeRecord(record), nil
}

// runSearchRecords searches the records of a collection.
//
// The search is a filter over the text fields of the records, in the
// PocketBase grammar.
func runSearchRecords(ctx context.Context, store pbstore.Store, params *SearchRecordsParams) (string, error) {
	if params.Collection == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	if params.Term == "" {
		return "", fmt.Errorf("agent: the search term is required")
	}

	filter := fmt.Sprintf("title ~ %q", params.Term)

	result, err := store.List(ctx, pbstore.Query{
		Collection: params.Collection,
		Filter:     filter,
		Page:       1,
		PerPage:    clampPerPage(params.PerPage),
	})
	if err != nil {
		return "", err
	}

	return describeRecords(result), nil
}

// runCountRecords counts the records of a collection.
func runCountRecords(ctx context.Context, store pbstore.Store, params *CountRecordsParams) (string, error) {
	if params.Collection == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	count, err := store.Count(ctx, params.Collection, params.Filter)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%d records", count), nil
}

// runListCollections lists the available collections.
//
// The internal collections are left out: the model reads the answer as
// the data of the application, and the superusers, the authentication
// helpers and the stored conversations are not that data. Listing them
// only invites the model to browse the internals of the store.
func runListCollections(ctx context.Context, store pbstore.Store, _ *ListCollectionsParams) (string, error) {
	collections, err := store.Collections(ctx)
	if err != nil {
		return "", err
	}

	names := make([]string, 0, len(collections))
	for _, collection := range collections {
		if pbstore.IsSystemCollection(collection.Name) {
			continue
		}

		names = append(names, collection.Name)
	}

	if len(names) == 0 {
		return "no collections", nil
	}

	return strings.Join(names, "\n"), nil
}

// runCreateRecord creates a record.
func runCreateRecord(ctx context.Context, store pbstore.Store, params *CreateRecordParams) (string, error) {
	if params.Collection == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	if len(params.Fields) == 0 {
		return "", fmt.Errorf("agent: the fields are required")
	}

	record, err := store.Create(ctx, params.Collection, params.Fields)
	if err != nil {
		return "", err
	}

	return "created: " + describeRecord(record), nil
}

// runUpdateRecord updates a record.
func runUpdateRecord(ctx context.Context, store pbstore.Store, params *UpdateRecordParams) (string, error) {
	if params.Collection == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	if params.ID == "" {
		return "", fmt.Errorf("agent: the record id is required")
	}

	if len(params.Fields) == 0 {
		return "", fmt.Errorf("agent: the fields are required")
	}

	record, err := store.Update(ctx, params.Collection, params.ID, params.Fields)
	if err != nil {
		return "", err
	}

	return "updated: " + describeRecord(record), nil
}

// runDeleteRecord deletes a record.
func runDeleteRecord(ctx context.Context, store pbstore.Store, params *DeleteRecordParams) (string, error) {
	if params.Collection == "" {
		return "", fmt.Errorf("agent: the collection is required")
	}

	if params.ID == "" {
		return "", fmt.Errorf("agent: the record id is required")
	}

	if err := store.Delete(ctx, params.Collection, params.ID); err != nil {
		return "", err
	}

	return "deleted " + params.ID, nil
}

// The descriptions of the tools.
//
// They are the prose the model reads to decide what to call, so they
// are written for the model and not for the user of the template.
const (
	listRecordsDescription = "List the records of a PocketBase collection. " +
		"Use it to see what is stored before answering a question about the data. " +
		"The filter and the sort use the PocketBase grammar: " +
		"`status = \"active\"` for a filter, `-created,name` for a sort."

	getRecordDescription = "Fetch a single record of a PocketBase collection by its id. " +
		"Use it when the id is already known, for example after a listing returned it."

	searchRecordsDescription = "Search the records of a PocketBase collection for a text. " +
		"The text is matched against the title field of the records. " +
		"Use it to find a record whose content the user described."

	countRecordsDescription = "Count the records of a PocketBase collection. " +
		"Use it when the user asks how many records there are, " +
		"optionally narrowed by a PocketBase filter."

	listCollectionsDescription = "List the available PocketBase collections. " +
		"Use it when the collection is not known, or to see what the data holds. " +
		"The internal collections of PocketBase and of the application are not listed: " +
		"they are not data."

	createRecordDescription = "Create a record in a PocketBase collection. " +
		"Use it when the user asks to add, save or create something."

	updateRecordDescription = "Update the fields of an existing record. " +
		"The fields that are not listed are left untouched."

	deleteRecordDescription = "Delete a record from a PocketBase collection. " +
		"Use it only when the user explicitly asks for the record to be removed."
)

// describeRecords renders a page of records for the model.
//
// The rendering is compact: the model gets the fields it needs without
// the noise of the full payload, and a page that was truncated says so.
func describeRecords(result *pbstore.PageResult) string {
	if result == nil || len(result.Records) == 0 {
		return "no records"
	}

	var b strings.Builder

	for _, record := range result.Records {
		b.WriteString(describeRecord(record))
		b.WriteString("\n")
	}

	if result.Truncated {
		b.WriteString("(the list is partial)\n")
	}

	return strings.TrimSpace(b.String())
}

// describeRecord renders a single record for the model.
func describeRecord(record *pbstore.Record) string {
	if record == nil {
		return ""
	}

	fields, err := record.JSON()
	if err != nil {
		return record.ID
	}

	return record.ID + ": " + fields
}
