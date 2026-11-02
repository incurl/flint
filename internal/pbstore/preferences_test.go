package pbstore_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/pocketbase/pocketbase"

	"github.com/incurl/flint/internal/pbstore"
)

// newPreferencesStore creates an embedded store for the preferences
// tests.
func newPreferencesStore(t *testing.T) *pbstore.Embedded {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "pb_data")
	app := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir:  dir,
		HideStartBanner: true,
	})

	store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{DataDir: dir})
	if err != nil {
		t.Fatalf("OpenEmbedded() error = %v", err)
	}

	return store
}

func TestLoadPreferencesOnAnEmptyStore(t *testing.T) {
	t.Parallel()

	store := newPreferencesStore(t)
	defer func() { _ = store.Close() }()

	// No preferences were stored yet: the defaults apply and it is not
	// an error.
	prefs, err := pbstore.LoadPreferences(context.Background(), store)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}

	if prefs != (pbstore.Preferences{}) {
		t.Errorf("prefs = %+v, want the zero value", prefs)
	}
}

func TestSaveAndLoadPreferences(t *testing.T) {
	t.Parallel()

	store := newPreferencesStore(t)
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	want := pbstore.Preferences{Theme: "light", Scale: 120, PerPage: 50}
	if err := pbstore.SavePreferences(ctx, store, want); err != nil {
		t.Fatalf("SavePreferences() error = %v", err)
	}

	got, err := pbstore.LoadPreferences(ctx, store)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}

	if got != want {
		t.Errorf("prefs = %+v, want %+v", got, want)
	}
}

func TestSavePreferencesIsIdempotent(t *testing.T) {
	t.Parallel()

	store := newPreferencesStore(t)
	defer func() { _ = store.Close() }()

	ctx := context.Background()

	if err := pbstore.SavePreferences(ctx, store, pbstore.Preferences{
		Theme: "light", Scale: 120, PerPage: 50,
	}); err != nil {
		t.Fatalf("SavePreferences() error = %v", err)
	}

	// The second save updates the record instead of creating a second
	// one.
	if err := pbstore.SavePreferences(ctx, store, pbstore.Preferences{
		Theme: "dark", Scale: 100, PerPage: 30,
	}); err != nil {
		t.Fatalf("SavePreferences() error = %v", err)
	}

	page, err := store.List(ctx, pbstore.Query{
		Collection: pbstore.PreferencesCollection,
		Page:       1,
		PerPage:    10,
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if len(page.Records) != 1 {
		t.Fatalf("the collection holds %d records, want 1", len(page.Records))
	}

	got, err := pbstore.LoadPreferences(ctx, store)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}

	if got.Theme != "dark" || got.Scale != 100 || got.PerPage != 30 {
		t.Errorf("prefs = %+v, want the updated values", got)
	}
}

func TestPreferencesSurviveAReopen(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "pb_data")

	open := func() *pbstore.Embedded {
		app := pocketbase.NewWithConfig(pocketbase.Config{
			DefaultDataDir:  dir,
			HideStartBanner: true,
		})

		store, err := pbstore.OpenEmbedded(app, pbstore.EmbeddedConfig{DataDir: dir})
		if err != nil {
			t.Fatalf("OpenEmbedded() error = %v", err)
		}

		return store
	}

	ctx := context.Background()

	first := open()
	if err := pbstore.SavePreferences(ctx, first, pbstore.Preferences{
		Theme: "light", Scale: 130, PerPage: 45,
	}); err != nil {
		t.Fatalf("SavePreferences() error = %v", err)
	}

	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// The preferences must be there after a restart.
	second := open()
	defer func() { _ = second.Close() }()

	got, err := pbstore.LoadPreferences(ctx, second)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}

	if got.Theme != "light" || got.Scale != 130 || got.PerPage != 45 {
		t.Errorf("prefs = %+v, want the stored values", got)
	}
}

func TestSavePreferencesOnTheRemoteBackend(t *testing.T) {
	t.Parallel()

	// The remote backend cannot create a collection, so the preferences
	// have nowhere to live.
	remote := &fakeRemoteStore{}

	err := pbstore.SavePreferences(context.Background(), remote, pbstore.Preferences{})
	if err == nil {
		t.Fatal("SavePreferences() returned no error on the remote backend")
	}

	if !errors.Is(err, pbstore.ErrPreferencesUnavailable) {
		t.Errorf("error = %v, want ErrPreferencesUnavailable", err)
	}
}

func TestLoadPreferencesOnTheRemoteBackend(t *testing.T) {
	t.Parallel()

	// The remote backend returns the defaults: the preferences live
	// with the local data.
	remote := &fakeRemoteStore{}

	prefs, err := pbstore.LoadPreferences(context.Background(), remote)
	if err != nil {
		t.Fatalf("LoadPreferences() error = %v", err)
	}

	if prefs != (pbstore.Preferences{}) {
		t.Errorf("prefs = %+v, want the zero value", prefs)
	}
}
