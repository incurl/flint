package wikidata_test

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/tggo/goRDFlib/sparql"

	"github.com/incurl/flint/internal/wikidata"
)

// newRecord builds a record of a collection with the given fields.
//
// It is the mock the query builder is tested with: a real core record of
// a real collection, so that the fields behave the way they behave in the
// application.
func newRecord(t *testing.T, fields map[string]string) *core.Record {
	t.Helper()

	collection := core.NewBaseCollection("people")

	for name := range fields {
		collection.Fields.Add(&core.TextField{Name: name, Max: 500})
	}

	record := core.NewRecord(collection)

	for name, value := range fields {
		record.Set(name, value)
	}

	return record
}

// TestBuildQueryFromPocketBaseAsksForThePropertiesOfTheEntityType checks
// that the entity type drives the properties of the query.
func TestBuildQueryFromPocketBaseAsksForThePropertiesOfTheEntityType(t *testing.T) {
	record := newRecord(t, map[string]string{
		"name":       "Douglas Adams",
		"birth_date": "1956-05-12",
	})

	query, err := wikidata.BuildQueryFromPocketBase(record, "person")
	if err != nil {
		t.Fatalf("BuildQueryFromPocketBase() error = %v", err)
	}

	if !strings.Contains(query, "wdt:P569") {
		t.Errorf("the query does not ask for the date of birth: %s", query)
	}

	if !strings.Contains(query, "wdt:P19") {
		t.Errorf("the query does not ask for the place of birth: %s", query)
	}

	if !strings.Contains(query, "wdt:P27") {
		t.Errorf("the query does not ask for the citizenship: %s", query)
	}
}

// TestBuildQueryFromPocketBaseAsksForTheCountryOfAField checks that a
// field of the record adds its property to the query.
func TestBuildQueryFromPocketBaseAsksForTheCountryOfAField(t *testing.T) {
	record := newRecord(t, map[string]string{
		"name":    "Paris",
		"country": "France",
	})

	query, err := wikidata.BuildQueryFromPocketBase(record, "place")
	if err != nil {
		t.Fatalf("BuildQueryFromPocketBase() error = %v", err)
	}

	if !strings.Contains(query, "wdt:P17") {
		t.Errorf("the query does not ask for the country: %s", query)
	}
}

// TestBuildQueryFromPocketBaseEscapesTheValues is the test of the bug
// that a value holding a quote would end the literal and change the
// meaning of the query.
//
// The value is put in the query as a literal, and a literal that is not
// escaped is a query that is not a query: the parser rejects it, and a
// parser that does not reject it would run a different query.
func TestBuildQueryFromPocketBaseEscapesTheValues(t *testing.T) {
	record := newRecord(t, map[string]string{
		"name":   `O"Brien`,
		"note":   `back\slash`,
		"quoted": "he said \"hi\"",
	})

	query, err := wikidata.BuildQueryFromPocketBase(record, "person")
	if err != nil {
		t.Fatalf("BuildQueryFromPocketBase() error = %v", err)
	}

	if _, err := sparql.Parse(query); err != nil {
		t.Fatalf("the query does not parse: %v\n%s", err, query)
	}

	if !strings.Contains(query, `\"`) {
		t.Errorf("the quote is not escaped: %s", query)
	}
}

// TestBuildQueryFromPocketBaseRejectsAnUnknownEntityType checks that an
// unknown entity type is an error and not a default query.
//
// A query about the wrong thing is worse than a query that failed.
func TestBuildQueryFromPocketBaseRejectsAnUnknownEntityType(t *testing.T) {
	record := newRecord(t, map[string]string{"name": "Douglas Adams"})

	_, err := wikidata.BuildQueryFromPocketBase(record, "spaceship")
	if err == nil {
		t.Fatal("BuildQueryFromPocketBase() error = nil, want an error")
	}
}

// TestBuildQueryFromPocketBaseDoesNotChangeTheRecord checks that the
// builder does not mutate the record it reads.
func TestBuildQueryFromPocketBaseDoesNotChangeTheRecord(t *testing.T) {
	record := newRecord(t, map[string]string{
		"name":    "Douglas Adams",
		"country": "United Kingdom",
	})

	before := record.FieldsData()

	if _, err := wikidata.BuildQueryFromPocketBase(record, "person"); err != nil {
		t.Fatalf("BuildQueryFromPocketBase() error = %v", err)
	}

	after := record.FieldsData()

	if len(before) != len(after) {
		t.Fatalf("the fields changed: %v -> %v", before, after)
	}

	for name, value := range before {
		if after[name] != value {
			t.Errorf("the field %q changed: %v -> %v", name, value, after[name])
		}
	}
}

// TestBuildQueryFromPocketBaseUsesTheIdentifierOfTheRecord checks that
// the identifier of the record is used when it holds one.
//
// The identifier is data: a record that holds one is asked about
// directly, and it is never hardcoded.
func TestBuildQueryFromPocketBaseUsesTheIdentifierOfTheRecord(t *testing.T) {
	record := newRecord(t, map[string]string{
		"name":        "Douglas Adams",
		"wikidata_id": "Q42",
	})

	query, err := wikidata.BuildQueryFromPocketBase(record, "person")
	if err != nil {
		t.Fatalf("BuildQueryFromPocketBase() error = %v", err)
	}

	if !strings.Contains(query, "wd:Q42") {
		t.Errorf("the query does not use the identifier of the record: %s", query)
	}

	if _, err := sparql.Parse(query); err != nil {
		t.Fatalf("the query does not parse: %v\n%s", err, query)
	}
}

// TestBuildQueryFromPocketBaseSearchesByName checks that a record
// without an identifier is searched for by its name.
func TestBuildQueryFromPocketBaseSearchesByName(t *testing.T) {
	record := newRecord(t, map[string]string{
		"name": "Douglas Adams",
	})

	query, err := wikidata.BuildQueryFromPocketBase(record, "person")
	if err != nil {
		t.Fatalf("BuildQueryFromPocketBase() error = %v", err)
	}

	if !strings.Contains(query, `"Douglas Adams"`) {
		t.Errorf("the query does not search for the name: %s", query)
	}

	if strings.Contains(query, "wd:Q") {
		t.Errorf("the query invents an identifier: %s", query)
	}

	if _, err := sparql.Parse(query); err != nil {
		t.Fatalf("the query does not parse: %v\n%s", err, query)
	}
}

// TestInferProperties checks that the fields of a record decide what else
// is worth asking for.
func TestInferProperties(t *testing.T) {
	record := newRecord(t, map[string]string{
		"name":       "Douglas Adams",
		"birth_date": "1956-05-12",
		"country":    "United Kingdom",
		"unknown":    "ignored",
	})

	properties := wikidata.InferProperties(record, "person")

	iris := map[string]bool{}
	for _, property := range properties {
		iris[property.IRI] = true
	}

	if !iris["wdt:P569"] {
		t.Errorf("the birth date does not suggest the date of birth: %v", iris)
	}

	if !iris["wdt:P17"] {
		t.Errorf("the country does not suggest the country: %v", iris)
	}

	if !iris["wdt:P27"] {
		t.Errorf("the properties of the entity type are missing: %v", iris)
	}
}

// TestInferPropertiesIgnoresAnUnknownEntityType checks that an unknown
// entity type contributes nothing and does not fail.
func TestInferPropertiesIgnoresAnUnknownEntityType(t *testing.T) {
	record := newRecord(t, map[string]string{"name": "Douglas Adams"})

	if properties := wikidata.InferProperties(record, "spaceship"); properties != nil {
		t.Errorf("InferProperties() = %v, want nil", properties)
	}
}

// TestQuoteLiteral checks the escaping of a literal.
func TestQuoteLiteral(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "plain", value: "Douglas Adams", want: `"Douglas Adams"`},
		{name: "quote", value: `O"Brien`, want: `"O\"Brien"`},
		{name: "backslash", value: `a\b`, want: `"a\\b"`},
		{name: "newline", value: "a\nb", want: `"a\nb"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := wikidata.QuoteLiteral(test.value)

			if got != test.want {
				t.Errorf("QuoteLiteral(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

// TestQuoteLiteralProducesAParsableQuery checks that an escaped literal
// is a literal the parser accepts.
func TestQuoteLiteralProducesAParsableQuery(t *testing.T) {
	values := []string{
		`O"Brien`,
		`back\slash`,
		"line\nbreak",
		`both " and \`,
	}

	for _, value := range values {
		query := `SELECT ?x WHERE { ?x <http://example.org/p> ` + wikidata.QuoteLiteral(value) + ` . }`

		if _, err := sparql.Parse(query); err != nil {
			t.Errorf("the query with %q does not parse: %v", value, err)
		}
	}
}
