package pbstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/tools/search"
)

// marshalIndent renders a value as an indented JSON document.
//
// The stdlib encoder is used on purpose: PocketBase v0.40 relies on
// `encoding/json/v2`, which is still behind a GOEXPERIMENT build tag and
// cannot be imported from a regular module.
func marshalIndent(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("pbstore: marshal json: %w", err)
	}

	return string(b), nil
}

// NormalizePage clamps a page number to the valid 1-based range.
func NormalizePage(page int) int {
	if page < 1 {
		return 1
	}

	return page
}

// NormalizePerPage clamps a per-page value to the range accepted by the
// PocketBase REST API (1..[search.MaxPerPage]).
func NormalizePerPage(perPage int) int {
	if perPage <= 0 {
		return search.DefaultPerPage
	}

	if perPage > search.MaxPerPage {
		return search.MaxPerPage
	}

	return perPage
}

// QueryParams converts a [Query] into the native PocketBase URL query
// parameters used by the REST API.
//
// The produced parameter names and semantics are exactly the ones accepted
// by `GET /api/collections/{collection}/records`, which keeps the remote
// backend wire-compatible with the embedded one.
func QueryParams(q Query) url.Values {
	values := url.Values{}

	if q.Filter != "" {
		values.Set(search.FilterQueryParam, q.Filter)
	}

	if q.Sort != "" {
		values.Set(search.SortQueryParam, q.Sort)
	}

	if q.Expand != "" {
		values.Set("expand", q.Expand)
	}

	if q.Fields != "" {
		values.Set("fields", q.Fields)
	}

	if q.SkipTotal {
		values.Set(search.SkipTotalQueryParam, "true")
	}

	values.Set(search.PageQueryParam, strconv.Itoa(NormalizePage(q.Page)))
	values.Set(search.PerPageQueryParam, strconv.Itoa(NormalizePerPage(q.PerPage)))

	return values
}

// SearchURL builds the relative records endpoint for a collection.
//
// The result is meant to be joined to a server base URL, eg.
// `http://127.0.0.1:8090` + `/api/collections/items/records`.
func SearchURL(collection string) string {
	return "/api/collections/" + url.PathEscape(collection) + "/records"
}

// RecordURL builds the relative endpoint of a single record.
func RecordURL(collection, id string) string {
	return SearchURL(collection) + "/" + url.PathEscape(id)
}

// CollectionURL builds the relative endpoint of a single collection.
func CollectionURL(collection string) string {
	return "/api/collections/" + url.PathEscape(collection)
}

// ExpandList splits a comma separated expand expression into its parts.
//
// Empty entries are discarded so that a trailing comma is harmless.
func ExpandList(expand string) []string {
	if strings.TrimSpace(expand) == "" {
		return nil
	}

	parts := strings.Split(expand, ",")

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}

	return out
}

// SyncFilter builds the native filter expression used to pull the records
// that need to be refreshed during an incremental sync.
//
// The expression relies on the PocketBase `updated` field and is combined
// with the user supplied filter using the native `&&` operator.
func SyncFilter(userFilter string, since *string) string {
	clauses := make([]string, 0, 2)

	if s := strings.TrimSpace(userFilter); s != "" {
		clauses = append(clauses, "("+s+")")
	}

	if since != nil && *since != "" {
		clauses = append(clauses, `updated > "`+escapeFilterValue(*since)+`"`)
	}

	if len(clauses) == 0 {
		return ""
	}

	return strings.Join(clauses, " && ")
}

// escapeFilterValue escapes the characters that are special inside a
// double-quoted PocketBase filter literal.
func escapeFilterValue(v string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`"`, `\"`,
	)

	return replacer.Replace(v)
}

// Names of the PocketBase system fields that are not exported as
// constants by the core package.
const (
	// fieldNameCreated is the name of the "created" autodate field.
	fieldNameCreated = "created"
	// fieldNameUpdated is the name of the "updated" autodate field.
	fieldNameUpdated = "updated"
)

// joinErrors merges a map of errors into a single error.
//
// The map values are joined in a deterministic (key sorted) order so that
// the error message is stable.
func joinErrors(errs map[string]error) error {
	if len(errs) == 0 {
		return nil
	}

	keys := make([]string, 0, len(errs))
	for k := range errs {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	messages := make([]string, 0, len(keys))
	for _, k := range keys {
		if err := errs[k]; err != nil {
			messages = append(messages, k+": "+err.Error())
		}
	}

	if len(messages) == 0 {
		return nil
	}

	return errors.New(strings.Join(messages, "; "))
}
