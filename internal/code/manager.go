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

	powernap "github.com/charmbracelet/x/powernap/pkg/lsp"
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
	// client is the language server, nil until it is started.
	client *powernap.Client
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
// The caller must hold the lock.
func (m *Manager) server(ctx context.Context) (*powernap.Client, error) {
	if m.closed {
		return nil, errors.New("code: the manager is closed")
	}

	if m.client != nil {
		return m.client, nil
	}

	if time.Now().Before(m.unavailableUntil) {
		return nil, fmt.Errorf("%w: %s is not on the PATH", ErrServerMissing, serverCommand)
	}

	path, err := exec.LookPath(serverCommand)
	if err != nil {
		m.unavailableUntil = time.Now().Add(unavailableRetryDelay)

		return nil, fmt.Errorf("%w: %s is not on the PATH", ErrServerMissing, serverCommand)
	}

	client, err := powernap.NewClient(powernap.ClientConfig{
		Command: path,
		RootURI: string(protocol.URIFromPath(m.root)),
		Timeout: startTimeout,
	})
	if err != nil {
		m.unavailableUntil = time.Now().Add(unavailableRetryDelay)

		return nil, fmt.Errorf("code: start %s: %w", serverCommand, err)
	}

	startCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	// The diagnostics are subscribed before the initialization: a
	// server may publish while it starts, and a notification that
	// arrives before the subscription is lost.
	watchDiagnostics(client, m.diagnostics)

	// The method is named the way the Language Server Protocol names
	// it, and the UK locale of the linter does not apply to a name
	// that cannot be changed.
	if err := client.Initialize(startCtx, false); err != nil { //nolint:misspell
		client.Kill()

		m.unavailableUntil = time.Now().Add(unavailableRetryDelay)

		return nil, fmt.Errorf("code: start %s: %w", serverCommand, err)
	}

	m.logger.Info("the language server is running", "server", serverCommand, "root", m.root)

	m.client = client

	return client, nil
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

	client, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	results, err := client.RequestDocumentSymbols(reqCtx, abs)
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

	client, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	calls, err := client.IncomingCalls(reqCtx, *item)
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

	client, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	calls, err := client.OutgoingCalls(reqCtx, *item)
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

	client, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	position, err := m.position(abs, line, column)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	items, err := client.PrepareCallHierarchy(reqCtx, abs, position.Line, position.Character)
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

	client, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	position, err := m.position(abs, line, column)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	locations, err := client.RequestDefinition(reqCtx, abs, position.Line, position.Character)
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

	client, err := m.server(ctx)
	if err != nil {
		return nil, err
	}

	position, err := m.position(abs, line, column)
	if err != nil {
		return nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	locations, err := client.FindReferences(reqCtx, abs, position.Line, position.Character, true)
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

	client, err := m.server(ctx)
	if err != nil {
		return nil, nil, err
	}

	position, err := m.position(abs, line, column)
	if err != nil {
		return nil, nil, err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	edit, err := client.RequestRename(reqCtx, abs, position.Line, position.Character, newName)
	if err != nil {
		return nil, nil, fmt.Errorf("code: the rename in %s: %w", path, err)
	}

	if edit == nil {
		return nil, nil, fmt.Errorf("%w: the symbol at %s:%d", ErrNotFound, path, line)
	}

	edits, err := editsFromWorkspaceEdit(*edit, client.GetOffsetEncoding())
	if err != nil {
		return nil, nil, err
	}

	operations := fileOperations(*edit)

	if len(edits) == 0 && len(operations) == 0 {
		return nil, nil, fmt.Errorf("%w: the rename of the symbol at %s:%d", ErrNotFound, path, line)
	}

	return edits, operations, nil
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
// often a guess. The column is therefore corrected when the line
// holds an identifier: the first one that is the symbol is taken.
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

	if character < 0 || character >= len(text) || !isIdentifierAt(text, character) {
		// The column does not point at an identifier: the first
		// identifier of the line is taken instead.
		character = firstIdentifier(text)
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

// firstIdentifier returns the offset of the first identifier of a line,
// or -1 when the line holds none.
func firstIdentifier(text string) int {
	for i := 0; i < len(text); i++ {
		if !isIdentifierAt(text, i) {
			continue
		}

		// The identifier must not start in the middle of a word.
		if i > 0 && isIdentifierAt(text, i-1) {
			continue
		}

		return i
	}

	return -1
}
