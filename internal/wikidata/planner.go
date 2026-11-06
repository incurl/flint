package wikidata

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/incurl/flint/internal/pbstore"
)

// Action is what the planner decided to do.
type Action string

const (
	// ActionCreate creates the enriched collection and enriches it.
	ActionCreate Action = "create"
	// ActionEnrich enriches a collection that holds no facts yet.
	ActionEnrich Action = "enrich"
	// ActionRefresh enriches a collection again, because the last run is
	// older than the refresh interval.
	ActionRefresh Action = "refresh"
	// ActionStop does nothing: there is nothing to do.
	ActionStop Action = "stop"
)

// String implements [fmt.Stringer].
func (a Action) String() string { return string(a) }

// State is what the planner reads to decide.
//
// It is the state of the store, and nothing else: the planner does not
// query the endpoint and it does not write.
type State struct {
	// TargetCollection is the collection to enrich.
	TargetCollection string
	// EnrichedCollection is the collection that holds the facts.
	EnrichedCollection string
	// EnrichedExists reports whether the enriched collection is there.
	EnrichedExists bool
	// EnrichedRecords is the number of records it holds.
	EnrichedRecords int
	// LastRun is the moment of the last enrichment, if there was one.
	LastRun time.Time
	// RefreshInterval is the age after which a populated collection is
	// enriched again.
	RefreshInterval time.Duration
}

// NeedsEnrichment reports whether the state calls for an enrichment.
func (s State) NeedsEnrichment() bool {
	switch {
	case !s.EnrichedExists:
		return true
	case s.EnrichedRecords == 0:
		return true
	case s.RefreshInterval > 0 && !s.LastRun.IsZero() && time.Since(s.LastRun) > s.RefreshInterval:
		return true
	default:
		return false
	}
}

// Planner decides the next action.
//
// The planner decides and it does not follow a script: the rules are the
// ones of [State.NeedsEnrichment], and the state is what it reads.
type Planner interface {
	// Plan inspects the state and returns the action to take.
	Plan(ctx context.Context, state State) (Action, error)
}

// StorePlanner plans the enrichment of a collection by looking at the
// store.
type StorePlanner struct {
	// store is the data layer.
	store pbstore.Store
	// logger receives the decisions.
	logger *slog.Logger
}

// NewStorePlanner returns a planner that reads the state from the store.
func NewStorePlanner(store pbstore.Store, logger *slog.Logger) *StorePlanner {
	return &StorePlanner{store: store, logger: logger}
}

// Plan implements [Planner].
func (p *StorePlanner) Plan(ctx context.Context, state State) (Action, error) {
	if p == nil || p.store == nil {
		return ActionStop, fmt.Errorf("wikidata: no store configured")
	}

	action := decideAction(state)

	if p.logger != nil {
		p.logger.InfoContext(ctx, "the planner decided",
			"action", action.String(),
			"collection", state.TargetCollection,
			"enriched_records", state.EnrichedRecords,
			"last_run", state.LastRun,
		)
	}

	return action, nil
}

// decideAction maps a state to an action.
//
// It is a pure function, so that the decision can be tested without a
// store.
func decideAction(state State) Action {
	switch {
	case !state.EnrichedExists:
		return ActionCreate
	case state.EnrichedRecords == 0:
		return ActionEnrich
	case state.RefreshInterval > 0 && !state.LastRun.IsZero() && time.Since(state.LastRun) > state.RefreshInterval:
		return ActionRefresh
	default:
		return ActionStop
	}
}

// ReadState reads the state of the enrichment of a collection.
//
// The state is read through the store, so it is the state of the backend
// that is in use.
func ReadState(ctx context.Context, store pbstore.Store, target string, refreshInterval time.Duration) (State, error) {
	state := State{
		TargetCollection:   target,
		EnrichedCollection: EnrichedCollectionName(target),
		RefreshInterval:    refreshInterval,
	}

	if store == nil {
		return state, fmt.Errorf("wikidata: no store configured")
	}

	collections, err := store.Collections(ctx)
	if err != nil {
		return state, fmt.Errorf("wikidata: read the collections: %w", err)
	}

	for _, collection := range collections {
		if collection.Name == state.EnrichedCollection {
			state.EnrichedExists = true

			break
		}
	}

	if !state.EnrichedExists {
		return state, nil
	}

	count, err := store.Count(ctx, state.EnrichedCollection, "")
	if err != nil {
		return state, fmt.Errorf("wikidata: count the records of %q: %w", state.EnrichedCollection, err)
	}

	state.EnrichedRecords = count

	lastRun, err := lastRunTime(ctx, store)
	if err != nil {
		// The log is a convenience: a run that cannot read it still
		// enriches.
		return state, nil //nolint:nilerr // the log is not the state.
	}

	state.LastRun = lastRun

	return state, nil
}

// lastRunTime returns the moment of the last enrichment.
//
// It is read from the agent log, and it is the moment of the last run
// that succeeded.
func lastRunTime(ctx context.Context, store pbstore.Store) (time.Time, error) {
	records, err := store.List(ctx, pbstore.Query{
		Collection: LogCollectionName,
		Page:       1,
		PerPage:    1,
		Sort:       "-started_at",
	})
	if err != nil {
		return time.Time{}, err
	}

	if len(records.Records) == 0 {
		return time.Time{}, nil
	}

	value := strings.TrimSpace(records.Records[0].StringField("started_at"))
	if value == "" {
		return time.Time{}, nil
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("wikidata: parse the run timestamp: %w", err)
	}

	return parsed, nil
}
