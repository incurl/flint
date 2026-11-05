package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/incurl/flint/internal/pbstore"
)

// runCreateCollection creates a collection with the given fields.
//
// The call is refused when the name is reserved for the internals of
// PocketBase or of the application: those collections are not the data
// of the user, and a model that creates one of them writes into the
// book-keeping of the store.
//
// A collection that already exists is left as it is, and its actual
// fields are reported: the call does not change a schema, and saying
// otherwise would be a lie.
func runCreateCollection(
	ctx context.Context,
	store pbstore.Store,
	params *CreateCollectionParams,
) (string, error) {
	name := strings.TrimSpace(params.Collection)

	if name == "" {
		return "", errors.New("agent: the collection name is required")
	}

	if pbstore.IsSystemCollection(name) {
		return "", fmt.Errorf("agent: the collection %q is reserved for the internals", name)
	}

	fields, err := buildFields(params.Fields)
	if err != nil {
		return "", err
	}

	_, err = store.EnsureCollection(ctx, name, fields)
	if err != nil {
		return "", err
	}

	// The schema that is reported is the one of the collection, and
	// not the one that was asked for: a collection that already
	// existed keeps its fields.
	actual, err := store.CollectionSchema(ctx, name)
	if err != nil {
		return "", err
	}

	return formatSchema(name, actual), nil
}

// buildFields converts the fields of the model into the fields of the
// store.
//
// The types are checked here, and not by the store: the error of the
// store names the field, and the one of the tool names the type that
// is not supported.
func buildFields(params []CollectionFieldParams) ([]pbstore.CollectionField, error) {
	fields := make([]pbstore.CollectionField, 0, len(params))

	for _, param := range params {
		name := strings.TrimSpace(param.Name)

		if name == "" {
			return nil, errors.New("agent: the name of a field is required")
		}

		fieldType := strings.TrimSpace(param.Type)
		if fieldType == "" {
			fieldType = pbstore.FieldTypeText
		}

		if !validFieldType(fieldType) {
			return nil, fmt.Errorf(
				"agent: the type %q of the field %q is not supported; it is one of %s",
				fieldType,
				name,
				strings.Join(supportedFieldTypes(), ", "),
			)
		}

		fields = append(fields, pbstore.CollectionField{
			Name:     name,
			Type:     fieldType,
			Required: param.Required,
			Max:      param.Max,
			Options:  param.Options,
		})
	}

	return fields, nil
}

// validFieldType reports whether a type is one the store supports.
func validFieldType(fieldType string) bool {
	for _, supported := range supportedFieldTypes() {
		if supported == fieldType {
			return true
		}
	}

	return false
}

// supportedFieldTypes returns the types a collection field may have.
func supportedFieldTypes() []string {
	return []string{
		pbstore.FieldTypeText,
		pbstore.FieldTypeEditor,
		pbstore.FieldTypeJSON,
		pbstore.FieldTypeNumber,
		pbstore.FieldTypeBool,
		pbstore.FieldTypeDate,
		pbstore.FieldTypeSelect,
		pbstore.FieldTypeAutodate,
	}
}

// formatSchema renders the fields of a collection.
//
// The line of a field reads `name (type)`, and the qualifiers follow
// inside the parentheses: `name (text, required)`.
func formatSchema(name string, fields []pbstore.CollectionField) string {
	if len(fields) == 0 {
		return fmt.Sprintf("the collection %q has no field", name)
	}

	var b strings.Builder

	fmt.Fprintf(&b, "the collection %q has %d field(s):\n\n", name, len(fields))

	for _, field := range fields {
		fmt.Fprintf(&b, "%s (%s", field.Name, field.Type)

		if field.Required {
			b.WriteString(", required")
		}

		if len(field.Options) > 0 {
			fmt.Fprintf(&b, ", one of %s", strings.Join(field.Options, ", "))
		}

		b.WriteString(")\n")
	}

	return b.String()
}
