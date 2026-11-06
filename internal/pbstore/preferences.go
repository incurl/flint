package pbstore

import (
	"context"
	"errors"
	"fmt"
)

// PreferencesCollection is the name of the collection holding the user
// preferences.
//
// The collection holds a single record: the preferences of the current
// user. A single record keeps the read simple and avoids the need for a
// user identity, which the template does not have.
const PreferencesCollection = "_flint_preferences"

// ErrPreferencesUnavailable is returned when the preferences cannot be
// persisted.
//
// It happens when the remote backend is used: the remote store cannot
// create a collection, so the preferences have nowhere to live.
var ErrPreferencesUnavailable = errors.New("pbstore: the preferences are not available on this backend")

// Preferences holds the user preferences that survive a restart.
//
// The deployment configuration — the mode, the data directory, the
// remote URL — stays in the TOML file, because it describes where the
// application runs. The preferences describe how the application looks
// to the person using it, so they belong with the data.
type Preferences struct {
	// Theme is the name of the theme variant: "dark" or "light".
	Theme string
	// Scale is the UI scale, as a percentage.
	Scale int
	// PerPage is the page size of the record list.
	//
	// A value of 0 means the deployment default.
	PerPage int
	// Compact renders the record list without the blank separator
	// lines.
	Compact bool
}

// LoadPreferences reads the preferences of the current user.
//
// It returns the zero preferences when none were stored yet, which is
// not an error: the defaults apply.
//
// The remote backend cannot hold the preferences, so it always returns
// the defaults.
func LoadPreferences(ctx context.Context, store Store) (Preferences, error) {
	if store == nil {
		return Preferences{}, nil
	}

	record, err := store.FindFirst(ctx, PreferencesCollection, "")
	if err != nil {
		// The collection does not exist yet, or the record does not:
		// the preferences were never stored. That is not an error.
		if errors.Is(err, ErrNotFound) {
			return Preferences{}, nil
		}

		return Preferences{}, err
	}

	if record == nil {
		return Preferences{}, nil
	}

	prefs := Preferences{}

	if value, ok := record.Field("theme"); ok {
		prefs.Theme = fmt.Sprintf("%v", value)
	}

	if value, ok := record.Field("scale"); ok {
		prefs.Scale = toInt(value)
	}

	if value, ok := record.Field("per_page"); ok {
		prefs.PerPage = toInt(value)
	}

	if value, ok := record.Field("compact"); ok {
		prefs.Compact = toBool(value)
	}

	return prefs, nil
}

// SavePreferences stores the preferences of the current user.
//
// It is idempotent: the preferences are written to a single record that
// is updated in place.
//
// The remote backend cannot hold the preferences, so it returns
// [ErrPreferencesUnavailable].
func SavePreferences(ctx context.Context, store Store, prefs Preferences) error {
	if store == nil {
		return nil
	}

	if store.Mode() == ModeRemote {
		return ErrPreferencesUnavailable
	}

	if _, err := store.EnsureDictionaryCollection(ctx, PreferencesCollection, []DictionaryField{
		{Name: "theme", Type: FieldTypeText},
		{Name: "scale", Type: FieldTypeNumber},
		{Name: "per_page", Type: FieldTypeNumber},
		{Name: "compact", Type: FieldTypeBool},
	}); err != nil {
		return fmt.Errorf("pbstore: ensure the preferences collection: %w", err)
	}

	existing, err := store.FindFirst(ctx, PreferencesCollection, "")
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("pbstore: find the preferences: %w", err)
	}

	data := map[string]any{
		"key":      "preferences",
		"label":    "User preferences",
		"theme":    prefs.Theme,
		"scale":    prefs.Scale,
		"per_page": prefs.PerPage,
		"compact":  prefs.Compact,
	}

	if existing != nil && existing.ID != "" {
		if _, err := store.Update(ctx, PreferencesCollection, existing.ID, data); err != nil {
			return fmt.Errorf("pbstore: update the preferences: %w", err)
		}

		return nil
	}

	if _, err := store.Create(ctx, PreferencesCollection, data); err != nil {
		return fmt.Errorf("pbstore: create the preferences: %w", err)
	}

	return nil
}

// toBool converts a field value to a bool.
//
// The values arrive from the database as an any, and their concrete type
// depends on the backend.
func toBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int32:
		return v != 0
	case int64:
		return v != 0
	case float32:
		return v != 0
	case float64:
		return v != 0
	case string:
		return v == "true" || v == "1"
	default:
		return false
	}
}

// toInt converts a field value to an int.
func toInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
