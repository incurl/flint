// Package wikidata contains a complete example of a domain adapter built
// on top of the Flint core packages.
//
// It demonstrates the four extension points of the template:
//
//   - a custom collection migration;
//   - a custom record renderer;
//   - a label lookup cache stored in a PocketBase collection;
//   - a custom filter transform.
//
// None of the core packages (pbstore, app, theme, components) is
// modified by this adapter.
package wikidata

import (
	"github.com/pocketbase/pocketbase/core"
)

// Collection names used by the adapter.
const (
	// ItemsCollection holds the Wikidata items.
	ItemsCollection = "wd_items"
	// LabelsCollection holds the label lookup cache.
	LabelsCollection = "wd_labels"
)

// Migrations returns the collections required by the Wikidata adapter.
//
// Register them in cmd/tui/main.go:
//
//	appMigrations() []core.Migration { return wikidata.Migrations() }
func Migrations() []core.Migration {
	return []core.Migration{
		{
			File: "1700000001_create_wikidata.go",
			Up: func(txApp core.App) error {
				items := core.NewBaseCollection(ItemsCollection)
				items.Fields.Add(
					&core.TextField{Name: "qid", Required: true, Max: 32},
					&core.JSONField{Name: "labels"},
					&core.JSONField{Name: "descriptions"},
					&core.JSONField{Name: "claims"},
				)
				if err := txApp.Save(items); err != nil {
					return err
				}

				labels := core.NewBaseCollection(LabelsCollection)
				labels.Fields.Add(
					&core.TextField{Name: "qid", Required: true, Max: 32},
					&core.TextField{Name: "language", Required: true, Max: 16},
					&core.TextField{Name: "label", Max: 500},
				)
				return txApp.Save(labels)
			},
			Down: func(txApp core.App) error {
				return nil
			},
		},
	}
}
