package code

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// The timeouts of the language server.
const (
	// startTimeout bounds the start of the server and its
	// initialization.
	//
	// gopls indexes the workspace when it starts, and a large workspace
	// takes a while. The bound is generous on purpose: a server that
	// cannot start in a minute will not start at all.
	startTimeout = 60 * time.Second

	// requestTimeout bounds a single request to the server.
	requestTimeout = 30 * time.Second

	// unavailableRetryDelay is the delay before the missing server is
	// looked for again.
	//
	// The server is looked up on the PATH, and the PATH does not change
	// while the application runs: a missing server is missing until the
	// user installs it and restarts. Retrying every call would make
	// every call pay for the search.
	unavailableRetryDelay = 30 * time.Second
)

// serverCommand is the executable of the language server.
//
// It is a variable so that a test can point it at a fake one.
var serverCommand = "gopls"

// Manager owns the language server and answers the questions about the
// code.
//
// The server is started on the first question and kept until the
// manager is closed. One server serves the whole directory: gopls
// indexes the workspace once.
//
// A Manager is safe for concurrent use.
type Manager struct {
	// root is the directory the manager is allowed to read.
	root string
	// logger receives the diagnostics of the server.
	logger *slog.Logger

	// mu guards the client and the state below.
	mu sync.Mutex
	// startMu guards the start of the server.
	//
	// The start is slow, and it happens outside of mu, so that a
	// concurrent Close is not blocked behind it. This mutex is what
	// keeps two callers from starting two servers.
	startMu sync.Mutex
	// client is the language server, nil until it is started.
	client *client
	// diagnostics holds the problems the server found.
	diagnostics *diagnostics
	// unavailableUntil is the time before which the server is not
	// looked for again.
	unavailableUntil time.Time
	// closed reports whether the manager was closed.
	closed bool
}

// NewManager returns a manager that reads the code under root.
//
// The root is the boundary: a path outside it is refused, and the
// server is started with it as its workspace root. An empty root is
// refused: a manager without a boundary would answer questions about
// the whole filesystem.
func NewManager(root string, logger *slog.Logger) (*Manager, error) {
	if root == "" {
		return nil, errors.New("code: the code directory is required")
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("code: the code directory %q: %w", root, err)
	}

	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{root: abs, logger: logger, diagnostics: newDiagnostics()}, nil
}

// Root returns the directory the manager reads.
func (m *Manager) Root() string {
	return m.root
}

// Close stops the language server.
//
// It is safe to call it more than once, and it is safe to call it while
// a request is in flight: the request fails, and the server is gone.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.closed = true

	if m.client == nil {
		return
	}

	// The server is a child process: it is killed rather than asked to
	// leave, because a server that ignores the request would outlive
	// the application.
	m.client.Kill()
	m.client = nil
}

// server returns the language server, starting it when needed.
//
// It is safe for concurrent use: the tools of a single step may run in
// parallel, and two of them may ask for the server at the same time.
// Only one of them starts it, and the others wait for it.
//
// The start is slow -- gopls indexes the workspace -- and it happens
// outside the lock, so that a concurrent Close is not blocked behind
// it for a minute. The lock is taken again to publish the client.
func (m *Manager) server(ctx context.Context) (*client, error) {
	m.mu.Lock()
	closed := m.closed
	client := m.client
	m.mu.Unlock()

	if closed {
		return nil, errors.New("code: the manager is closed")
	}

	if client != nil {
		return client, nil
	}

	return m.startServer(ctx)
}

// startServer starts the language server, once.
//
// The callers that arrive at the same time wait for the one that does
// the work: the start is guarded by a mutex of its own, so that only
// one of them starts a server and the others find it running.
func (m *Manager) startServer(ctx context.Context) (*client, error) {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	// The server may have been started by another caller while this
	// one waited for the lock.
	m.mu.Lock()
	closed := m.closed
	client := m.client
	m.mu.Unlock()

	if closed {
		return nil, errors.New("code: the manager is closed")
	}

	if client != nil {
		return client, nil
	}

	if time.Now().Before(m.unavailableUntil) {
		return nil, fmt.Errorf("%w: %s is not on the PATH", ErrServerMissing, serverCommand)
	}

	path, err := exec.LookPath(serverCommand)
	if err != nil {
		m.markUnavailable()

		return nil, fmt.Errorf("%w: %s is not on the PATH", ErrServerMissing, serverCommand)
	}

	c, err := newClient(path, m.root, m.logger)
	if err != nil {
		m.markUnavailable()

		return nil, err
	}

	startCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	// The diagnostics are subscribed before the handshake: a server
	// may publish while it starts, and a notification that arrives
	// before the subscription is lost.
	err = c.Initialize(startCtx, m.publishDiagnostics) //nolint:misspell
	if err != nil {
		c.Kill()

		m.markUnavailable()

		return nil, err
	}

	m.logger.Info("the language server is running", "server", serverCommand, "root", m.root)

	// The client is published under the lock, and the manager may have
	// been closed while the server was starting: a server that nobody
	// asked for any more is killed instead of being left running.
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		c.Kill()

		return nil, errors.New("code: the manager is closed")
	}

	m.client = c

	return c, nil
}

// markUnavailable records that the server is not to be looked for
// again for a while.
//
// A server that is missing stays missing until the user installs it
// and restarts, and retrying every call would make every call pay for
// the search.
func (m *Manager) markUnavailable() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.unavailableUntil = time.Now().Add(unavailableRetryDelay)
}

// publishDiagnostics stores the problems the server found.
//
// It is the callback of the client: the server publishes them as a
// notification, and the cache holds the last set for every file.
func (m *Manager) publishDiagnostics(uri string, items []protocol.Diagnostic) {
	path, err := protocol.DocumentURI(uri).Path()
	if err != nil {
		return
	}

	converted := make([]Diagnostic, 0, len(items))

	for _, item := range items {
		converted = append(converted, diagnosticFrom(item, path))
	}

	m.diagnostics.publish(path, converted)
}

// resolve turns a path given by the model into an absolute path inside
// the root.
//
// The path may be relative to the root, or absolute. A path that
// escapes the root — through a ".." or through a symbolic link — is
// refused: the root is the boundary, and the model does not get to
// move it.
func (m *Manager) resolve(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("code: the path is required")
	}

	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(m.root, candidate)
	}

	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("code: the path %q: %w", path, err)
	}

	// The check is lexical: a symbolic link inside the root that points
	// outside of it is not followed. The server is rooted at the same
	// directory, so it cannot read it either.
	if !withinRoot(m.root, abs) {
		return "", fmt.Errorf("%w: %s", ErrOutsideRoot, path)
	}

	return abs, nil
}

// withinRoot reports whether path is root or is under it.
func withinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	if rel == "." {
		return true
	}

	// A path outside the root has a ".." at the start of the relative
	// path.
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Symbols returns the declarations of a file.
//
// The file does not have to exist in the editor: the server reads it
// from the disk.
func (m *Manager) Symbols(ctx context.Context, path string) ([]Symbol, error) {
	abs, err := m.resolve(path)
	if err != nil {
		return nil, err
	}

	srv, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	results, err := srv.DocumentSymbols(reqCtx, abs)
	if err != nil {
		return nil, fmt.Errorf("code: the symbols of %s: %w", path, err)
	}

	symbols := make([]Symbol, 0, len(results))

	for _, result := range results {
		symbols = append(symbols, symbolFromResult(result))
	}

	return symbols, nil
}

// Diagnostics returns the problems the server found in a file.
//
// The server publishes them when it finishes analysing the file, so
// the answer is what the server knows now: a file it has not looked
// at yet has no problems, and a file it has looked at has the ones
// it reported.
func (m *Manager) Diagnostics(ctx context.Context, path string) ([]Diagnostic, error) {
	abs, err := m.resolve(path)
	if err != nil {
		return nil, err
	}

	// The server is asked to look at the file: a file that was never
	// opened has no diagnostics, and the empty answer of a file that
	// was never read is a lie.
	if _, err := m.server(ctx); err != nil {
		return nil, err
	}

	return m.diagnostics.of(abs), nil
}

// AllDiagnostics returns the problems the server found in every file.
func (m *Manager) AllDiagnostics(ctx context.Context) ([]Diagnostic, error) {
	if _, err := m.server(ctx); err != nil {
		return nil, err
	}

	return m.diagnostics.all(), nil
}

// Call is one end of a call: a function that calls another, or that
// is called by another.
type Call struct {
	// Name is the name of the function.
	Name string
	// Path is the file the function is in.
	Path string
	// Line is the line the call is at, counted from 1.
	Line int
	// Column is the column the call is at, counted from 1.
	Column int
}

// String returns the call as "path:line: name".
func (c Call) String() string {
	return fmt.Sprintf("%s:%d: %s", c.Path, c.Line, c.Name)
}

// Callers returns the functions that call the symbol at a place.
//
// The symbol is given by the file it is used in and the position of
// the use: the server resolves it from there, which is the only way
// to tell two symbols of the same name apart.
func (m *Manager) Callers(ctx context.Context, path string, line, column int) ([]Call, error) {
	item, err := m.callHierarchyItem(ctx, path, line, column)
	if err != nil {
		return nil, err
	}

	srv, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	calls, err := srv.IncomingCalls(reqCtx, *item)
	if err != nil {
		return nil, fmt.Errorf("code: the callers in %s: %w", path, err)
	}

	out := make([]Call, 0, len(calls))

	for _, call := range calls {
		out = append(out, callFrom(call.From))
	}

	return out, nil
}

// Callees returns the functions that the symbol at a place calls.
func (m *Manager) Callees(ctx context.Context, path string, line, column int) ([]Call, error) {
	item, err := m.callHierarchyItem(ctx, path, line, column)
	if err != nil {
		return nil, err
	}

	srv, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	calls, err := srv.OutgoingCalls(reqCtx, *item)
	if err != nil {
		return nil, fmt.Errorf("code: the callees in %s: %w", path, err)
	}

	out := make([]Call, 0, len(calls))

	for _, call := range calls {
		out = append(out, callFrom(call.To))
	}

	return out, nil
}

// callHierarchyItem asks the server about the symbol at a place.
//
// The server answers with the items of the hierarchy, and the first
// one is the symbol itself.
func (m *Manager) callHierarchyItem(
	ctx context.Context,
	path string,
	line, column int,
) (*protocol.CallHierarchyItem, error) {
	abs, err := m.resolve(path)
	if err != nil {
		return nil, err
	}

	srv, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	// The server answers a question about a position only for the
	// files it knows.
	if err := srv.OpenFile(reqCtx, abs); err != nil {
		return nil, err
	}

	position, err := m.position(abs, line, column)
	if err != nil {
		return nil, err
	}

	items, err := srv.PrepareCallHierarchy(reqCtx, abs, position.Line, position.Character)
	if err != nil {
		return nil, fmt.Errorf("code: the call hierarchy in %s: %w", path, err)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("%w: the call hierarchy of the symbol at %s:%d", ErrNotFound, path, line)
	}

	return &items[0], nil
}

// callFrom converts a call hierarchy item into a [Call].
func callFrom(item protocol.CallHierarchyItem) Call {
	call := Call{
		Name:   item.Name,
		Line:   int(item.SelectionRange.Start.Line) + 1,
		Column: int(item.SelectionRange.Start.Character) + 1,
	}

	if path, err := item.URI.Path(); err == nil {
		call.Path = path
	}

	return call
}

// Definition returns the places where a symbol is defined.
//
// The symbol is given by the file it is used in and the position of the
// use: the server resolves it from there, which is the only way to
// tell two symbols of the same name apart.
//
// The position is resolved by [Manager.Position], so the caller may
// give the name of the symbol instead of the column it is at.
func (m *Manager) Definition(ctx context.Context, path string, line, column int) ([]Location, error) {
	abs, err := m.resolve(path)
	if err != nil {
		return nil, err
	}

	srv, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	// The server answers a question about a position only for the
	// files it knows.
	if err := srv.OpenFile(reqCtx, abs); err != nil {
		return nil, err
	}

	position, err := m.position(abs, line, column)
	if err != nil {
		return nil, err
	}

	locations, err := srv.Definition(reqCtx, abs, position.Line, position.Character)
	if err != nil {
		return nil, fmt.Errorf("code: the definition in %s: %w", path, err)
	}

	if len(locations) == 0 {
		return nil, fmt.Errorf("%w: the definition of the symbol at %s:%d", ErrNotFound, path, line)
	}

	return m.locate(locations), nil
}

// References returns the places where a symbol is used.
//
// The declaration is included: the reader wants to see the whole of
// the symbol, not only its uses.
func (m *Manager) References(ctx context.Context, path string, line, column int) ([]Location, error) {
	abs, err := m.resolve(path)
	if err != nil {
		return nil, err
	}

	srv, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	// The server answers a question about a position only for the
	// files it knows.
	if err := srv.OpenFile(reqCtx, abs); err != nil {
		return nil, err
	}

	position, err := m.position(abs, line, column)
	if err != nil {
		return nil, err
	}

	locations, err := srv.References(reqCtx, abs, position.Line, position.Character, true)
	if err != nil {
		return nil, fmt.Errorf("code: the references in %s: %w", path, err)
	}

	if len(locations) == 0 {
		return nil, fmt.Errorf("%w: the references of the symbol at %s:%d", ErrNotFound, path, line)
	}

	return m.locate(locations), nil
}

// Rename returns the changes that rename a symbol.
//
// The changes are returned, not applied: the caller decides whether
// they are written to the disk. They are two kinds: the edits of the
// text, and the operations on the files themselves — a file that is
// created, moved or removed. The server may ask for the second kind
// alongside the first, and the caller shows both to the user.
func (m *Manager) Rename(
	ctx context.Context,
	path string,
	line, column int,
	newName string,
) ([]Edit, []FileOperation, error) {
	if strings.TrimSpace(newName) == "" {
		return nil, nil, errors.New("code: the new name is required")
	}

	abs, err := m.resolve(path)
	if err != nil {
		return nil, nil, err
	}

	srv, err := m.server(ctx)
	if err != nil {
		return nil, nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	// The server answers a question about a position only for the
	// files it knows.
	if err := srv.OpenFile(reqCtx, abs); err != nil {
		return nil, nil, err
	}

	position, err := m.position(abs, line, column)
	if err != nil {
		return nil, nil, err
	}

	edit, err := srv.Rename(reqCtx, abs, position.Line, position.Character, newName)
	if err != nil {
		return nil, nil, fmt.Errorf("code: the rename in %s: %w", path, err)
	}

	if edit == nil {
		return nil, nil, fmt.Errorf("%w: the symbol at %s:%d", ErrNotFound, path, line)
	}

	edits, err := editsFromWorkspaceEdit(*edit, srv.OffsetEncoding())
	if err != nil {
		return nil, nil, err
	}

	operations := fileOperations(*edit)

	if len(edits) == 0 && len(operations) == 0 {
		return nil, nil, fmt.Errorf("%w: the rename of the symbol at %s:%d", ErrNotFound, path, line)
	}

	return edits, operations, nil
}

// ReferencesOf returns the places where a symbol is used, over the
// whole workspace.
//
// The symbol is named, and the server finds it: the search covers the
// module, and the answer is every use of the symbol, in every file.
//
// The name alone is not enough when two symbols share it: the search
// returns the uses of all of them, and the caller tells them apart by
// the file they are in.
func (m *Manager) ReferencesOf(ctx context.Context, name string) ([]Location, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("code: the name of the symbol is required")
	}

	// The symbol is found first: the references need the place it is
	// declared at, and the declaration is what the search answers.
	declarations, err := m.Search(ctx, name)
	if err != nil {
		return nil, err
	}

	if len(declarations) == 0 {
		return nil, fmt.Errorf("%w: the symbol %q", ErrNotFound, name)
	}

	var out []Location

	for _, declaration := range declarations {
		locations, err := m.References(
			ctx,
			declaration.Path,
			declaration.Line,
			declaration.Column,
		)
		if err != nil {
			// A symbol that has no use is not a failure: the
			// declaration is still an answer.
			continue
		}

		out = append(out, locations...)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("%w: the references of the symbol %q", ErrNotFound, name)
	}

	return out, nil
}

// SearchResult is a symbol of the workspace that matches a query.
//
// It is the answer of the search: the name of the symbol, the kind of
// it, and the place it is declared.
type SearchResult struct {
	// Name is the name of the symbol.
	Name string
	// Kind is the kind of the declaration.
	Kind string
	// Path is the file the symbol is in.
	Path string
	// Line is the line the declaration is at, counted from 1.
	Line int
	// Column is the column the declaration is at, counted from 1.
	Column int
	// Container is the name of the symbol that holds it, when it is
	// held by one: the method of a type, for instance.
	Container string
}

// String returns the result as "path:line: name".
func (r SearchResult) String() string {
	return fmt.Sprintf("%s:%d: %s", r.Path, r.Line, r.Name)
}

// Search returns the symbols of the workspace that match a query.
//
// The query is a name or a part of one: the server matches it in a
// relaxed way, and the answers are the symbols of every file of the
// workspace.
//
// This is the search that reads the code instead of the text of it: it
// finds the declarations, and not the lines that hold the name.
func (m *Manager) Search(ctx context.Context, query string) ([]SearchResult, error) {
	if strings.TrimSpace(query) == "" {
		return nil, errors.New("code: the query is required")
	}

	srv, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	symbols, err := srv.WorkspaceSymbols(reqCtx, query)
	if err != nil {
		return nil, err
	}

	out := make([]SearchResult, 0, len(symbols))

	for _, symbol := range symbols {
		result, ok := searchResultFrom(symbol)
		if !ok {
			continue
		}

		// The answer is the code of the user: a symbol outside of the
		// root is not part of it.
		if !withinRoot(m.root, result.Path) {
			continue
		}

		out = append(out, result)
	}

	return out, nil
}

// searchResultFrom converts a symbol of the workspace into a
// [SearchResult].
func searchResultFrom(symbol protocol.WorkspaceSymbolResult) (SearchResult, bool) {
	location := symbol.GetLocation()

	path, err := location.URI.Path()
	if err != nil {
		return SearchResult{}, false
	}

	result := SearchResult{
		Name:   symbol.GetName(),
		Kind:   symbolKindOf(symbol),
		Path:   path,
		Line:   int(location.Range.Start.Line) + 1,
		Column: int(location.Range.Start.Character) + 1,
	}

	// The container is the symbol that holds this one: the method of
	// a type, for instance.
	if information, ok := symbol.(*protocol.SymbolInformation); ok {
		result.Container = information.ContainerName
	}

	return result, true
}

// symbolKindOf returns the kind of a symbol of the workspace.
//
// The interface of the protocol does not carry the kind, so it is read
// from the type of the symbol.
func symbolKindOf(symbol protocol.WorkspaceSymbolResult) string {
	switch typed := symbol.(type) {
	case *protocol.SymbolInformation:
		return symbolKind(typed.Kind)
	case *protocol.WorkspaceSymbol:
		return symbolKind(typed.Kind)
	default:
		return "symbol"
	}
}

// CreateFile creates an empty file.
//
// The path is relative to the root, or absolute inside it. A path that
// is outside of the root is refused: the root is the boundary.
func (m *Manager) CreateFile(path string) error {
	abs, err := m.resolve(path)
	if err != nil {
		return err
	}

	return m.applyFileOperation(FileOperation{Kind: "create", Path: abs})
}

// MoveFile moves a file.
//
// The destination is relative to the root, or absolute inside it. A
// file that is already at the destination is not replaced, unless the
// operation says so.
func (m *Manager) MoveFile(from, to string) error {
	source, err := m.resolve(from)
	if err != nil {
		return err
	}

	destination, err := m.resolve(to)
	if err != nil {
		return err
	}

	return m.applyFileOperation(FileOperation{
		Kind:    "rename",
		Path:    destination,
		OldPath: source,
	})
}

// DeleteFile removes a file.
//
// A directory is removed with everything in it, when the operation says
// so.
func (m *Manager) DeleteFile(path string, recursive bool) error {
	abs, err := m.resolve(path)
	if err != nil {
		return err
	}

	return m.applyFileOperation(FileOperation{
		Kind:      "delete",
		Path:      abs,
		Recursive: recursive,
	})
}

// Position is a place in a file, in the units of the protocol.
//
// The line is counted from 0 and the character from 0, which is what
// the protocol uses.
type Position struct {
	// Line is the line of the position, counted from 0.
	Line int
	// Character is the column of the position, counted from 0.
	Character int
}

// position resolves the place a caller points at.
//
// A model reads a file and knows the line of a symbol; the column is
// often a guess, and a guess that lands on a keyword — on the
// `return` of a line that calls a function — is answered by the
// server with "identifier not found".
//
// The column is therefore corrected: the identifier of the line that
// is the closest to the one the caller gave is taken. The closest and
// not the first, because the symbol is rarely the first word of the
// line it is on.
//
// The correction is what makes the tools usable: a model that points
// at the wrong column of the right line still gets the right answer.
func (m *Manager) position(path string, line, column int) (Position, error) {
	// The protocol counts from 0.
	zeroLine := line - 1

	if zeroLine < 0 {
		return Position{}, fmt.Errorf("code: the line %d is before the first line", line)
	}

	source, err := os.ReadFile(path)
	if err != nil {
		return Position{}, fmt.Errorf("code: read %s: %w", path, err)
	}

	lines := strings.Split(string(source), "\n")

	if zeroLine >= len(lines) {
		return Position{}, fmt.Errorf("code: the line %d is past the end of the file", line)
	}

	text := lines[zeroLine]

	// The column is counted from 1 by the tools, and from 0 by the
	// protocol.
	character := column - 1

	if character < 0 || character >= len(text) ||
		!isIdentifierAt(text, character) || isKeyword(text, character) {
		// The column does not point at a name: the closest one of the
		// line is taken instead.
		character = nearestIdentifier(text, character)
	}

	if character < 0 {
		return Position{}, fmt.Errorf("%w: the line %d holds no symbol", ErrNotFound, line)
	}

	return Position{Line: zeroLine, Character: character}, nil
}

// isIdentifierAt reports whether the character at an offset is part of
// an identifier.
func isIdentifierAt(text string, offset int) bool {
	if offset < 0 || offset >= len(text) {
		return false
	}

	c := text[offset]

	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// The keywords of Go.
//
// A keyword has the shape of a name, and the server refuses a position
// that is on one: "identifier not found". A model that points at the
// `return` of a line that calls a function lands on one.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// isKeyword reports whether the identifier at an offset is a keyword.
func isKeyword(text string, offset int) bool {
	start := offset

	for start > 0 && isIdentifierAt(text, start-1) {
		start--
	}

	end := offset

	for end < len(text) && isIdentifierAt(text, end) {
		end++
	}

	return goKeywords[text[start:end]]
}

// nearestIdentifier returns the offset of the identifier of a line that
// is the closest to an offset, or -1 when the line holds none.
//
// The identifiers of the line are the words of it: the keywords and
// the names are the same shape. The one that is the closest to what
// the caller gave is the one it meant — and a keyword is never it.
func nearestIdentifier(text string, offset int) int {
	if offset < 0 {
		offset = 0
	}

	best := -1
	bestDistance := 0

	for i := 0; i < len(text); i++ {
		if !isIdentifierAt(text, i) {
			continue
		}

		// The identifier must not start in the middle of a word.
		if i > 0 && isIdentifierAt(text, i-1) {
			continue
		}

		if isKeyword(text, i) {
			continue
		}

		distance := i - offset
		if distance < 0 {
			distance = -distance
		}

		if best < 0 || distance < bestDistance {
			best = i
			bestDistance = distance
		}
	}

	return best
}
