package wikidata

import (
	"fmt"
	"strings"
)

// BuildFilter translates a human query into a native PocketBase filter.
//
// A query such as "Q42" or "human instance of" is turned into a filter
// that PocketBase parses. The output is always a valid native filter, so
// the embedded and the remote backends behave identically.
func BuildFilter(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}

	// A QID is an exact match.
	if strings.HasPrefix(strings.ToUpper(query), "Q") {
		return fmt.Sprintf("qid = %q", strings.ToUpper(query))
	}

	// Otherwise search the labels.
	return fmt.Sprintf(`label ~ %q`, query)
}
