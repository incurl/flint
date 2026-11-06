package wikidata

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// The prefixes of the queries.
//
// They are declared in every query so that the query is self-contained: a
// query that depends on the prefixes of the endpoint is a query that
// answers a different thing on a different endpoint.
const (
	// PrefixWD is the prefix of the entities of Wikidata.
	PrefixWD = "http://www.wikidata.org/entity/"
	// PrefixWDT is the prefix of the direct properties of Wikidata.
	PrefixWDT = "http://www.wikidata.org/prop/direct/"
	// PrefixRDFS is the prefix of the schema vocabulary.
	PrefixRDFS = "http://www.w3.org/2000/01/rdf-schema#"
	// PrefixWikibase is the prefix of the Wikidata service vocabulary.
	PrefixWikibase = "http://wikiba.se/ontology#"
	// PrefixLabel is the property that carries a label.
	PrefixLabel = PrefixRDFS + "label"
)

// Property describes one Wikidata property to ask for.
//
// It is data, not logic: the registry of the property sets is a table, and
// a downstream project adds a property to a table instead of changing the
// query builder.
type Property struct {
	// IRI is the property, in its prefixed form: "wdt:P569".
	//
	// The prefixed form is what goes into the query, and it is what the
	// report names: "wdt:P569" is what an operator reads.
	IRI string
	// Label is a human readable name of the property.
	Label string
	// FullIRI is the absolute form of the property.
	FullIRI string
}

// PropertySet is the set of properties to ask for about an entity type.
//
// It is the unit of extensibility: a new entity type is a new set, and a
// new property is an entry in a set.
type PropertySet struct {
	// EntityType names the set.
	EntityType string
	// Description explains what the set is for.
	Description string
	// Properties are the properties to ask for.
	Properties []Property
}

// The registry of the properties.
//
// It is guarded by a mutex: the tools of a step may run in parallel, and
// a registration may happen while a query is being built.
//
// The registry is the extension point: a downstream project registers its
// own entity types and its own fields with [RegisterPropertySet] and
// [RegisterFieldProperty], and the query builder picks them up.
var (
	// registryMu guards the three tables below.
	registryMu sync.RWMutex
	// propertySets are the known property sets, keyed by entity type.
	propertySets = map[string]PropertySet{
		"person": {
			EntityType:  "person",
			Description: "A human being.",
			Properties: []Property{
				{IRI: "wdt:P31", Label: "instance of", FullIRI: PrefixWDT + "P31"},
				{IRI: "wdt:P569", Label: "date of birth", FullIRI: PrefixWDT + "P569"},
				{IRI: "wdt:P19", Label: "place of birth", FullIRI: PrefixWDT + "P19"},
				{IRI: "wdt:P27", Label: "country of citizenship", FullIRI: PrefixWDT + "P27"},
				{IRI: "wdt:P106", Label: "occupation", FullIRI: PrefixWDT + "P106"},
			},
		},
		"place": {
			EntityType:  "place",
			Description: "A place: a city, a country, a region.",
			Properties: []Property{
				{IRI: "wdt:P17", Label: "country", FullIRI: PrefixWDT + "P17"},
				{IRI: "wdt:P131", Label: "located in the administrative territorial entity", FullIRI: PrefixWDT + "P131"},
				{IRI: "wdt:P31", Label: "instance of", FullIRI: PrefixWDT + "P31"},
				{IRI: "wdt:P1082", Label: "population", FullIRI: PrefixWDT + "P1082"},
			},
		},
		"organisation": {
			EntityType:  "organisation",
			Description: "An organisation: a company, an institution, a team.",
			Properties: []Property{
				{IRI: "wdt:P17", Label: "country", FullIRI: PrefixWDT + "P17"},
				{IRI: "wdt:P571", Label: "inception", FullIRI: PrefixWDT + "P571"},
				{IRI: "wdt:P31", Label: "instance of", FullIRI: PrefixWDT + "P31"},
				{IRI: "wdt:P159", Label: "headquarters location", FullIRI: PrefixWDT + "P159"},
			},
		},
	}
	// fieldProperties maps a field name to the property it suggests.
	//
	// It is the table of the inference: the decision to ask for a
	// property is made from the record at hand, and the table says
	// which property a field name stands for.
	fieldProperties = map[string]string{
		"birth_date":             "wdt:P569",
		"birthdate":              "wdt:P569",
		"date_of_birth":          "wdt:P569",
		"birth_year":             "wdt:P569",
		"birth_place":            "wdt:P19",
		"place_of_birth":         "wdt:P19",
		"country":                "wdt:P17",
		"country_of_citizenship": "wdt:P27",
		"citizenship":            "wdt:P27",
		"nationality":            "wdt:P27",
		"occupation":             "wdt:P106",
		"profession":             "wdt:P106",
		"inception":              "wdt:P571",
		"founded":                "wdt:P571",
		"founding_date":          "wdt:P571",
		"parent_org":             "wdt:P749",
		"location":               "wdt:P131",
		"located_in":             "wdt:P131",
		"population":             "wdt:P1082",
		"headquarters":           "wdt:P159",
		"instance_of":            "wdt:P31",
	}
	// entityTypeNames maps a collection name to an entity type.
	//
	// The name of the collection is the first hint; the fields of the
	// records are the second one. A collection that is named "people"
	// holds people unless its fields say otherwise.
	entityTypeNames = map[string]string{
		"people":        "person",
		"person":        "person",
		"persons":       "person",
		"authors":       "person",
		"artists":       "person",
		"employees":     "person",
		"users":         "person",
		"players":       "person",
		"places":        "place",
		"place":         "place",
		"cities":        "place",
		"countries":     "place",
		"regions":       "place",
		"locations":     "place",
		"organisation":  "organisation",
		"organisations": "organisation",
		"companies":     "organisation",
		"institutions":  "organisation",
		"teams":         "organisation",
	}
)

// labelFields are the field names that carry the name of an entity.
//
// They are tried in order, and the first one that holds a value is the
// name the entity is searched by.
var labelFields = []string{
	"label",
	"title",
	"name",
	"full_name",
	"fullname",
	"display_name",
	"wikidata_label",
}

// qidFields are the field names that carry the Wikidata identifier.
//
// The identifier is data: it is never hardcoded, and a record that holds
// one is asked about directly instead of being searched for by name.
var qidFields = []string{
	"wikidata_id",
	"qid",
	"wikidata",
	"wd_id",
}

// RegisterPropertySet registers a property set under an entity type.
//
// It is the extension point of the package: a downstream project
// registers the properties of an entity type of its own, and the query
// builder picks them up.
//
// A set that is already registered is replaced, and the properties of the
// two are merged: a registration that adds a property to a known entity
// type does not drop the others.
//
// It is safe for concurrent use: the tools of a step may run in parallel.
func RegisterPropertySet(set PropertySet) error {
	name := strings.ToLower(strings.TrimSpace(set.EntityType))
	if name == "" {
		return fmt.Errorf("wikidata: the entity type is required")
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	existing, ok := propertySets[name]
	if !ok {
		existing = PropertySet{EntityType: name, Description: set.Description}
	}

	if set.Description != "" {
		existing.Description = set.Description
	}

	existing.EntityType = name

	// The properties are merged by their IRI: a registration that adds
	// one property does not drop the others.
	seen := make(map[string]bool, len(existing.Properties))

	merged := make([]Property, 0, len(existing.Properties)+len(set.Properties))

	for _, property := range existing.Properties {
		if seen[property.IRI] {
			continue
		}

		seen[property.IRI] = true

		merged = append(merged, property)
	}

	for _, property := range set.Properties {
		if seen[property.IRI] {
			continue
		}

		seen[property.IRI] = true

		merged = append(merged, property)
	}

	existing.Properties = merged

	propertySets[name] = existing

	return nil
}

// RegisterFieldProperty registers the property that a field name
// suggests.
//
// It is the second half of the extension point: a collection that holds a
// field the template does not know about can say what that field is
// worth.
//
// The name is normalised the way the inference normalises it, so
// "first-published" and "first_published" are the same field.
//
// It is safe for concurrent use: the tools of a step may run in parallel.
func RegisterFieldProperty(field, property string) error {
	name := normaliseFieldName(field)
	if name == "" {
		return fmt.Errorf("wikidata: the field name is required")
	}

	iri := strings.TrimSpace(property)
	if iri == "" {
		return fmt.Errorf("wikidata: the property is required")
	}

	registryMu.Lock()
	defer registryMu.Unlock()

	fieldProperties[name] = iri

	return nil
}

// KnownEntityTypes returns the names of the known entity types.
//
// They are sorted, so that a caller that shows them to a user shows them
// in the same order every time.
func KnownEntityTypes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(propertySets))
	for name := range propertySets {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// PropertySetFor returns the property set of an entity type.
//
// An unknown entity type is an error and not a default: a query about the
// wrong thing is worse than a query that failed.
func PropertySetFor(entityType string) (PropertySet, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()

	set, ok := propertySets[strings.ToLower(strings.TrimSpace(entityType))]
	if !ok {
		return PropertySet{}, ErrUnknownEntityType
	}

	return set, nil
}
