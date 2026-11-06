package wikidata_test

import (
	"testing"

	"github.com/incurl/flint/internal/wikidata"
)

// TestRegisterPropertySetAddsAnEntityType checks the extension point of
// the registry.
//
// A downstream project registers the properties of an entity type of its
// own, and the query builder picks them up: the registry is data, and
// the docs say that it is.
func TestRegisterPropertySetAddsAnEntityType(t *testing.T) {
	const entityType = "species"

	err := wikidata.RegisterPropertySet(wikidata.PropertySet{
		EntityType:  entityType,
		Description: "A species of animal.",
		Properties: []wikidata.Property{
			{IRI: "wdt:P141", Label: "conservation status", FullIRI: "http://www.wikidata.org/prop/direct/P141"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterPropertySet() error = %v", err)
	}

	set, err := wikidata.PropertySetFor(entityType)
	if err != nil {
		t.Fatalf("PropertySetFor() error = %v", err)
	}

	if len(set.Properties) != 1 {
		t.Fatalf("the set holds %d properties, want 1: %v", len(set.Properties), set.Properties)
	}

	if set.Properties[0].IRI != "wdt:P141" {
		t.Errorf("the property is %q, want %q", set.Properties[0].IRI, "wdt:P141")
	}

	// The type is known now.
	found := false

	for _, name := range wikidata.KnownEntityTypes() {
		if name == entityType {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("the entity type is not known: %v", wikidata.KnownEntityTypes())
	}
}

// TestRegisterPropertySetMergesTheProperties checks that a registration
// that adds one property does not drop the others.
//
// A downstream project that adds a property to a known entity type
// keeps the properties of the template.
func TestRegisterPropertySetMergesTheProperties(t *testing.T) {
	before, err := wikidata.PropertySetFor("person")
	if err != nil {
		t.Fatalf("PropertySetFor() error = %v", err)
	}

	err = wikidata.RegisterPropertySet(wikidata.PropertySet{
		EntityType: "person",
		Properties: []wikidata.Property{
			{IRI: "wdt:P214", Label: "VIAF ID", FullIRI: "http://www.wikidata.org/prop/direct/P214"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterPropertySet() error = %v", err)
	}

	after, err := wikidata.PropertySetFor("person")
	if err != nil {
		t.Fatalf("PropertySetFor() error = %v", err)
	}

	if len(after.Properties) != len(before.Properties)+1 {
		t.Errorf("the set holds %d properties, want %d", len(after.Properties), len(before.Properties)+1)
	}
}

// TestRegisterPropertySetRejectsAnEmptyEntityType checks that a
// registration without a name is an error.
func TestRegisterPropertySetRejectsAnEmptyEntityType(t *testing.T) {
	if err := wikidata.RegisterPropertySet(wikidata.PropertySet{}); err == nil {
		t.Error("RegisterPropertySet() error = nil, want an error")
	}
}

// TestRegisterFieldPropertyAddsAField checks the second half of the
// extension point.
//
// A collection that holds a field the template does not know about can
// say what that field is worth.
func TestRegisterFieldPropertyAddsAField(t *testing.T) {
	err := wikidata.RegisterFieldProperty("first_published", "wdt:P577")
	if err != nil {
		t.Fatalf("RegisterFieldProperty() error = %v", err)
	}

	record := newRecord(t, map[string]string{
		"name":            "The Book",
		"first_published": "1979",
	})

	properties := wikidata.InferProperties(record, "person")

	found := false

	for _, property := range properties {
		if property.IRI == "wdt:P577" {
			found = true

			break
		}
	}

	if !found {
		t.Errorf("the field does not suggest its property: %v", properties)
	}
}

// TestRegisterFieldPropertyRejectsAnEmptyValue checks that a registration
// without a name or without a property is an error.
func TestRegisterFieldPropertyRejectsAnEmptyValue(t *testing.T) {
	if err := wikidata.RegisterFieldProperty("", "wdt:P577"); err == nil {
		t.Error("RegisterFieldProperty() error = nil, want an error")
	}

	if err := wikidata.RegisterFieldProperty("published", ""); err == nil {
		t.Error("RegisterFieldProperty() error = nil, want an error")
	}
}
