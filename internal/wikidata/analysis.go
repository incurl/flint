package wikidata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/incurl/flint/internal/pbstore"
)

// The defaults of the analysis.
//
// The threshold is the confidence a fuzzy match needs before it is
// counted as a fact. A guess is not a fact, and a report that counts the
// guesses as facts is a report that lies.
const (
	// DefaultConfidenceThreshold is the confidence a fuzzy match needs.
	DefaultConfidenceThreshold = 0.8
	// DefaultAnalysisLimit is the number of records the analysis reads
	// from each side of the join.
	DefaultAnalysisLimit = 500
)

// AnalysisReport describes what the enrichment achieved.
//
// It is the answer of the join between a collection and the collection
// that holds the facts of Wikidata.
type AnalysisReport struct {
	// OriginalCollection is the collection that was enriched.
	OriginalCollection string `json:"original_collection"`
	// EnrichedCollection is the collection that holds the facts.
	EnrichedCollection string `json:"enriched_collection"`
	// OriginalRecords is the number of records of the original
	// collection that were read.
	OriginalRecords int `json:"original_records"`
	// EnrichedRecords is the number of records of the enriched
	// collection.
	EnrichedRecords int `json:"enriched_records"`
	// MatchedRecords is the number of records that were matched and
	// that hold at least one property.
	MatchedRecords int `json:"matched_records"`
	// UnmatchedRecords is the number of records that were not matched.
	UnmatchedRecords int `json:"unmatched_records"`
	// ByProperty counts the facts per property: "wdt:P569" -> 12.
	ByProperty map[string]int `json:"by_property"`
	// FuzzyMatches is the number of records that were matched by name
	// and not by identifier.
	FuzzyMatches int `json:"fuzzy_matches"`
	// ConfidenceThreshold is the confidence a fuzzy match needed.
	ConfidenceThreshold float64 `json:"confidence_threshold"`
}

// AnalyzeWithWikidata joins the original collection with its wikidata_
// collection and reports what was enriched.
//
// The join is exact when the original collection holds a Wikidata
// identifier, and fuzzy when it does not: a record is then matched against
// the labels of the facts by its name.
//
// A record that was matched but that holds no property at all is not
// counted as enriched: a match without a fact is not an enrichment.
func AnalyzeWithWikidata(ctx context.Context, store pbstore.Store, originalCollectionName string) (*AnalysisReport, error) {
	return AnalyzeWithWikidataConfig(ctx, store, originalCollectionName, AnalysisOptions{})
}

// AnalysisOptions are the settings of the analysis.
type AnalysisOptions struct {
	// Threshold is the confidence a fuzzy match needs.
	//
	// Zero uses [DefaultConfidenceThreshold].
	Threshold float64
	// Limit is the number of records read from each side of the join.
	//
	// Zero uses [DefaultAnalysisLimit].
	Limit int
	// Logger receives the report.
	Logger *slog.Logger
}

// AnalyzeWithWikidataConfig joins the original collection with its
// wikidata_ collection, with the given settings.
func AnalyzeWithWikidataConfig(
	ctx context.Context,
	store pbstore.Store,
	originalCollectionName string,
	opts AnalysisOptions,
) (*AnalysisReport, error) {
	if store == nil {
		return nil, fmt.Errorf("wikidata: no store configured")
	}

	if originalCollectionName == "" {
		return nil, fmt.Errorf("wikidata: the collection name is required")
	}

	threshold := opts.Threshold
	if threshold <= 0 {
		threshold = DefaultConfidenceThreshold
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultAnalysisLimit
	}

	enrichedName := EnrichedCollectionName(originalCollectionName)

	report := &AnalysisReport{
		OriginalCollection:  originalCollectionName,
		EnrichedCollection:  enrichedName,
		ByProperty:          map[string]int{},
		ConfidenceThreshold: threshold,
	}

	originals, err := store.List(ctx, pbstore.Query{
		Collection: originalCollectionName,
		Page:       1,
		PerPage:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("wikidata: read the collection %q: %w", originalCollectionName, err)
	}

	report.OriginalRecords = len(originals.Records)

	enriched, err := store.List(ctx, pbstore.Query{
		Collection: enrichedName,
		Page:       1,
		PerPage:    limit,
	})
	if err != nil {
		// A collection that does not exist is not a failure: it is a
		// collection that was never enriched. The report says so,
		// and the model can enrich it and ask again.
		if isMissingCollection(err) {
			logReport(ctx, opts.Logger, report)

			return report, nil
		}

		return nil, fmt.Errorf("wikidata: read the collection %q: %w", enrichedName, err)
	}

	report.EnrichedRecords = len(enriched.Records)

	// The facts are indexed by the identifier of the entity they belong
	// to, and by their label, so that the join does not scan the whole
	// collection for every record.
	index := buildFactIndex(enriched.Records)

	for _, record := range originals.Records {
		match, ok := index.match(record, threshold)
		if !ok {
			report.UnmatchedRecords++

			continue
		}

		if match.fuzzy {
			report.FuzzyMatches++
		}

		if len(match.properties) == 0 {
			// A match without a fact is not an enrichment.
			report.UnmatchedRecords++

			continue
		}

		report.MatchedRecords++

		for property := range match.properties {
			report.ByProperty[property]++
		}
	}

	logReport(ctx, opts.Logger, report)

	return report, nil
}

// isMissingCollection reports whether an error means that the
// collection is not there.
//
// The two backends do not report a missing collection the same way: the
// embedded one fails to resolve the collection, and the remote one gets
// an empty list from the server. The error of the embedded one is a
// wrapped one, so the check is on the message.
func isMissingCollection(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, pbstore.ErrNotFound) {
		return true
	}

	message := err.Error()

	return strings.Contains(message, "resolve collection") ||
		strings.Contains(message, "no rows in result set")
}

// EnrichedCollectionName returns the name of the collection that holds
// the facts of a collection.
//
// The name is derived, so that the join does not need a table: the facts
// of "people" live in "wikidata_people".
func EnrichedCollectionName(collection string) string {
	return "wikidata_" + collection
}

// factIndex holds the facts of the enriched collection, indexed for the
// join.
type factIndex struct {
	// byQID holds the facts per Wikidata identifier.
	byQID map[string]map[string]bool
	// labels holds the labels of the entities and the facts they hold.
	//
	// The list holds every label: a fuzzy match looks at all of them,
	// and a map keyed by the exact label would only ever find the
	// labels that are equal to the name of the record.
	labels []labelFact
}

// labelFact is the label of an entity and the properties it holds.
type labelFact struct {
	// label is the name of the entity.
	label string
	// properties are the properties the entity holds.
	properties map[string]bool
}

// buildFactIndex indexes the records of the enriched collection.
func buildFactIndex(records []*pbstore.Record) *factIndex {
	index := &factIndex{
		byQID:  map[string]map[string]bool{},
		labels: make([]labelFact, 0, len(records)),
	}

	for _, record := range records {
		if record == nil {
			continue
		}

		qid := strings.TrimSpace(record.StringField("wikidata_id"))
		property := strings.TrimSpace(record.StringField("property"))
		label := strings.TrimSpace(record.StringField("label"))

		if property == "" {
			continue
		}

		if qid != "" {
			if index.byQID[qid] == nil {
				index.byQID[qid] = map[string]bool{}
			}

			index.byQID[qid][property] = true
		}

		if label != "" {
			index.labels = append(index.labels, labelFact{
				label:      label,
				properties: map[string]bool{property: true},
			})
		}
	}

	return index
}

// match is the result of a join.
type match struct {
	// properties are the properties the matched entity holds.
	properties map[string]bool
	// fuzzy reports whether the match was made by name.
	fuzzy bool
}

// match finds the facts of a record.
//
// The identifier is tried first, because it is exact. The name is the
// second attempt, and it is only counted when it is confident enough.
func (i *factIndex) match(record *pbstore.Record, threshold float64) (match, bool) {
	if record == nil {
		return match{}, false
	}

	if qid := recordQIDFromFields(record); qid != "" {
		if properties, ok := i.byQID[qid]; ok {
			return match{properties: properties}, true
		}
	}

	name := recordNameFromFields(record)
	if name == "" {
		return match{}, false
	}

	// The candidates are the labels that begin with the same letter:
	// a label that does not begin with it cannot be a close enough
	// match, and looking at all of them would be a scan of the whole
	// collection for every record.
	candidates := i.candidates(name)
	if len(candidates) == 0 {
		return match{}, false
	}

	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		names = append(names, candidate.label)
	}

	best, confidence := bestMatch(name, names)
	if best == "" || confidence < threshold {
		return match{}, false
	}

	for _, candidate := range candidates {
		if candidate.label == best {
			return match{properties: candidate.properties, fuzzy: true}, true
		}
	}

	return match{}, false
}

// candidates returns the labels that begin with the same letter as the
// name.
//
// The first letter is the cheap filter: a label that does not begin with
// it cannot hold the whole name, and the confidence of the match would be
// below the threshold anyway.
func (i *factIndex) candidates(name string) []labelFact {
	normalised := normaliseFieldName(name)
	if normalised == "" {
		return nil
	}

	first := normalised[0]

	out := make([]labelFact, 0, 8)

	for _, candidate := range i.labels {
		if normaliseFieldName(candidate.label)[0] != first {
			continue
		}

		out = append(out, candidate)
	}

	return out
}

// bestMatch returns the candidate that matches the name best, and the
// confidence of the match.
//
// The score of the fuzzy library is not a confidence: it is an unbounded
// number that grows with the length of the name and with the length of
// the run of the matching characters. It is divided by the score of the
// name against itself, so that the result is a ratio between zero and one
// that says how much of the name the candidate holds.
//
// A name that matches itself exactly has the score of one, and a
// candidate that holds every character of the name scores close to it.
func bestMatch(name string, candidates []string) (string, float64) {
	self := fuzzy.Find(name, []string{name})
	if len(self) == 0 || self[0].Score <= 0 {
		return "", 0
	}

	found := fuzzy.Find(name, candidates)
	if len(found) == 0 {
		return "", 0
	}

	best := found[0]

	return best.Str, float64(best.Score) / float64(self[0].Score)
}

// recordQIDFromFields returns the Wikidata identifier of a record.
func recordQIDFromFields(record *pbstore.Record) string {
	for _, name := range qidFields {
		value := strings.TrimSpace(record.StringField(name))
		if value == "" {
			continue
		}

		if after, ok := strings.CutPrefix(value, PrefixWD); ok {
			return after
		}

		if after, ok := strings.CutPrefix(value, "http://www.wikidata.org/entity/"); ok {
			return after
		}

		return value
	}

	return ""
}

// recordNameFromFields returns the name of a record.
func recordNameFromFields(record *pbstore.Record) string {
	for _, name := range labelFields {
		value := strings.TrimSpace(record.StringField(name))
		if value != "" {
			return value
		}
	}

	return ""
}

// logReport writes the report as JSON.
//
// The report is written as JSON so that a downstream consumer can parse
// the line: a report that only a human can read is a report that a
// pipeline cannot use.
func logReport(ctx context.Context, logger *slog.Logger, report *AnalysisReport) {
	if logger == nil {
		return
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		logger.ErrorContext(ctx, "the report cannot be encoded", "error", err)

		return
	}

	logger.InfoContext(ctx, "the analysis ran", "report", string(encoded))
}
