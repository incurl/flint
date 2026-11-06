package generator_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/incurl/flint/internal/generator"
	"github.com/incurl/flint/internal/pbstore"
)

// newStore creates a store that holds the collections of an enriched
// store.
//
// The store is a fake and not a real PocketBase: the generator reads the
// schema through the store, and a fake that answers with the same
// schema is enough to exercise it.
func newStore(t *testing.T, collections map[string][]pbstore.CollectionField) pbstore.Store {
	t.Helper()

	return &fakeStore{collections: collections}
}

// fakeStore answers the schema of the collections it holds.
type fakeStore struct {
	pbstore.Store

	// collections are the schemas, keyed by the name of the collection.
	collections map[string][]pbstore.CollectionField
}

// CollectionSchema implements the part of the store the generator uses.
func (s *fakeStore) CollectionSchema(_ context.Context, name string) ([]pbstore.CollectionField, error) {
	fields, ok := s.collections[name]
	if !ok {
		return nil, pbstore.ErrNotFound
	}

	return fields, nil
}

// peopleFields is the schema of the collection of the people.
func peopleFields() []pbstore.CollectionField {
	return []pbstore.CollectionField{
		{Name: "name", Type: pbstore.FieldTypeText, Required: true, Max: 200},
		{Name: "wikidata_id", Type: pbstore.FieldTypeText, Max: 32},
		{Name: "meta", Type: pbstore.FieldTypeJSON},
		{Name: "rank", Type: pbstore.FieldTypeNumber},
	}
}

// enrichedFields is the schema of the collection of the facts.
func enrichedFields() []pbstore.CollectionField {
	return []pbstore.CollectionField{
		{Name: "wikidata_id", Type: pbstore.FieldTypeText, Required: true, Max: 32},
		{Name: "label", Type: pbstore.FieldTypeText, Max: 500},
		{Name: "property", Type: pbstore.FieldTypeText, Max: 64},
		{Name: "value", Type: pbstore.FieldTypeText, Max: 1000},
	}
}

// TestGenerateProducesAProjectThatCompiles is the test of the generator.
//
// A generated project that does not compile is worse than no generated
// project, so the test builds every kind of the project it writes.
func TestGenerateProducesAProjectThatCompiles(t *testing.T) {
	if testing.Short() {
		t.Skip("the generation test is slow, and it is skipped in short mode")
	}

	for _, kind := range []string{generator.KindCLI, generator.KindAPI, generator.KindDashboard} {
		t.Run(kind, func(t *testing.T) {
			store := newStore(t, map[string][]pbstore.CollectionField{
				"people":          peopleFields(),
				"wikidata_people": enrichedFields(),
			})

			root := t.TempDir()
			dir := filepath.Join(root, "project")

			err := generator.Generate(context.Background(), store, generator.Options{
				OriginalCollection: "people",
				EnrichedCollection: "wikidata_people",
				Kind:               kind,
				OutputDir:          "project",
				Root:               root,
				ModulePath:         "example.com/generated",
			})
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			for _, name := range []string{"go.mod", "models.go", "main.go"} {
				path := filepath.Join(dir, name)

				if _, err := os.Stat(path); err != nil {
					t.Fatalf("the file %q was not written: %v", name, err)
				}
			}

			buildGenerated(t, dir)
		})
	}
}

// buildGenerated compiles the generated project.
//
// The build runs in the directory of the project, with the module cache of
// the machine: the generated project depends on the PocketBase SDK, and
// the SDK is already in the cache.
func buildGenerated(t *testing.T, dir string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(t.TempDir(), "generated"), "./...")
	cmd.Dir = dir

	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the generated project does not build: %v\n%s", err, out)
	}
}

// TestGenerateRejectsAnUnknownKind checks that an unknown kind is an
// error and not a project of the wrong shape.
func TestGenerateRejectsAnUnknownKind(t *testing.T) {
	store := newStore(t, map[string][]pbstore.CollectionField{
		"people":          peopleFields(),
		"wikidata_people": enrichedFields(),
	})

	err := generator.Generate(context.Background(), store, generator.Options{
		OriginalCollection: "people",
		EnrichedCollection: "wikidata_people",
		Kind:               "spreadsheet",
		OutputDir:          "project",
		Root:               t.TempDir(),
	})
	if err == nil {
		t.Fatal("Generate() error = nil, want an error")
	}
}

// TestGenerateRejectsAMissingOutputDirectory checks that the options are
// checked before anything is written.
func TestGenerateRejectsAMissingOutputDirectory(t *testing.T) {
	store := newStore(t, map[string][]pbstore.CollectionField{
		"people":          peopleFields(),
		"wikidata_people": enrichedFields(),
	})

	err := generator.Generate(context.Background(), store, generator.Options{
		OriginalCollection: "people",
		EnrichedCollection: "wikidata_people",
		Kind:               generator.KindCLI,
		Root:               t.TempDir(),
	})
	if err == nil {
		t.Fatal("Generate() error = nil, want an error")
	}
}

// TestGenerateRefusesAnOutputOutsideOfTheRoot is the test of the
// boundary.
//
// The model chooses the directory, and a directory outside of the root
// is refused: the root is the boundary, and the model does not get to
// move it.
func TestGenerateRefusesAnOutputOutsideOfTheRoot(t *testing.T) {
	store := newStore(t, map[string][]pbstore.CollectionField{
		"people":          peopleFields(),
		"wikidata_people": enrichedFields(),
	})

	root := t.TempDir()

	outputs := []string{
		"../escaped",
		"nested/../../escaped",
		filepath.Join(root, "..", "escaped"),
	}

	for _, output := range outputs {
		err := generator.Generate(context.Background(), store, generator.Options{
			OriginalCollection: "people",
			EnrichedCollection: "wikidata_people",
			Kind:               generator.KindCLI,
			OutputDir:          output,
			Root:               root,
		})
		if err == nil {
			t.Errorf("Generate() with the output %q error = nil, want an error", output)
		}
	}
}

// TestGenerateRefusesAnAbsoluteOutputOutsideOfTheRoot checks that an
// absolute path is refused as well.
//
// The path may be absolute, and it may not escape the root either.
func TestGenerateRefusesAnAbsoluteOutputOutsideOfTheRoot(t *testing.T) {
	store := newStore(t, map[string][]pbstore.CollectionField{
		"people":          peopleFields(),
		"wikidata_people": enrichedFields(),
	})

	err := generator.Generate(context.Background(), store, generator.Options{
		OriginalCollection: "people",
		EnrichedCollection: "wikidata_people",
		Kind:               generator.KindCLI,
		OutputDir:          "/tmp/escaped",
		Root:               t.TempDir(),
	})
	if err == nil {
		t.Fatal("Generate() error = nil, want an error")
	}
}

// TestGenerateTurnsAFieldNameIntoAnIdentifier checks that a field name
// that is not a valid Go identifier becomes one.
//
// A field called "first name" would not compile as a struct field.
func TestGenerateTurnsAFieldNameIntoAnIdentifier(t *testing.T) {
	store := newStore(t, map[string][]pbstore.CollectionField{
		"odd": {
			{Name: "first_name", Type: pbstore.FieldTypeText, Max: 100},
			{Name: "2nd_place", Type: pbstore.FieldTypeText, Max: 100},
		},
		"wikidata_people": enrichedFields(),
	})

	root := t.TempDir()

	err := generator.Generate(context.Background(), store, generator.Options{
		OriginalCollection: "odd",
		EnrichedCollection: "wikidata_people",
		Kind:               generator.KindCLI,
		OutputDir:          "project",
		Root:               root,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	content, err := os.ReadFile(filepath.Join(root, "project", "models.go"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	source := string(content)

	if !strings.Contains(source, "FirstName") {
		t.Errorf("the field name was not converted: %s", source)
	}

	if !strings.Contains(source, "Field2NdPlace") {
		t.Errorf("the field name was not prefixed: %s", source)
	}

	// A control character in an identifier is not an identifier: the
	// generated code would not compile.
	for _, r := range source {
		if r < 32 && r != '\t' && r != '\n' {
			t.Fatalf("the generated code holds a control character: %q", source)

			break
		}
	}
}

// TestGoTypeNameTurnsNamesIntoIdentifiers checks the conversion of a name
// into a Go identifier.
//
// The bug it guards against is the one of the digit: the offset that
// turns a lower case letter into an upper case one does not turn a digit
// into an upper case one, and applying it to a digit writes a control
// character — which is not an identifier, and the generated code would
// not compile.
func TestGoTypeNameTurnsNamesIntoIdentifiers(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "id", want: "Id"},
		{name: "first_name", want: "FirstName"},
		{name: "first name", want: "FirstName"},
		{name: "first-name", want: "FirstName"},
		{name: "2nd_place", want: "Field2NdPlace"},
		{name: "wikidata_id", want: "WikidataId"},
		{name: "", want: "Field"},
		{name: "___", want: "Field"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := generator.GoTypeName(test.name)

			if got != test.want {
				t.Errorf("GoTypeName(%q) = %q, want %q", test.name, got, test.want)
			}

			for _, r := range got {
				if r < 32 {
					t.Errorf("GoTypeName(%q) = %q holds a control character", test.name, got)
				}
			}
		})
	}
}

// TestTheGeneratedProjectIsTidy checks that "go mod tidy" leaves the
// generated project as it is.
//
// A go.mod that a tidy changes is a go.mod that was not right: the
// requirement it holds is one the project does not use, and a project
// whose go.mod does not survive a tidy is a project that does not
// build for the next person.
func TestTheGeneratedProjectIsTidy(t *testing.T) {
	if testing.Short() {
		t.Skip("the generation test is slow, and it is skipped in short mode")
	}

	store := newStore(t, map[string][]pbstore.CollectionField{
		"people":          peopleFields(),
		"wikidata_people": enrichedFields(),
	})

	root := t.TempDir()
	dir := filepath.Join(root, "project")

	err := generator.Generate(context.Background(), store, generator.Options{
		OriginalCollection: "people",
		EnrichedCollection: "wikidata_people",
		Kind:               generator.KindCLI,
		OutputDir:          "project",
		Root:               root,
		ModulePath:         "example.com/generated",
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	before, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = dir

	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy error = %v\n%s", err, out)
	}

	after, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(before) != string(after) {
		t.Errorf("the tidy changed the go.mod:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
}
