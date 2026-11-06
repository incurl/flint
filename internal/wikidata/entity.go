package wikidata

import (
	"strings"
)

// InferEntityType guesses the entity type of a collection.
//
// The name of the collection is the first hint: a collection called
// "people" holds people. The fields of the records are the second one: a
// collection that holds a date of birth holds people whatever it is
// called.
//
// An empty result means that the collection does not look like any of the
// known types.
func InferEntityType(collection string, fields []string) string {
	guess, err := InferEntityTypeFor(collection, fields, "")
	if err != nil {
		return ""
	}

	return guess
}

// InferEntityTypeFor guesses the entity type of a collection, with an
// override.
//
// The override is the answer of the caller: a flag or a configuration
// option that names the type. It wins over the guess, and it is checked:
// a type that is not known is an error and not a silent fallback.
func InferEntityTypeFor(collection string, fields []string, override string) (string, error) {
	if override != "" {
		if _, err := PropertySetFor(override); err != nil {
			return "", err
		}

		return strings.ToLower(strings.TrimSpace(override)), nil
	}

	if guess := guessEntityType(collection, fields); guess != "" {
		return guess, nil
	}

	return "", ErrUnknownEntityType
}

// guessEntityType returns the entity type a collection looks like.
//
// The name is tried first, and the fields are the second hint. A field
// that only a person has — a date of birth — is a stronger hint than the
// name of the collection, because a collection called "records" may well
// hold people.
func guessEntityType(collection string, fields []string) string {
	name := normaliseFieldName(collection)

	registryMu.RLock()

	for alias, entityType := range entityTypeNames {
		if normaliseFieldName(alias) == name {
			registryMu.RUnlock()

			return entityType
		}
	}

	registryMu.RUnlock()

	for _, field := range fields {
		switch normaliseFieldName(field) {
		case "birthdate", "dateofbirth", "birthyear":
			return "person"
		case "occupation", "profession":
			return "person"
		case "population":
			return "place"
		case "inception", "founded", "foundingdate":
			return "organisation"
		}
	}

	return ""
}
