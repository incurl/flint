package wikidata

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The query templates.
//
// The prefixes are declared in every query: a query that depends on the
// prefixes of the endpoint is a query that answers a different thing on a
// different endpoint.
const (
	queryPrefixes = `PREFIX wd: <http://www.wikidata.org/entity/>
PREFIX wdt: <http://www.wikidata.org/prop/direct/>
PREFIX rdfs: <http://www.w3.org/2000/01/rdf-schema#>
PREFIX wikibase: <http://wikiba.se/ontology#>
`
	// querySelect is the projection of every enrichment query.
	//
	// The property and the value are what a fact is, and the label of the
	// value is what makes it readable: "Paris" is a fact, the URI of
	// Paris is not.
	//
	// The entity is projected as well, because it is the join key: a
	// record that is searched for by its name has no identifier of its
	// own, and the one the endpoint resolved is the identifier of the
	// fact.
	querySelect = `SELECT ?entity ?property ?value ?valueLabel WHERE {`
	// queryByQID asks about an entity whose identifier is known.
	queryByQID = `  VALUES ?entity { %s }
  VALUES ?property { %s }
  ?entity ?property ?value .
  OPTIONAL { ?value rdfs:label ?valueLabel }
}`
	// queryByLabel asks about an entity whose name is known.
	//
	// The name is matched against the labels with and without a language
	// tag: Wikidata stores the label of an entity in every language, and
	// the plain literal is the one that is not tagged with any.
	//
	// The subquery picks the entity with the most sitelinks, which is the
	// one the public knows: two entities may share a name, and the
	// popular one is the one the record is about.
	queryByLabel = `  {
    SELECT ?entity WHERE {
      { ?entity rdfs:label %[1]s }
      UNION
      { ?entity rdfs:label %[2]s }
      ?entity wikibase:sitelinks ?sitelinks .
    }
    ORDER BY DESC(?sitelinks)
    LIMIT 1
  }
  VALUES ?property { %[3]s }
  ?entity ?property ?value .
  OPTIONAL { ?value rdfs:label ?valueLabel }
}`
)

// BuildQueryFromPocketBase constructs a SPARQL query from the fields of a
// record.
//
// The entity type selects the properties to ask for; the fields of the
// record select the entity to ask about. A record that holds a Wikidata
// identifier is asked about directly, and a record that only holds a name
// is searched for by that name.
//
// The function performs no I/O and does not change the record.
func BuildQueryFromPocketBase(record *core.Record, entityType string) (string, error) {
	if record == nil {
		return "", fmt.Errorf("%w: no record", ErrUnknownEntityType)
	}

	set, err := PropertySetFor(entityType)
	if err != nil {
		return "", fmt.Errorf("%w: %q", ErrUnknownEntityType, entityType)
	}

	properties := inferProperties(record, set)

	if len(properties) == 0 {
		return "", fmt.Errorf("%w: %q has no properties", ErrUnknownEntityType, entityType)
	}

	values := propertyValues(properties)

	if qid := recordQID(record); qid != "" {
		return queryPrefixes + querySelect + "\n" +
			fmt.Sprintf(queryByQID, "wd:"+qid, values), nil
	}

	label := recordLabel(record)
	if label == "" {
		return "", fmt.Errorf("wikidata: the record holds neither a Wikidata id nor a name")
	}

	return queryPrefixes + querySelect + "\n" +
		fmt.Sprintf(queryByLabel,
			QuoteLiteral(label),
			QuoteLanguageLiteral(label, "en"),
			values), nil
}

// InferProperties suggests the Wikidata properties that fit the fields of a
// record.
//
// It looks at the field names of the record and at its entity type, and
// returns the union of the two: the properties of the entity type, and the
// properties the fields stand for.
//
// A field name that is not in the table contributes nothing, and it does
// not break the inference.
func InferProperties(record *core.Record, entityType string) []Property {
	set, err := PropertySetFor(entityType)
	if err != nil {
		return nil
	}

	return inferProperties(record, set)
}

// inferProperties is the implementation of [InferProperties] for a known
// entity type.
func inferProperties(record *core.Record, set PropertySet) []Property {
	seen := make(map[string]bool, len(set.Properties))
	out := make([]Property, 0, len(set.Properties))

	for _, property := range set.Properties {
		if seen[property.IRI] {
			continue
		}

		seen[property.IRI] = true

		out = append(out, property)
	}

	if record == nil {
		return out
	}

	// The fields of the record decide what else is worth asking for. The
	// decision is made from the record at hand: a collection that holds a
	// birth date is asked for the date of birth, and one that does not
	// is not.
	for _, field := range recordFieldNames(record) {
		iri, ok := fieldProperties[normaliseFieldName(field)]
		if !ok {
			continue
		}

		if seen[iri] {
			continue
		}

		seen[iri] = true

		out = append(out, Property{
			IRI:     iri,
			Label:   propertyLabel(iri),
			FullIRI: PrefixWDT + strings.TrimPrefix(iri, "wdt:"),
		})
	}

	return out
}

// propertyValues renders the properties as a VALUES clause.
//
// The list is sorted so that the same record always produces the same
// query: a query that changes from one run to the next is a query that
// cannot be compared with the one of the run before.
func propertyValues(properties []Property) string {
	iris := make([]string, 0, len(properties))
	for _, property := range properties {
		iris = append(iris, property.IRI)
	}

	sort.Strings(iris)

	return strings.Join(iris, " ")
}

// propertyLabel returns the human readable name of a property.
func propertyLabel(iri string) string {
	for _, set := range propertySets {
		for _, property := range set.Properties {
			if property.IRI == iri {
				return property.Label
			}
		}
	}

	return iri
}

// recordQID returns the Wikidata identifier of a record.
//
// The identifier is data: it is read from the fields of the record, and it
// is never hardcoded.
func recordQID(record *core.Record) string {
	if record == nil {
		return ""
	}

	for _, name := range qidFields {
		value := strings.TrimSpace(record.GetString(name))
		if value == "" {
			continue
		}

		// The identifier may be written in its full form, which is what
		// the templates of the example adapter use.
		if after, ok := strings.CutPrefix(value, PrefixWD); ok {
			return after
		}

		if after, ok := strings.CutPrefix(value, "http://www.wikidata.org/entity/"); ok {
			return after
		}

		return value
	}

	return ""
}

// recordLabel returns the name of a record.
//
// The fields are tried in order, and the first one that holds a value
// wins: a record that holds a "label" is asked about by its label, and
// one that only holds a "name" by its name.
func recordLabel(record *core.Record) string {
	if record == nil {
		return ""
	}

	for _, name := range labelFields {
		value := strings.TrimSpace(record.GetString(name))
		if value != "" {
			return value
		}
	}

	return ""
}

// recordFieldNames returns the names of the fields of a record.
//
// The names of the schema come first, and the custom data comes after: a
// record that holds a field the schema does not declare still carries
// information.
func recordFieldNames(record *core.Record) []string {
	if record == nil {
		return nil
	}

	seen := make(map[string]bool)

	out := make([]string, 0, len(record.Collection().Fields))

	for _, field := range record.Collection().Fields {
		name := field.GetName()
		if seen[name] {
			continue
		}

		seen[name] = true

		out = append(out, name)
	}

	for name := range record.CustomData() {
		if seen[name] {
			continue
		}

		seen[name] = true

		out = append(out, name)
	}

	return out
}

// normaliseFieldName reduces a field name to the form the table is keyed
// by: lower case, and without the separators.
func normaliseFieldName(name string) string {
	var b strings.Builder

	b.Grow(len(name))

	for _, r := range strings.ToLower(name) {
		switch r {
		case '_', '-', ' ', '.':
			continue
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}
