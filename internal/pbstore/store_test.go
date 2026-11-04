package pbstore_test

import (
	"testing"

	"github.com/incurl/flint/internal/pbstore"
)

// TestIsSystemCollection verifies the names that are internal to
// PocketBase or to the application.
//
// The predicate is a property of the name: the remote backend does not
// report the system flag of a collection, and the template keeps its own
// book-keeping behind the same prefix as PocketBase does.
func TestIsSystemCollection(t *testing.T) {
	cases := map[string]bool{
		"_superusers":                 true,
		"_mfas":                       true,
		"_otps":                       true,
		"_externalAuths":              true,
		"_authOrigins":                true,
		pbstore.PreferencesCollection: true,
		"_flint_sessions":             true,
		"_flint_messages":             true,
		"vocab":                       false,
		"users":                       false,
		"":                            false,
	}

	for name, want := range cases {
		if got := pbstore.IsSystemCollection(name); got != want {
			t.Errorf("IsSystemCollection(%q) = %t, want %t", name, got, want)
		}
	}
}
