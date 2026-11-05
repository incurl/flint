package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/incurl/flint/internal/pbstore"
)

// The descriptions the model reads.
const (
	collectionSchemaDescription = "Describe the fields of a collection: the " +
		"names of the fields, their types, and whether they are required. " +
		"Use it before writing a record, to know what the collection holds."

	createCollectionDescription = "Create a collection with the given fields. " +
		"The collection is created when it does not exist yet; when it " +
		"already exists, its fields are left as they are. " +
		"The field types are: text, editor, json, number, bool, date, " +
		"select and autodate."
)

// The parameter types of the collection tools.

// CollectionSchemaParams are the arguments of the collection_schema tool.
type CollectionSchemaParams struct {
	// Collection is the name of the collection.
	Collection string `json:"collection" description:"The name of the collection to describe."`
}

// CreateCollectionParams are the arguments of the create_collection
// tool.
type CreateCollectionParams struct {
	// Collection is the name of the collection.
	Collection string `json:"collection" description:"The name of the collection to create."`
	// Fields are the fields of the collection.
	Fields []CollectionFieldParams `json:"fields" description:"The fields of the collection."`
}

// CollectionFieldParams describes one field of a collection.
//
// The type of the field is a plain string, because the schema of a
// tool is generated from the struct: the model reads the tag of every
// argument, and a nested struct would not be described.
type CollectionFieldParams struct {
	// Name is the name of the field.
	Name string `json:"name" description:"The name of the field."`
	// Type is the type of the field.
	Type string `json:"type" description:"The type of the field: text, editor, json, number, bool, date, select or autodate."`
	// Required marks the field as required.
	Required bool `json:"required,omitempty" description:"The field must hold a value."`
	// Max is the maximum length of a text field.
	Max int `json:"max,omitempty" description:"The maximum length of a text field. It is 500 by default."`
	// Options lists the allowed values of a select field.
	Options []string `json:"options,omitempty" description:"The allowed values of a select field."`
}

// NewCollectionSchemaTool returns the tool that describes a
// collection.
//
// The tool reads the shape of the collection, and not its records: the
// names of the fields, their types, and whether they are required.
//
// It is not gated: the shape of a collection is not the data of the
// user, and a model that cannot see it writes records that the store
// refuses.
func NewCollectionSchemaTool(store pbstore.Store) Tool {
	return &storeTool[CollectionSchemaParams]{
		store:       store,
		name:        ToolCollectionSchema,
		description: collectionSchemaDescription,
		run:         runCollectionSchema,
	}
}

// NewCreateCollectionTool returns the tool that creates a collection.
//
// The tool is behind the permission gate: a collection is part of the
// data of the user, and creating one changes the shape of it.
func NewCreateCollectionTool(store pbstore.Store) Tool {
	return &storeTool[CreateCollectionParams]{
		store:       store,
		name:        ToolCreateCollection,
		description: createCollectionDescription,
		run:         runCreateCollection,
	}
}

// runCollectionSchema describes the fields of a collection.
func runCollectionSchema(
	ctx context.Context,
	store pbstore.Store,
	params *CollectionSchemaParams,
) (string, error) {
	name := strings.TrimSpace(params.Collection)

	if name == "" {
		return "", errors.New("agent: the collection name is required")
	}

	fields, err := store.CollectionSchema(ctx, name)
	if err != nil {
		return "", err
	}

	return formatSchema(name, fields), nil
}
