package agent_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/pbstore"
)

// newEmbeddedStore creates an embedded store backed by a temporary
// PocketBase data directory.
func newEmbeddedStore(t *testing.T) *pbstore.Embedded {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "pb_data")

	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	return store
}

// seedCollection creates a collection with a few records.
//
// The dictionary collection always carries a required "key" field, so
// the records are seeded with one.
func seedCollection(t *testing.T, store *pbstore.Embedded) {
	t.Helper()

	ctx := context.Background()

	if _, err := store.EnsureDictionaryCollection(ctx, "vocab", []pbstore.DictionaryField{
		{Name: "label", Type: pbstore.FieldTypeText},
	}); err != nil {
		t.Fatalf("EnsureDictionaryCollection() error = %v", err)
	}

	for _, term := range []string{"alpha", "beta", "gamma"} {
		if _, err := store.Create(ctx, "vocab", map[string]any{"key": term}); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
}

// firstRecordID returns the id of the first record of a collection.
func firstRecordID(t *testing.T, store *pbstore.Embedded) string {
	t.Helper()

	result, err := store.List(context.Background(), pbstore.Query{
		Collection: "vocab",
		Page:       1,
		PerPage:    1,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(result.Records) == 0 {
		t.Fatal("the collection is empty")
	}

	return result.Records[0].ID
}

// TestListRecordsTool verifies that the listing tool returns the records
// of a collection.
func TestListRecordsTool(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	tool := agent.NewListRecordsTool(store)

	out, err := tool.Run(context.Background(), `{"collection":"vocab"}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	for _, term := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(out, term) {
			t.Errorf("Run() = %q, want it to contain %q", out, term)
		}
	}
}

// TestListRecordsToolRequiresCollection verifies that a tool call
// without the required argument is an error the model can correct.
func TestListRecordsToolRequiresCollection(t *testing.T) {
	store := newEmbeddedStore(t)

	tool := agent.NewListRecordsTool(store)

	if _, err := tool.Run(context.Background(), `{}`); err == nil {
		t.Error("Run() returned no error for a missing collection")
	}
}

// TestListRecordsToolRejectsBadJSON verifies that malformed arguments
// are reported rather than panicking.
func TestListRecordsToolRejectsBadJSON(t *testing.T) {
	store := newEmbeddedStore(t)

	tool := agent.NewListRecordsTool(store)

	_, err := tool.Run(context.Background(), `{"collection":`)
	if !errors.Is(err, agent.ErrInvalidToolInput) {
		t.Errorf("Run() error = %v, want ErrInvalidToolInput", err)
	}
}

// TestGetRecordTool verifies that the tool returns a single record.
func TestGetRecordTool(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	id := firstRecordID(t, store)

	tool := agent.NewGetRecordTool(store)

	out, err := tool.Run(context.Background(), `{"collection":"vocab","id":"`+id+`"}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out, id) {
		t.Errorf("Run() = %q, want it to contain the record id", out)
	}
}

// TestCountRecordsTool verifies that the counting tool counts the
// records.
func TestCountRecordsTool(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	tool := agent.NewCountRecordsTool(store)

	out, err := tool.Run(context.Background(), `{"collection":"vocab"}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out, "3") {
		t.Errorf("Run() = %q, want it to count three records", out)
	}
}

// TestListCollectionsTool verifies that the tool lists the collections.
func TestListCollectionsTool(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	tool := agent.NewListCollectionsTool(store)

	out, err := tool.Run(context.Background(), `{}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out, "vocab") {
		t.Errorf("Run() = %q, want it to contain the collection name", out)
	}
}

// TestCreateRecordTool verifies that the model can create a record.
func TestCreateRecordTool(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	tool := agent.NewCreateRecordTool(store)

	out, err := tool.Run(context.Background(), `{"collection":"vocab","fields":{"key":"delta"}}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out, "delta") {
		t.Errorf("Run() = %q, want it to contain the new record", out)
	}

	count, err := store.Count(context.Background(), "vocab", "")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if count != 4 {
		t.Errorf("the collection holds %d records, want 4", count)
	}
}

// TestUpdateRecordTool verifies that the model can update a record.
func TestUpdateRecordTool(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	id := firstRecordID(t, store)

	tool := agent.NewUpdateRecordTool(store)

	if _, err := tool.Run(context.Background(),
		`{"collection":"vocab","id":"`+id+`","fields":{"label":"renamed"}}`); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	record, err := store.Get(context.Background(), "vocab", id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if record.StringField("label") != "renamed" {
		t.Errorf("the record label is %q, want renamed", record.StringField("title"))
	}
}

// TestDeleteRecordTool verifies that the model can delete a record.
func TestDeleteRecordTool(t *testing.T) {
	store := newEmbeddedStore(t)
	seedCollection(t, store)

	id := firstRecordID(t, store)

	tool := agent.NewDeleteRecordTool(store)

	if _, err := tool.Run(context.Background(), `{"collection":"vocab","id":"`+id+`"}`); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	count, err := store.Count(context.Background(), "vocab", "")
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if count != 2 {
		t.Errorf("the collection holds %d records, want 2", count)
	}
}

// TestRegisterStoreTools verifies that the registry holds the whole
// tool set.
func TestRegisterStoreTools(t *testing.T) {
	store := newEmbeddedStore(t)

	registry := agent.NewRegistry()
	agent.RegisterStoreTools(registry, store, nil)

	names := registry.Names()
	if len(names) != 8 {
		t.Fatalf("the registry holds %d tools, want 8: %v", len(names), names)
	}

	for _, want := range []string{
		agent.ToolListRecords,
		agent.ToolGetRecord,
		agent.ToolCountRecords,
		agent.ToolCreateRecord,
		agent.ToolUpdateRecord,
		agent.ToolDeleteRecord,
	} {
		found := false

		for _, name := range names {
			if name == want {
				found = true

				break
			}
		}

		if !found {
			t.Errorf("the registry does not hold %q", want)
		}
	}
}

// TestToolSchemaIsExposed verifies that the model receives the schema
// of the arguments.
//
// The schema is what makes a tool usable: without it the model has to
// guess the names of the arguments.
func TestToolSchemaIsExposed(t *testing.T) {
	store := newEmbeddedStore(t)

	info := agent.NewWrappedTool(agent.NewListRecordsTool(store)).Info()

	if len(info.Parameters) == 0 {
		t.Fatal("the tool exposes no parameters")
	}

	if _, ok := info.Parameters["collection"]; !ok {
		t.Errorf("the schema holds %v, want a collection parameter", info.Parameters)
	}

	if len(info.Required) != 1 || info.Required[0] != "collection" {
		t.Errorf("Required = %v, want [collection]", info.Required)
	}
}
