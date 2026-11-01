package pbstore_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase"

	"github.com/incurl/flint/internal/pbstore"
)

// newHybridStore creates a hybrid store over an embedded backend with a
// JSON field and a few records in it.
func newHybridStore(t *testing.T, maxLocalRecords int) *pbstore.Hybrid {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "pb_data")
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	inner, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}

	store := pbstore.NewHybrid(inner, pbstore.HybridConfig{MaxLocalRecords: maxLocalRecords})

	ctx := context.Background()
	if _, err := store.EnsureDictionaryCollection(ctx, "jf", []pbstore.DictionaryField{
		{Name: "meta", Type: pbstore.FieldTypeJSON},
	}); err != nil {
		t.Fatalf("EnsureDictionaryCollection() error = %v", err)
	}

	rows := []map[string]any{
		{"key": "r1", "meta": map[string]any{
			"lang": "go", "level": 3, "tags": []any{"cli", "fast"},
			"items": []any{map[string]any{"name": "alpha", "qty": 1}},
		}},
		{"key": "r2", "meta": map[string]any{
			"lang": "rust", "level": 2, "tags": []any{"sys"},
			"items": []any{map[string]any{"name": "beta", "qty": 7}},
		}},
		{"key": "r3", "meta": map[string]any{
			"level": 5, "tags": []any{"cli"},
		}},
	}

	for _, row := range rows {
		if _, err := store.Create(ctx, "jf", row); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	return store
}

func TestHybridNativeFilterGoesToTheDatabase(t *testing.T) {
	t.Parallel()

	store := newHybridStore(t, 0)
	defer func() { _ = store.Close() }()

	// A native filter is evaluated by the database.
	page, err := store.List(context.Background(), pbstore.Query{
		Collection: "jf",
		Filter:     `meta.lang = "go"`,
		Page:       1,
		PerPage:    10,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(page.Records) != 1 {
		t.Errorf("got %d records, want 1", len(page.Records))
	}

	if page.Truncated {
		t.Error("Truncated = true for a native filter, want false")
	}
}

func TestHybridLocalFilterReachesIntoArrays(t *testing.T) {
	t.Parallel()

	store := newHybridStore(t, 0)
	defer func() { _ = store.Close() }()

	tests := []struct {
		name  string
		expr  string
		count int
	}{
		{"array contains", `meta.tags.#(=="cli")`, 2},
		{"array index", `meta.items.0.name = "alpha"`, 1},
		{"numeric across elements", `meta.items.#.qty > 5`, 1},
		{"array contains no match", `meta.tags.#(=="nope")`, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The subtests share the store, so they must not run in
			// parallel: the parent closes it when it returns.
			page, err := store.List(context.Background(), pbstore.Query{
				Collection: "jf",
				Filter:     tt.expr,
				Page:       1,
				PerPage:    10,
			})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}

			if len(page.Records) != tt.count {
				t.Errorf("got %d records, want %d", len(page.Records), tt.count)
			}

			if page.TotalItems != tt.count {
				t.Errorf("TotalItems = %d, want %d", page.TotalItems, tt.count)
			}
		})
	}
}

func TestHybridLocalFilterPaginates(t *testing.T) {
	t.Parallel()

	store := newHybridStore(t, 0)
	defer func() { _ = store.Close() }()

	// Two records match. The first page holds one.
	page, err := store.List(context.Background(), pbstore.Query{
		Collection: "jf",
		Filter:     `meta.tags.#(=="cli")`,
		Page:       1,
		PerPage:    1,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(page.Records) != 1 {
		t.Errorf("got %d records on the page, want 1", len(page.Records))
	}

	if page.TotalItems != 2 {
		t.Errorf("TotalItems = %d, want 2", page.TotalItems)
	}

	if page.TotalPages != 2 {
		t.Errorf("TotalPages = %d, want 2", page.TotalPages)
	}

	// The second page holds the other one.
	page2, err := store.List(context.Background(), pbstore.Query{
		Collection: "jf",
		Filter:     `meta.tags.#(=="cli")`,
		Page:       2,
		PerPage:    1,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(page2.Records) != 1 {
		t.Errorf("got %d records on the second page, want 1", len(page2.Records))
	}

	if page2.Records[0].ID == page.Records[0].ID {
		t.Error("the second page holds the same record as the first")
	}
}

func TestHybridLocalFilterIsBounded(t *testing.T) {
	t.Parallel()

	// The bound is smaller than the number of records, so the result is
	// truncated and reported as such.
	//
	// The filter must be one that needs the local evaluation: a native
	// filter is evaluated by the database and is never bounded.
	store := newHybridStore(t, 2)
	defer func() { _ = store.Close() }()

	page, err := store.List(context.Background(), pbstore.Query{
		Collection: "jf",
		Filter:     `meta.tags.#(=="cli")`,
		Page:       1,
		PerPage:    10,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if !page.Truncated {
		t.Error("Truncated = false, want true when the bound is reached")
	}

	if len(page.Records) > 2 {
		t.Errorf("got %d records, want at most 2", len(page.Records))
	}
}

func TestHybridCount(t *testing.T) {
	t.Parallel()

	store := newHybridStore(t, 0)
	defer func() { _ = store.Close() }()

	// A native filter.
	count, err := store.Count(context.Background(), "jf", `meta.lang = "go"`)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if count != 1 {
		t.Errorf("got %d, want 1", count)
	}

	// A local filter.
	count, err = store.Count(context.Background(), "jf", `meta.tags.#(=="cli")`)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}

	if count != 2 {
		t.Errorf("got %d, want 2", count)
	}
}

func TestHybridFindFirst(t *testing.T) {
	t.Parallel()

	store := newHybridStore(t, 0)
	defer func() { _ = store.Close() }()

	record, err := store.FindFirst(context.Background(), "jf", `meta.tags.#(=="sys")`)
	if err != nil {
		t.Fatalf("FindFirst() error = %v", err)
	}

	if record == nil {
		t.Fatal("FindFirst() returned no record")
	}

	// A filter that matches nothing reports the not found error.
	_, err = store.FindFirst(context.Background(), "jf", `meta.tags.#(=="nope")`)
	if err == nil {
		t.Error("FindFirst() returned no error for a missing record")
	}
}

func TestHybridInvalidLocalFilter(t *testing.T) {
	t.Parallel()

	store := newHybridStore(t, 0)
	defer func() { _ = store.Close() }()

	_, err := store.List(context.Background(), pbstore.Query{
		Collection: "jf",
		Filter:     `meta.tags.#(=="cli"`,
		Page:       1,
		PerPage:    10,
	})
	if err == nil {
		t.Fatal("List() returned no error for a malformed filter")
	}
}

func TestHybridModeIsPreserved(t *testing.T) {
	t.Parallel()

	store := newHybridStore(t, 0)
	defer func() { _ = store.Close() }()

	if store.Mode() != pbstore.ModeEmbedded {
		t.Errorf("Mode() = %q, want %q", store.Mode(), pbstore.ModeEmbedded)
	}
}

func TestHybridDelegatesTheWrites(t *testing.T) {
	t.Parallel()

	store := newHybridStore(t, 0)
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	created, err := store.Create(ctx, "jf", map[string]any{
		"key":  "r4",
		"meta": map[string]any{"lang": "go", "tags": []any{"new"}},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	fetched, err := store.Get(ctx, "jf", created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if fetched.ID != created.ID {
		t.Errorf("Get() returned %q, want %q", fetched.ID, created.ID)
	}

	if _, err := store.Update(ctx, "jf", created.ID, map[string]any{
		"label": "updated",
	}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	if err := store.Delete(ctx, "jf", created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

// TestHybridAgreesWithTheNativePath checks that the hybrid returns the
// same records as the wrapped store for the expressions that both can
// evaluate.
//
// The hybrid routes an expression to the local evaluation only when the
// native grammar cannot handle it, so the two paths must never disagree
// on an expression they both understand.
func TestHybridAgreesWithTheNativePath(t *testing.T) {
	t.Parallel()

	store := newHybridStore(t, 0)
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	expressions := []string{
		`meta.lang = "go"`,
		`meta.lang != "go"`,
		`meta.level > 2`,
		`meta.level >= 3`,
		`meta.level < 5`,
		`meta.level <= 3`,
		`meta.level = 3`,
		`meta.lang ~ "o"`,
		`meta.lang = "go" && meta.level > 2`,
		`meta.lang = "go" || meta.level > 9`,
	}

	for _, expr := range expressions {
		t.Run(expr, func(t *testing.T) {
			// The native path, through the unwrapped store.
			native, err := store.Unwrap().List(ctx, pbstore.Query{
				Collection: "jf",
				Filter:     expr,
				Page:       1,
				PerPage:    50,
			})
			if err != nil {
				t.Fatalf("native List() error = %v", err)
			}

			// The hybrid path.
			hybrid, err := store.List(ctx, pbstore.Query{
				Collection: "jf",
				Filter:     expr,
				Page:       1,
				PerPage:    50,
			})
			if err != nil {
				t.Fatalf("hybrid List() error = %v", err)
			}

			if len(hybrid.Records) != len(native.Records) {
				t.Fatalf("hybrid returned %d records, native returned %d",
					len(hybrid.Records), len(native.Records))
			}

			if hybrid.TotalItems != native.TotalItems {
				t.Errorf("hybrid TotalItems = %d, native = %d",
					hybrid.TotalItems, native.TotalItems)
			}

			for i := range native.Records {
				if hybrid.Records[i].ID != native.Records[i].ID {
					t.Errorf("record %d: hybrid = %q, native = %q",
						i, hybrid.Records[i].ID, native.Records[i].ID)
				}
			}
		})
	}
}
