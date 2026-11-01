package wikidata

import (
	"context"
	"errors"
	"fmt"

	"github.com/incurl/flint/internal/pbstore"
)

// ErrUnknownLabel is returned when a QID is not in the cache.
var ErrUnknownLabel = errors.New("wikidata: unknown label")

// LabelStore resolves the labels from the local cache.
//
// The cache is a plain PocketBase collection queried through the
// [pbstore.Store] interface: there is no parallel Go map.
type LabelStore struct {
	// store is the data layer.
	store pbstore.Store
	// language is the preferred label language.
	language string
}

// NewLabelStore creates a label store.
func NewLabelStore(store pbstore.Store, language string) *LabelStore {
	if language == "" {
		language = "en"
	}

	return &LabelStore{store: store, language: language}
}

// Label returns the label of a QID.
func (l *LabelStore) Label(ctx context.Context, qid string) (string, error) {
	rec, err := l.store.FindFirst(ctx, LabelsCollection, fmt.Sprintf(
		"qid = %q && language = %q", qid, l.language,
	))
	if err != nil {
		if errors.Is(err, pbstore.ErrNotFound) {
			return "", fmt.Errorf("%w: %s", ErrUnknownLabel, qid)
		}

		return "", err
	}

	return rec.StringField("label"), nil
}

// Sync refreshes the label cache from the remote server.
//
// It is an explicit operation: the TUI triggers it from the sync page.
func (l *LabelStore) Sync(ctx context.Context, batchSize int) (*pbstore.SyncReport, error) {
	return l.store.SyncCollection(ctx, pbstore.SyncRequest{
		RemoteCollection: LabelsCollection,
		LocalCollection:  LabelsCollection,
		BatchSize:        batchSize,
	})
}
