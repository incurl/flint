package pbstore_test

import (
	"net/url"
	"testing"

	"github.com/pocketbase/pocketbase/tools/search"

	"github.com/incurl/flint/internal/pbstore"
)

// TestQueryParams verifies that a [pbstore.Query] is translated into the
// native PocketBase URL query parameters.
func TestQueryParams(t *testing.T) {
	tests := []struct {
		name  string
		query pbstore.Query
		want  url.Values
	}{
		{
			name:  "empty query",
			query: pbstore.Query{Collection: "items"},
			want: url.Values{
				"page":    []string{"1"},
				"perPage": []string{"30"},
			},
		},
		{
			name: "full query",
			query: pbstore.Query{
				Collection: "items",
				Filter:     `key = "a"`,
				Sort:       "-created",
				Page:       3,
				PerPage:    10,
				Expand:     "author",
				SkipTotal:  true,
			},
			want: url.Values{
				"filter":    []string{`key = "a"`},
				"sort":      []string{"-created"},
				"page":      []string{"3"},
				"perPage":   []string{"10"},
				"expand":    []string{"author"},
				"skipTotal": []string{"true"},
			},
		},
		{
			name: "out of range values are normalised",
			query: pbstore.Query{
				Collection: "items",
				Page:       -5,
				PerPage:    search.MaxPerPage + 100,
			},
			want: url.Values{
				"page":    []string{"1"},
				"perPage": []string{"1000"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pbstore.QueryParams(tt.query)
			if got.Encode() != tt.want.Encode() {
				t.Errorf("QueryParams() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestNormalizePage verifies the page normalisation.
func TestNormalizePage(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{in: -1, want: 1},
		{in: 0, want: 1},
		{in: 1, want: 1},
		{in: 7, want: 7},
	}
	for _, tt := range tests {
		if got := pbstore.NormalizePage(tt.in); got != tt.want {
			t.Errorf("NormalizePage(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestNormalizePerPage verifies the per-page normalisation.
func TestNormalizePerPage(t *testing.T) {
	tests := []struct {
		in   int
		want int
	}{
		{in: -1, want: search.DefaultPerPage},
		{in: 0, want: search.DefaultPerPage},
		{in: 50, want: 50},
		{in: search.MaxPerPage + 1, want: search.MaxPerPage},
	}
	for _, tt := range tests {
		if got := pbstore.NormalizePerPage(tt.in); got != tt.want {
			t.Errorf("NormalizePerPage(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// TestSortFields verifies that the native PocketBase sort syntax is used.
func TestSortFields(t *testing.T) {
	fields := pbstore.SortFields("-created,name")
	if len(fields) != 2 {
		t.Fatalf("SortFields() returned %d fields, want 2", len(fields))
	}

	if fields[0].Name != "created" || fields[0].Direction != search.SortDesc {
		t.Errorf("first field = %+v, want created DESC", fields[0])
	}

	if fields[1].Name != "name" || fields[1].Direction != search.SortAsc {
		t.Errorf("second field = %+v, want name ASC", fields[1])
	}

	if got := pbstore.SortFields(""); got != nil {
		t.Errorf("SortFields(\"\") = %v, want nil", got)
	}
}

// TestFilterExpr verifies that a filter is compiled into a native
// PocketBase WHERE expression.
func TestFilterExpr(t *testing.T) {
	expr, err := pbstore.FilterExpr(`key = "a"`, search.NewSimpleFieldResolver("key"))
	if err != nil {
		t.Fatalf("FilterExpr() error = %v", err)
	}

	if expr == nil {
		t.Fatal("FilterExpr() returned a nil expression")
	}

	// An empty filter is not an error.
	expr, err = pbstore.FilterExpr("", nil)
	if err != nil {
		t.Fatalf("FilterExpr(\"\") error = %v", err)
	}

	if expr != nil {
		t.Error("FilterExpr(\"\") returned a non nil expression")
	}

	// An invalid filter is reported as an error.
	_, err = pbstore.FilterExpr(`key = `, search.NewSimpleFieldResolver("key"))
	if err == nil {
		t.Error("FilterExpr() with an invalid filter returned no error")
	}
}

// TestSyncFilter verifies the incremental sync filter construction.
func TestSyncFilter(t *testing.T) {
	since := "2024-01-02 03:04:05.000Z"

	tests := []struct {
		name   string
		filter string
		since  *string
		want   string
	}{
		{
			name: "empty",
			want: "",
		},
		{
			name:   "user filter only",
			filter: `key = "a"`,
			want:   `(key = "a")`,
		},
		{
			name:  "since only",
			since: &since,
			want:  `updated > "2024-01-02 03:04:05.000Z"`,
		},
		{
			name:   "both",
			filter: `key = "a"`,
			since:  &since,
			want:   `(key = "a") && updated > "2024-01-02 03:04:05.000Z"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pbstore.SyncFilter(tt.filter, tt.since); got != tt.want {
				t.Errorf("SyncFilter() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestExpandList verifies the expand expression parsing.
func TestExpandList(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{in: "", want: nil},
		{in: "  ", want: nil},
		{in: "author", want: []string{"author"}},
		{in: "author,editor", want: []string{"author", "editor"}},
		{in: "author, editor ,", want: []string{"author", "editor"}},
	}
	for _, tt := range tests {
		got := pbstore.ExpandList(tt.in)
		if len(got) != len(tt.want) {
			t.Fatalf("ExpandList(%q) = %v, want %v", tt.in, got, tt.want)
		}

		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("ExpandList(%q) = %v, want %v", tt.in, got, tt.want)
			}
		}
	}
}

// TestSearchURL verifies the native PocketBase endpoint construction.
func TestSearchURL(t *testing.T) {
	if got := pbstore.SearchURL("items"); got != "/api/collections/items/records" {
		t.Errorf("SearchURL() = %q", got)
	}

	if got := pbstore.RecordURL("items", "abc"); got != "/api/collections/items/records/abc" {
		t.Errorf("RecordURL() = %q", got)
	}

	if got := pbstore.CollectionURL("items"); got != "/api/collections/items" {
		t.Errorf("CollectionURL() = %q", got)
	}
}
