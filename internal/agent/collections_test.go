package agent_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/incurl/flint/internal/agent"
	"github.com/incurl/flint/internal/pbstore"
)

// TestCollectionSchemaTool verifies that the schema tool describes the
// fields of a collection.
func TestCollectionSchemaTool(t *testing.T) {
	store := newEmbeddedStore(t)

	ctx := context.Background()

	_, err := store.EnsureCollection(ctx, "articles", []pbstore.CollectionField{
		{Name: "title", Type: pbstore.FieldTypeText, Required: true},
		{Name: "views", Type: pbstore.FieldTypeNumber},
	})
	if err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}

	tool := agent.NewCollectionSchemaTool(store)

	result, err := tool.Run(ctx, `{"collection":"articles"}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(result, "title (text, required)") {
		t.Errorf("the schema does not describe the title field: %s", result)
	}

	if !strings.Contains(result, "views (number)") {
		t.Errorf("the schema does not describe the views field: %s", result)
	}

	// The system fields are not fields a caller writes.
	if strings.Contains(result, "created") {
		t.Errorf("the schema holds a system field: %s", result)
	}
}

// TestCollectionSchemaToolUnknown verifies that a collection that does
// not exist is an error, and not an empty schema.
//
// An empty answer for a collection that is not there would be read as
// a collection with no fields.
func TestCollectionSchemaToolUnknown(t *testing.T) {
	store := newEmbeddedStore(t)

	tool := agent.NewCollectionSchemaTool(store)

	_, err := tool.Run(context.Background(), `{"collection":"nothing"}`)
	if err == nil {
		t.Fatal("Run() error = nil, want an error")
	}

	if !errors.Is(err, pbstore.ErrNotFound) {
		t.Errorf("Run() error = %v, want ErrNotFound", err)
	}
}

// TestCollectionSchemaToolEmptyName verifies that a missing name is
// refused.
func TestCollectionSchemaToolEmptyName(t *testing.T) {
	store := newEmbeddedStore(t)

	tool := agent.NewCollectionSchemaTool(store)

	if _, err := tool.Run(context.Background(), `{"collection":"  "}`); err == nil {
		t.Fatal("Run() error = nil, want an error")
	}
}

// TestCreateCollectionTool verifies that the creation tool creates a
// collection with the requested fields.
func TestCreateCollectionTool(t *testing.T) {
	store := newEmbeddedStore(t)

	ctx := context.Background()

	tool := agent.NewCreateCollectionTool(store)

	result, err := tool.Run(ctx, `{"collection":"notes","fields":[
		{"name":"body","type":"editor","required":true},
		{"name":"kind","type":"select","options":["a","b"]}
	]}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(result, "body (editor, required)") {
		t.Errorf("the schema does not describe the body field: %s", result)
	}

	if !strings.Contains(result, "kind (select, one of a, b)") {
		t.Errorf("the schema does not describe the kind field: %s", result)
	}

	// The collection is really there.
	fields, err := store.CollectionSchema(ctx, "notes")
	if err != nil {
		t.Fatalf("CollectionSchema() error = %v", err)
	}

	if len(fields) != 2 {
		t.Fatalf("the collection has %d fields, want 2", len(fields))
	}
}

// TestCreateCollectionToolExisting verifies that a collection that
// already exists is left as it is.
//
// The call does not change a schema, and the answer says what the
// collection really holds.
func TestCreateCollectionToolExisting(t *testing.T) {
	store := newEmbeddedStore(t)

	ctx := context.Background()

	_, err := store.EnsureCollection(ctx, "notes", []pbstore.CollectionField{
		{Name: "body", Type: pbstore.FieldTypeText},
	})
	if err != nil {
		t.Fatalf("EnsureCollection() error = %v", err)
	}

	tool := agent.NewCreateCollectionTool(store)

	result, err := tool.Run(ctx, `{"collection":"notes","fields":[
		{"name":"other","type":"text"}
	]}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(result, "body (text)") {
		t.Errorf("the answer does not hold the field of the collection: %s", result)
	}

	if strings.Contains(result, "other") {
		t.Errorf("the answer holds a field that was not created: %s", result)
	}
}

// TestCreateCollectionToolSystemName verifies that a name reserved for
// the internals is refused.
func TestCreateCollectionToolSystemName(t *testing.T) {
	store := newEmbeddedStore(t)

	tool := agent.NewCreateCollectionTool(store)

	for _, name := range []string{"_flint_sessions", "_superusers", "_mine"} {
		_, err := tool.Run(context.Background(), `{"collection":"`+name+`","fields":[]}`)
		if err == nil {
			t.Errorf("Run() with the name %q error = nil, want an error", name)
		}
	}
}

// TestCreateCollectionToolBadType verifies that an unknown field type
// is refused, and that the error names the type.
func TestCreateCollectionToolBadType(t *testing.T) {
	store := newEmbeddedStore(t)

	tool := agent.NewCreateCollectionTool(store)

	_, err := tool.Run(context.Background(), `{"collection":"notes","fields":[
		{"name":"body","type":"relation"}
	]}`)
	if err == nil {
		t.Fatal("Run() error = nil, want an error")
	}

	if !strings.Contains(err.Error(), "relation") {
		t.Errorf("Run() error = %v, want the name of the type", err)
	}
}

// TestCreateCollectionToolRemote verifies that the remote backend
// refuses the creation.
//
// There, the collections are created by a migration on the server, and
// the error says so.
func TestCreateCollectionToolRemote(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the remote store called the server: %s", r.URL.Path)
	}))

	t.Cleanup(server.Close)

	store, err := pbstore.OpenRemote(pbstore.RemoteConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("OpenRemote() error = %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	tool := agent.NewCreateCollectionTool(store)

	_, err = tool.Run(context.Background(), `{"collection":"notes","fields":[]}`)
	if err == nil {
		t.Fatal("Run() error = nil, want an error")
	}

	if !strings.Contains(err.Error(), "migration") {
		t.Errorf("Run() error = %v, want an answer that names the migration", err)
	}
}

// TestCreateCollectionIsMutating verifies that the creation tool is
// behind the permission gate.
//
// The plan mode withholds it: the model cannot call a tool it does
// not have.
func TestCreateCollectionIsMutating(t *testing.T) {
	if !agent.IsMutatingTool(agent.ToolCreateCollection) {
		t.Error("the creation tool is not a mutating tool")
	}

	if agent.IsMutatingTool(agent.ToolCollectionSchema) {
		t.Error("the schema tool is a mutating tool")
	}
}

// TestPlanModeWithholdsTheCollectionCreation verifies that the planning
// mode does not offer the creation of a collection.
//
// The schema is offered: a plan needs to know what the collections
// hold.
func TestPlanModeWithholdsTheCollectionCreation(t *testing.T) {
	registry := agent.NewRegistry()
	registry.Register(&recordingTool{name: agent.ToolCollectionSchema})
	registry.Register(&recordingTool{name: agent.ToolCreateCollection})

	plan := registry.ToolNamesFor(agent.ModePlan)

	if contains(plan, agent.ToolCreateCollection) {
		t.Error("the planning mode offers the creation of a collection")
	}

	if !contains(plan, agent.ToolCollectionSchema) {
		t.Error("the planning mode does not offer the schema of a collection")
	}
}
