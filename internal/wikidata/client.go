// Package wikidata enriches PocketBase records with the facts of
// Wikidata.
//
// The package does three things: it asks a SPARQL endpoint about the
// entities of a collection, it writes what it learns into a PocketBase
// collection, and it reports what the join between the two holds.
//
// The data layer is reached through [pbstore.Store] and never through the
// PocketBase app itself: the store is the only thing that knows whether the
// data is local or remote, and a tool that branches on the backend is a
// tool that works on one of them.
//
// The client talks to a SPARQL endpoint over HTTP. Every query is bounded
// twice — by a deadline and by a budget of solutions — because a deadline
// bounds the time and not the memory.
package wikidata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/tggo/goRDFlib/graph"
	"github.com/tggo/goRDFlib/sparql"
	"github.com/tggo/goRDFlib/store/sparqlstore"
)

// Errors returned by the package.
//
// The sentinels are what a caller branches on: a query that ran out of
// budget is not the same failure as a query that timed out, and a caller
// that retries the second one must not retry the first one.
var (
	// ErrTooManySolutions is returned when a query builds more solutions
	// than the budget allows.
	//
	// The result is not a partial answer: the query failed.
	ErrTooManySolutions = errors.New("wikidata: the solution budget was spent")
	// ErrTimeout is returned when a query does not finish in time.
	ErrTimeout = errors.New("wikidata: the query timed out")
	// ErrUnknownEntityType is returned when the entity type of a
	// collection is not one of the known ones.
	ErrUnknownEntityType = errors.New("wikidata: unknown entity type")
	// ErrNoEndpoint is returned when the client has no endpoint to talk
	// to.
	ErrNoEndpoint = errors.New("wikidata: no SPARQL endpoint")
)

// The defaults of the client.
//
// They are the values of the public endpoint of Wikidata: it asks for a
// delay between the queries, and a query that runs for a minute is a query
// that answers nobody.
const (
	// DefaultDelay is the pause between two queries.
	DefaultDelay = 100 * time.Millisecond
	// DefaultTimeout bounds a single query.
	DefaultTimeout = 30 * time.Second
	// DefaultMaxSolutions is the budget of solutions of a single query.
	DefaultMaxSolutions = 100000
	// DefaultEndpoint is the public SPARQL endpoint of Wikidata.
	DefaultEndpoint = "https://query.wikidata.org/sparql"
)

// Client queries a SPARQL endpoint.
//
// A client is safe for concurrent use: the model library may run the tools
// of a single step in parallel.
type Client struct {
	// endpoint is the URL of the SPARQL endpoint.
	endpoint string
	// store is the remote triple store the queries are run against.
	store *sparqlstore.SPARQLStore
	// graph is the graph the engine evaluates the queries against.
	graph *graph.Graph
	// delay is the pause between two queries.
	delay time.Duration
	// timeout bounds a single query.
	timeout time.Duration
	// maxSolutions is the budget of solutions of a single query.
	maxSolutions int
	// logger receives the decisions and the failures.
	logger *slog.Logger
	// mu serialises the queries, so that the delay between them holds.
	mu sync.Mutex
	// lastQuery is the moment of the last query that started.
	lastQuery time.Time
}

// Option configures a [Client].
type Option func(*Client)

// WithDelay sets the pause between two queries.
//
// A negative delay is ignored: a pause cannot be negative, and a negative
// one would mean "no pause at all".
func WithDelay(d time.Duration) Option {
	return func(c *Client) {
		if d >= 0 {
			c.delay = d
		}
	}
}

// WithTimeout sets the deadline of a single query.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.timeout = d
		}
	}
}

// WithMaxSolutions sets the budget of solutions of a single query.
//
// The budget is what bounds the memory of a query: a deadline bounds the
// time, and a query that builds a million solutions in ten seconds is
// still a million solutions in memory.
func WithMaxSolutions(n int) Option {
	return func(c *Client) {
		if n > 0 {
			c.maxSolutions = n
		}
	}
}

// WithLogger sets the logger of the client.
//
// A logger that is nil is replaced by one that discards everything: the
// client never writes to the standard output, because a library that
// prints is a library that corrupts the output of the application.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) {
		if l != nil {
			c.logger = l
		}
	}
}

// WithHTTPClient sets the HTTP client of the remote store.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc == nil {
			return
		}

		c.store = sparqlstore.New(c.endpoint, sparqlstore.WithHTTPClient(hc))
		c.graph = graph.NewGraph(graph.WithStore(c.store))
	}
}

// NewClient creates a client for the given endpoint URL.
//
// An empty endpoint falls back to the public endpoint of Wikidata.
func NewClient(endpoint string, opts ...Option) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}

	client := &Client{
		endpoint:     endpoint,
		delay:        DefaultDelay,
		timeout:      DefaultTimeout,
		maxSolutions: DefaultMaxSolutions,
		logger:       slog.New(slog.DiscardHandler),
	}

	for _, opt := range opts {
		opt(client)
	}

	client.store = sparqlstore.New(endpoint, sparqlstore.WithTimeout(client.timeout))
	client.graph = graph.NewGraph(graph.WithStore(client.store))

	return client
}

// Endpoint returns the URL of the SPARQL endpoint.
func (c *Client) Endpoint() string {
	return c.endpoint
}

// Query executes a SPARQL query and returns the result graph.
//
// It is the CONSTRUCT-shaped answer: a graph holds triples, and a SELECT
// is answered by [Client.QueryRows], which returns the variable names and
// the rows of the result.
//
// A query that is not a CONSTRUCT produces a graph without triples.
func (c *Client) Query(ctx context.Context, query string) (*graph.Graph, error) {
	result, err := c.QueryRows(ctx, query)
	if err != nil {
		return nil, err
	}

	if result.Graph == nil {
		return graph.NewGraph(), nil
	}

	return result.Graph, nil
}

// QueryRows executes a SELECT and returns its bindings and its variable
// names.
//
// The query is bounded twice: by a deadline, which bounds the time, and by
// a budget of solutions, which bounds the memory. A query that spends the
// budget fails with [ErrTooManySolutions] — it is not a partial answer.
func (c *Client) QueryRows(ctx context.Context, query string) (*sparql.Result, error) {
	if c == nil || c.store == nil {
		return nil, ErrNoEndpoint
	}

	c.wait(ctx)

	// The budget and the deadline are set on the context, because the
	// engine reads them from there.
	ctx = sparql.WithMaxSolutions(ctx, c.maxSolutions)

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	result, err := sparql.QueryContext(ctx, c.graph, query)
	if err != nil {
		return nil, c.wrap(err)
	}

	return result, nil
}

// wait holds the client back so that the endpoint is not flooded.
//
// The pause is taken before the query, and it is measured from the start
// of the previous one: two queries that are sent back to back are the
// thing the endpoint asks the client not to do.
func (c *Client) wait(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.delay <= 0 {
		c.lastQuery = time.Now()

		return
	}

	if !c.lastQuery.IsZero() {
		elapsed := time.Since(c.lastQuery)
		if elapsed < c.delay {
			timer := time.NewTimer(c.delay - elapsed)
			defer timer.Stop()

			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		}
	}

	c.lastQuery = time.Now()
}

// wrap turns the error of the engine into one of the sentinels.
//
// The two failures are told apart because they need different answers: a
// timeout is worth another try, and a spent budget is worth a smaller
// query.
func (c *Client) wrap(err error) error {
	switch {
	case errors.Is(err, sparql.ErrTooManySolutions):
		return fmt.Errorf("%w: %w", ErrTooManySolutions, err)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", ErrTimeout, err)
	default:
		return fmt.Errorf("wikidata: the query failed: %w", err)
	}
}

// LogQuery records a query and its outcome.
//
// The log is what an operator reads when a run did not enrich anything:
// the query, the number of rows and the error say where it stopped.
func (c *Client) LogQuery(ctx context.Context, query string, rows int, err error) {
	if c == nil || c.logger == nil {
		return
	}

	if err != nil {
		c.logger.ErrorContext(ctx, "the query failed", "query", query, "error", err)

		return
	}

	c.logger.InfoContext(ctx, "the query ran", "query", query, "rows", rows)
}
