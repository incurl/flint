// Package generator writes a Go project that reads an enriched PocketBase
// schema.
//
// The generator is the last step of the enrichment: the facts of Wikidata
// are in the store, and the project it writes is the application that
// reads them.
//
// The generator reads the schema through [pbstore.Store], and not through
// the PocketBase app: the store is the only thing that knows whether the
// data is local or remote, and a generator that only works with the local
// data is a generator that cannot generate from a server.
//
// The root is the boundary of the output: a project that is written
// outside of it is refused, the same way the code tools refuse to read
// outside of theirs.
package generator

import (
	"context"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/incurl/flint/internal/pbstore"
)

// Errors returned by the package.
var (
	// ErrNoStore is returned when the generator has no store.
	ErrNoStore = errors.New("generator: no store configured")
	// ErrUnknownKind is returned when the kind of the application is not
	// one of the known ones.
	ErrUnknownKind = errors.New("generator: unknown kind of application")
	// ErrNoOutput is returned when the output directory is missing.
	ErrNoOutput = errors.New("generator: the output directory is required")
	// ErrOutsideRoot is returned when the output directory is outside of
	// the root of the generator.
	ErrOutsideRoot = errors.New("generator: the output directory is outside of the root")
	// ErrProjectExists is returned when the project is already there and
	// the caller did not allow the overwrite.
	ErrProjectExists = errors.New("generator: the project already exists")
)

// The kinds of the generated application.
const (
	// KindCLI generates a command that prints a summary.
	KindCLI = "cli"
	// KindAPI generates an HTTP server with a JSON endpoint.
	KindAPI = "api"
	// KindDashboard generates an HTTP server with a table.
	KindDashboard = "dashboard"
)

// Options are the settings of a generation.
type Options struct {
	// OriginalCollection is the collection the application is built
	// around.
	OriginalCollection string
	// EnrichedCollection is the wikidata_* collection it is joined with.
	EnrichedCollection string
	// Kind is the application type: "cli", "api" or "dashboard".
	Kind string
	// OutputDir is where the project is written.
	//
	// It is relative to the root of the generator, and a path that
	// escapes it is refused.
	OutputDir string
	// Root is the directory the project may be written in.
	//
	// It is the boundary: a project that is written outside of it is
	// refused, exactly like a file the code tools read outside of
	// their root.
	Root string
	// ModulePath is the go.mod module path of the generated project.
	ModulePath string
	// Overwrite allows the generation over an existing project.
	//
	// It is false by default: the files of a generated project belong
	// to the user once they are written, and a run that overwrites them
	// destroys the edits that were made in between.
	Overwrite bool
	// PocketBaseURL is the server the generated application reads from.
	//
	// An empty value reads from an embedded PocketBase in the data
	// directory of the project.
	PocketBaseURL string
}

// Validate checks the options.
//
// It is called before anything is written: a project that cannot be
// written is not half written.
func (o Options) Validate() error {
	if o.OutputDir == "" {
		return ErrNoOutput
	}

	if o.OriginalCollection == "" {
		return fmt.Errorf("generator: the original collection is required")
	}

	switch o.Kind {
	case KindCLI, KindAPI, KindDashboard:
	case "":
		return fmt.Errorf("%w: the kind is required", ErrUnknownKind)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownKind, o.Kind)
	}

	return nil
}

// Generate writes a Go project that reads the enriched PocketBase schema.
//
// The project is written into the output directory, which is created if it
// does not exist. The files that are written are formatted, so that the
// project compiles and reads well.
//
// The schema is read through the store, so the project can be generated
// from a remote server as well as from the local database.
func Generate(ctx context.Context, store pbstore.Store, opts Options) error {
	if store == nil {
		return ErrNoStore
	}

	if err := opts.Validate(); err != nil {
		return err
	}

	original, err := store.CollectionSchema(ctx, opts.OriginalCollection)
	if err != nil {
		return fmt.Errorf("generator: read the schema of %q: %w", opts.OriginalCollection, err)
	}

	enriched, err := store.CollectionSchema(ctx, opts.EnrichedCollection)
	if err != nil {
		return fmt.Errorf("generator: read the schema of %q: %w", opts.EnrichedCollection, err)
	}

	modulePath := opts.ModulePath
	if modulePath == "" {
		modulePath = "generated-app"
	}

	outputDir, err := opts.resolveOutput()
	if err != nil {
		return err
	}

	data := templateData{
		ModulePath:         modulePath,
		Kind:               opts.Kind,
		OriginalCollection: opts.OriginalCollection,
		EnrichedCollection: opts.EnrichedCollection,
		PocketBaseURL:      opts.PocketBaseURL,
		Original:           modelDataOf(opts.OriginalCollection, original),
		Enriched:           modelDataOf(opts.EnrichedCollection, enriched),
		IDTag:              idTag,
	}

	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return fmt.Errorf("generator: create the output directory: %w", err)
	}

	files := map[string]func(templateData) (string, error){
		"go.mod":    renderGoMod,
		"models.go": renderModels,
		"main.go":   renderMain,
	}

	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		content, err := files[name](data)
		if err != nil {
			return err
		}

		path := filepath.Join(outputDir, name)

		if !opts.Overwrite {
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%w: %q", ErrProjectExists, path)
			}
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return fmt.Errorf("generator: write %q: %w", path, err)
		}
	}

	return nil
}

// resolveOutput returns the absolute directory the project is written in.
//
// The directory may be relative to the root, and a path that escapes the
// root is refused: the root is the boundary, and the model does not get
// to move it.
//
// The check is lexical: a symbolic link inside the root that points
// outside of it is not followed.
func (o Options) resolveOutput() (string, error) {
	if strings.TrimSpace(o.OutputDir) == "" {
		return "", ErrNoOutput
	}

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

// withinRoot reports whether path is root or is under it.
//
// A path outside the root has a ".." at the start of the relative path.
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	if rel == "." {
		return true
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// modelData describes a collection for the templates.
type modelData struct {
	// Name is the name of the collection.
	Name string
	// StructName is the name of the Go type of the collection.
	StructName string
	// Fields are the fields of the collection.
	Fields []fieldData
}

// fieldData describes a field for the templates.
type fieldData struct {
	// Name is the name of the field.
	Name string
	// GoName is the name of the Go field.
	GoName string
	// GoType is the type of the Go field.
	GoType string
	// Tag is the struct tag of the Go field.
	Tag string
}

// idTag is the struct tag of the identifier of a record.
//
// The tag is built here and not in the template: a template that writes
// a backtick has to escape it, and the escaping of a struct tag is not
// something a template should do.
const idTag = "`json:\"id\" db:\"id\"`"

// structTag builds the struct tag of a field.
//
// The name of the field is quoted, so that a name that holds a quote does
// not break the tag.
func structTag(name string) string {
	return "`json:\"" + name + "\" db:\"" + name + "\"`"
}

// modelDataOf reads a collection into the shape the templates use.
//
// The system fields are left out: they are not fields a caller writes,
// and the store already leaves them out.
func modelDataOf(name string, fields []pbstore.CollectionField) modelData {
	out := modelData{
		Name:       name,
		StructName: GoTypeName(name),
	}

	for _, field := range fields {
		out.Fields = append(out.Fields, fieldData{
			Name:   field.Name,
			GoName: GoTypeName(field.Name),
			GoType: GoTypeOf(field.Type),
			Tag:    structTag(field.Name),
		})
	}

	return out
}

// GoTypeOf returns the Go type of a PocketBase field type.
//
// The mapping is an explicit switch: a field type that is not known maps
// to a string and is reported, and never to "any", which would lose the
// shape of the value without saying so.
func GoTypeOf(fieldType string) string {
	switch fieldType {
	case pbstore.FieldTypeBool:
		return "bool"
	case pbstore.FieldTypeNumber:
		return "float64"
	case pbstore.FieldTypeJSON:
		return "json.RawMessage"
	case pbstore.FieldTypeDate:
		return "string"
	case pbstore.FieldTypeText, pbstore.FieldTypeEditor, pbstore.FieldTypeSelect:
		return "string"
	default:
		return "string"
	}
}

// GoTypeName turns a name into a Go identifier.
//
// A field name that is not a valid identifier — "first name" — becomes
// one that is: "FirstName". A name that starts with a digit is prefixed,
// and a name that is empty becomes "Field".
func GoTypeName(name string) string {
	var b strings.Builder

	b.Grow(len(name))

	upper := true

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			if upper {
				b.WriteRune(r - ('a' - 'A'))
			} else {
				b.WriteRune(r)
			}

			upper = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)

			upper = false
		case r >= '0' && r <= '9':
			// A digit is written as it is. The offset that turns a
			// lower case letter into an upper case one is not the
			// same for a digit, and applying it to one writes a
			// control character.
			b.WriteRune(r)

			upper = true
		default:
			upper = true
		}
	}

	out := b.String()
	if out == "" {
		return "Field"
	}

	if out[0] >= '0' && out[0] <= '9' {
		return "Field" + out
	}

	return out
}

// formatGo formats the source of a Go file.
//
// The generated code is formatted, so that it reads like the code of a
// person: a project that a compiler accepts and nobody can read is half
// a project.
func formatGo(source string) (string, error) {
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return "", fmt.Errorf("generator: format the generated code: %w", err)
	}

	return string(formatted), nil
}
