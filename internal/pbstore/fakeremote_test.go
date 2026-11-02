package pbstore_test

import (
	"context"

	"github.com/incurl/flint/internal/pbstore"
)

// fakeRemoteStore is a [pbstore.Store] that behaves like the remote
// backend: it cannot create collections.
//
// It is used to check that the code paths that depend on the backend
// behave correctly without a server.
type fakeRemoteStore struct {
	pbstore.Store
}

// Mode reports the remote mode.
func (f *fakeRemoteStore) Mode() pbstore.Mode { return pbstore.ModeRemote }

// EnsureDictionaryCollection always fails, like the remote backend.
func (f *fakeRemoteStore) EnsureDictionaryCollection(_ context.Context, _ string, _ []pbstore.DictionaryField) (pbstore.Collection, error) {
	return pbstore.Collection{}, pbstore.ErrPreferencesUnavailable
}

// FindFirst returns the not found error, like the remote backend does
// for a missing collection.
func (f *fakeRemoteStore) FindFirst(_ context.Context, _, _ string) (*pbstore.Record, error) {
	return nil, pbstore.ErrNotFound
}
