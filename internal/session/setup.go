package session

import (
	"context"
	"fmt"

	"github.com/pocketbase/pocketbase/core"

	"github.com/incurl/flint/internal/pbstore"
)

// EnsureCollections creates the collections that hold the conversations.
//
// The call is idempotent, so it is safe to run on every start. The
// remote backend cannot create a collection: there, the server holds
// the collections and the application only reads and writes them.
//
// A missing collection is not fatal. The agent is an optional part of
// the template, and an application that cannot store its conversations
// still runs: the agent simply does not remember them.
func EnsureCollections(ctx context.Context, store pbstore.Store) error {
	if store == nil {
		return nil
	}

	if _, err := store.EnsureCollection(ctx, SessionsCollection, []pbstore.CollectionField{
		{Name: FieldTitle, Type: pbstore.FieldTypeText, Max: 500},
		// The pointer to the message that summarises the conversation.
		{Name: FieldSummary, Type: pbstore.FieldTypeText, Max: 30},
		{Name: "created", Type: pbstore.FieldTypeAutodate},
		{Name: "updated", Type: pbstore.FieldTypeAutodate},
	}); err != nil {
		return fmt.Errorf("session: ensure the sessions collection: %w", err)
	}

	if _, err := store.EnsureCollection(ctx, MessagesCollection, []pbstore.CollectionField{
		{Name: FieldSessionID, Type: pbstore.FieldTypeText, Required: true, Max: 30},
		{Name: FieldRole, Type: pbstore.FieldTypeText, Required: true, Max: 30},
		{Name: FieldTurn, Type: pbstore.FieldTypeNumber},
		{Name: FieldContent, Type: pbstore.FieldTypeJSON},
		{Name: "created", Type: pbstore.FieldTypeAutodate},
	}); err != nil {
		return fmt.Errorf("session: ensure the messages collection: %w", err)
	}

	return nil
}

// Migrations returns the migrations that create the collections of the
// conversations.
//
// The embedded backend creates them on demand, through
// [EnsureCollections]. A remote backend cannot: the collections live on
// the server, and a migration creates them there.
//
// A downstream project that runs against a remote server adds these
// migrations to its own migration list.
func Migrations() []core.Migration {
	return []core.Migration{
		{
			File: "1730000000_create_flint_sessions.go",
			Up:   createCollections,
			Down: func(txApp core.App) error {
				for _, name := range []string{MessagesCollection, SessionsCollection} {
					collection, err := txApp.FindCollectionByNameOrId(name)
					if err != nil {
						// The collection is already gone: the
						// migration is idempotent.
						continue
					}

					if err := txApp.Delete(collection); err != nil {
						return err
					}
				}

				return nil
			},
		},
	}
}

// createCollections creates the collections of the conversations.
func createCollections(txApp core.App) error {
	sessions := core.NewBaseCollection(SessionsCollection)
	sessions.Fields.Add(
		&core.TextField{Name: FieldTitle, Max: 500},
		// The pointer to the message that summarises the conversation.
		&core.TextField{Name: FieldSummary, Max: 30},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)

	messages := core.NewBaseCollection(MessagesCollection)
	messages.Fields.Add(
		&core.TextField{Name: FieldSessionID, Required: true, Max: 30},
		&core.TextField{Name: FieldRole, Required: true, Max: 30},
		&core.NumberField{Name: FieldTurn},
		&core.JSONField{Name: FieldContent, MaxSize: 100000},
		&core.AutodateField{Name: "created", OnCreate: true},
	)

	for _, collection := range []*core.Collection{sessions, messages} {
		if err := txApp.Save(collection); err != nil {
			return err
		}
	}

	return nil
}
