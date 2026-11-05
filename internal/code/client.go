package code

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
	"github.com/charmbracelet/x/powernap/pkg/transport"
)

// The methods of the protocol that the client uses.
const (
	methodInitialize         = "initialize"  //nolint:misspell
	methodInitialized        = "initialized" //nolint:misspell
	methodDidOpen            = "textDocument/didOpen"
	methodDocumentSymbol     = "textDocument/documentSymbol"
	methodDefinition         = "textDocument/definition"
	methodReferences         = "textDocument/references"
	methodRename             = "textDocument/rename"
	methodPrepareCallHierarc = "textDocument/prepareCallHierarchy"
	methodIncomingCalls      = "callHierarchy/incomingCalls"
	methodOutgoingCalls      = "callHierarchy/outgoingCalls"
	methodWorkspaceSymbol    = "workspace/symbol"
	methodPublishDiagnostics = "textDocument/publishDiagnostics"
	methodWorkspaceConfig    = "workspace/configuration"
	methodDidChangeConfig    = "workspace/didChangeConfiguration"
	methodDidChangeWatched   = "workspace/didChangeWatchedFiles"
)

// The offsets of the protocol.
//
// A character offset is a count of code units in UTF-16, which is not
// the same as a count of bytes: the conversion is the reason a name
// outside of ASCII is the case where it matters.
const (
	offsetUTF8  = "utf-8"
	offsetUTF16 = "utf-16"
	offsetUTF32 = "utf-32"
)

// client is a language server.
//
// It speaks the protocol over the transport of the powernap package —
// the JSON-RPC layer, which is MIT — and owns the process it talks to.
//
// The client of that package does not expose `workspace/symbol`, and
// the field that holds the connection is not exported, so the requests
// of the protocol are made here instead. One server, one index, and
// every request the protocol offers.
type client struct {
	// conn is the connection to the server.
	conn *transport.Connection
	// command is the path of the server executable.
	command string
	// rootURI is the workspace root, as a URI.
	rootURI string
	// logger receives the diagnostics of the server.
	logger *slog.Logger

	// ctx is the context of the client: it outlives the call that
	// started the server.
	ctx context.Context
	// cancel stops the server.
	cancel context.CancelFunc

	// ready reports whether the handshake is done.
	ready atomic.Bool
	// offsetEncoding is the encoding the server negotiated.
	offsetEncoding atomic.Value

	// mu guards the process handle below.
	mu sync.Mutex
	// cmd is the server process.
	cmd *exec.Cmd
}

// newClient starts the language server.
//
// The process is started even when the handshake fails: a server that
// does not answer must be killed, and the caller gets the error.
func newClient(
	command, root string,
	logger *slog.Logger,
) (*client, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// The context of the client outlives the call that started it: the
	// server lives as long as the manager does.
	clientCtx, cancel := context.WithCancel(context.Background())

	stream, cmd, err := startServer(clientCtx, command)
	if err != nil {
		cancel()

		return nil, err
	}

	conn, err := transport.NewConnection(clientCtx, stream, logger)
	if err != nil {
		_ = cmd.Process.Kill()

		cancel()

		return nil, fmt.Errorf("code: the connection to %s: %w", command, err)
	}

	c := &client{
		conn:    conn,
		command: command,
		rootURI: string(protocol.URIFromPath(root)),
		logger:  logger,
		ctx:     clientCtx,
		cancel:  cancel,
		cmd:     cmd,
	}

	// The default is UTF-16, the default of the protocol.
	c.offsetEncoding.Store(offsetUTF16)

	return c, nil
}

// startServer starts the language server process.
func startServer(ctx context.Context, command string) (io.ReadWriteCloser, *exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, command) //nolint:gosec

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("code: the input of %s: %w", command, err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("code: the output of %s: %w", command, err)
	}

	// The errors of the server are read and dropped: they would
	// otherwise land on the screen of the application, in the middle
	// of the interface.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("code: the errors of %s: %w", command, err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("code: start %s: %w", command, err)
	}

	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()

	return &serverStream{reader: stdout, writer: stdin, closer: stdin}, cmd, nil
}

// serverStream is the pipe to the language server.
//
// The input and the output are two pipes, and the closer closes both:
// closing the input is what tells the server to stop.
type serverStream struct {
	reader io.Reader
	writer io.Writer
	closer io.Closer
}

// Read implements [io.Reader].
func (s *serverStream) Read(p []byte) (int, error) { return s.reader.Read(p) }

// Write implements [io.Writer].
func (s *serverStream) Write(p []byte) (int, error) { return s.writer.Write(p) }

// Close implements [io.Closer].
func (s *serverStream) Close() error { return s.closer.Close() }

// Initialize performs the handshake.
//
// The name is the one of the protocol, and the linter that wants the
// British spelling does not apply to a name that cannot be changed.
//
// The subscription to the diagnostics is registered before it: a server
// may publish while it starts, and a notification that arrives before
// the subscription is lost.
func (c *client) Initialize(
	ctx context.Context,
	onDiagnostics func(uri string, items []protocol.Diagnostic),
) error {
	c.conn.RegisterNotificationHandler(
		methodPublishDiagnostics,
		func(_ context.Context, _ string, params json.RawMessage) {
			var published protocol.PublishDiagnosticsParams

			if err := json.Unmarshal(params, &published); err != nil {
				return
			}

			if onDiagnostics == nil {
				return
			}

			onDiagnostics(string(published.URI), published.Diagnostics)
		},
	)

	// The server asks for the configuration of the workspace. The
	// answer is empty: the tools need none.
	c.conn.RegisterHandler(methodWorkspaceConfig, func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
		return []any{}, nil
	})

	params := map[string]any{
		"processId": os.Getpid(),
		"clientInfo": map[string]any{
			"name":    "flint",
			"version": "0.1.0",
		},
		"locale":                "en-us",
		"rootPath":              strings.TrimPrefix(c.rootURI, "file://"),
		"rootUri":               c.rootURI,
		"capabilities":          c.capabilities(),
		"workspaceFolders":      []any{},
		"initializationOptions": map[string]any{},
		"trace":                 "off",
	}

	var result protocol.InitializeResult

	if err := c.conn.Call(ctx, methodInitialize, params, &result); err != nil {
		return fmt.Errorf("code: the handshake with %s: %w", c.command, err)
	}

	c.offsetEncoding.Store(c.negotiatedEncoding(&result))

	if err := c.conn.Notify(ctx, methodInitialized, map[string]any{}); err != nil {
		return fmt.Errorf("code: the start of %s: %w", c.command, err)
	}

	// gopls needs to be told that the workspace is there: without the
	// notification it answers "no views".
	if err := c.conn.Notify(ctx, methodDidChangeConfig, map[string]any{
		"settings": map[string]any{},
	}); err != nil {
		return fmt.Errorf("code: the configuration of %s: %w", c.command, err)
	}

	if err := c.conn.Notify(ctx, methodDidChangeWatched, map[string]any{
		"changes": []map[string]any{
			{"uri": c.rootURI, "type": 1},
		},
	}); err != nil {
		return fmt.Errorf("code: the files of %s: %w", c.command, err)
	}

	c.ready.Store(true)

	return nil
}

// negotiatedEncoding returns the encoding the server chose.
//
// The server picks one of the encodings the client offered, and the
// answer is the one every offset of it is counted in.
func (c *client) negotiatedEncoding(result *protocol.InitializeResult) string {
	if result.Capabilities.PositionEncoding != nil {
		return string(*result.Capabilities.PositionEncoding)
	}

	if result.OffsetEncoding != "" {
		return result.OffsetEncoding
	}

	return offsetUTF16
}

// capabilities returns the capabilities of the client.
//
// They are the ones the tools need: the symbols of a file, its
// definition, its references, a rename, the call hierarchy, the
// symbols of the workspace and the diagnostics.
func (c *client) capabilities() map[string]any {
	return map[string]any{
		"textDocument": map[string]any{
			"synchronization": map[string]any{ //nolint:misspell
				"dynamicRegistration": true,
				"didSave":             true,
			},
			"documentSymbol": map[string]any{
				"dynamicRegistration":               true,
				"hierarchicalDocumentSymbolSupport": true,
			},
			"definition": map[string]any{"dynamicRegistration": true},
			"references": map[string]any{"dynamicRegistration": true},
			"rename": map[string]any{
				"dynamicRegistration": true,
				"prepareSupport":      true,
			},
			"callHierarchy": map[string]any{"dynamicRegistration": true},
			"publishDiagnostics": map[string]any{
				"relatedInformation": true,
				"versionSupport":     true,
			},
		},
		"workspace": map[string]any{
			"symbol": map[string]any{
				"dynamicRegistration": true,
				"resolveSupport": map[string]any{
					"properties": []string{"location.range"},
				},
			},
			"workspaceFolders": true,
		},
		"general": map[string]any{
			"positionEncodings": []string{offsetUTF8, offsetUTF16},
		},
	}
}

// OffsetEncoding returns the encoding the server negotiated.
func (c *client) OffsetEncoding() string {
	encoding, _ := c.offsetEncoding.Load().(string)

	return encoding
}

// Kill stops the server.
//
// The server is killed rather than asked to leave: a server that
// ignores the request would outlive the application.
func (c *client) Kill() {
	c.cancel()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cmd == nil || c.cmd.Process == nil {
		return
	}

	_ = c.cmd.Process.Kill()
}

// Call sends a request to the server and decodes the answer.
func (c *client) Call(ctx context.Context, method string, params any, result any) error {
	if !c.ready.Load() {
		return errors.New("code: the language server is not ready")
	}

	return c.conn.Call(ctx, method, params, result)
}

// OpenFile tells the server that a file is open.
//
// The server answers the questions about a position only for the files
// it knows: a file that was never opened has no content in the
// server, and the answer is empty — or worse, an error about a file
// that does not exist.
//
// The file is opened once: the server is told about it, and the
// content is what is on the disk at that moment. The agent edits the
// files through the tools, and the server reads them again when it
// needs to.
func (c *client) OpenFile(ctx context.Context, path string) error {
	if !c.ready.Load() {
		return errors.New("code: the language server is not ready")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("code: read %s: %w", path, err)
	}

	params := protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        protocol.URIFromPath(path),
			LanguageID: protocol.LanguageKind(languageID(path)),
			Version:    1,
			Text:       string(content),
		},
	}

	return c.conn.Notify(ctx, methodDidOpen, params)
}

// languageID returns the language of a file, for the protocol.
//
// The protocol names the languages the way the editors do: "go" for
// the source of Go.
func languageID(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c":
		return "c"
	case ".h":
		return "c"
	case ".cpp", ".cc", ".cxx":
		return "cpp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".json":
		return "json"
	case ".md":
		return "markdown"
	case ".yaml", ".yml":
		return "yaml"
	case ".html":
		return "html"
	case ".css":
		return "css"
	default:
		return "plaintext"
	}
}
