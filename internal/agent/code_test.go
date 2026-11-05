package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/incurl/flint/internal/code"
)

// TestRegisterCodeTools checks the tools the registry holds.
//
// The order matters: the reading tools come before the one that changes
// the code, because the order of the tool list is the order the model
// reads.
func TestRegisterCodeTools(t *testing.T) {
	t.Parallel()

	manager, err := code.NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	registry := NewRegistry()
	RegisterCodeTools(registry, manager, nil)

	want := []string{
		ToolCodeSymbols,
		ToolCodeDefinition,
		ToolCodeReferences,
		ToolCodeDiagnostics,
		ToolCodeCallHierarchy,
		ToolCodeRename,
		ToolCodeReplaceSymbol,
		ToolCodeCreateFile,
		ToolCodeMoveFile,
		ToolCodeDeleteFile,
	}

	got := registry.Names()

	if len(got) != len(want) {
		t.Fatalf("the tools are %v, want %v", got, want)
	}

	for i, name := range want {
		if got[i] != name {
			t.Errorf("the tool %d is %q, want %q", i, got[i], name)
		}
	}
}

// TestRegisterCodeToolsWithoutAManager checks that the tools are not
// registered without a manager: a tool that cannot answer is a tool
// the model calls and fails.
func TestRegisterCodeToolsWithoutAManager(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	RegisterCodeTools(registry, nil, nil)

	if names := registry.Names(); len(names) != 0 {
		t.Errorf("the tools are %v, want none", names)
	}
}

// TestCodeToolsAreInTheMode checks the classification of the code
// tools.
//
// The reading tools belong to the planning mode: a plan for a change
// to the code needs to know what the code is. The renaming tool does
// not: a rename is a change to the code of the user, exactly like a
// write is a change to the data of the user.
func TestCodeToolsAreInTheMode(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		ToolCodeSymbols,
		ToolCodeDefinition,
		ToolCodeReferences,
		ToolCodeDiagnostics,
		ToolCodeCallHierarchy,
	} {
		if IsMutatingTool(name) {
			t.Errorf("%s is a mutating tool, want it to be a reading one", name)
		}
	}

	for _, name := range []string{
		ToolCodeRename,
		ToolCodeReplaceSymbol,
		ToolCodeCreateFile,
		ToolCodeMoveFile,
		ToolCodeDeleteFile,
	} {
		if !IsMutatingTool(name) {
			t.Errorf("%s is not a mutating tool, want it to be one", name)
		}
	}
}

// TestCodeToolSchema checks the schema of the code tools.
//
// The schema is what the model reads to call a tool: an argument
// without a description is an argument the model fills in wrongly.
func TestCodeToolSchema(t *testing.T) {
	t.Parallel()

	manager, err := code.NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	cases := []struct {
		tool  Tool
		props []string
	}{
		{NewCodeSymbolsTool(manager), []string{"path"}},
		{NewCodeDefinitionTool(manager), []string{"path", "line", "column"}},
		{NewCodeReferencesTool(manager), []string{"path", "line", "column"}},
		{NewCodeDiagnosticsTool(manager), []string{"path"}},
		{NewCodeCallHierarchyTool(manager), []string{"path", "line", "column", "direction"}},
		{NewCodeRenameTool(manager), []string{"path", "line", "column", "new_name"}},
		{NewCodeReplaceSymbolTool(manager), []string{"path", "symbol", "action", "replacement"}},
	}

	for _, tc := range cases {
		schemer, ok := tc.tool.(Schemer)
		if !ok {
			t.Fatalf("%s does not declare a schema", tc.tool.Name())
		}

		properties, _ := schemer.Schema()

		for _, name := range tc.props {
			if _, exists := properties[name]; !exists {
				t.Errorf("%s: the property %q is missing", tc.tool.Name(), name)
			}
		}
	}
}

// TestCodeToolRejectsAMalformedInput checks that a malformed argument
// is an error the model can correct.
func TestCodeToolRejectsAMalformedInput(t *testing.T) {
	t.Parallel()

	manager, err := code.NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	tool := NewCodeSymbolsTool(manager)

	_, err = tool.Run(context.Background(), "{not json")

	if !errors.Is(err, ErrInvalidToolInput) {
		t.Errorf("Run() error = %v, want it to be an invalid input", err)
	}
}

// TestCodeRenameIsGated checks that the renaming tool asks the user
// before it changes the code.
//
// The check is made on a tool that would fail without the server: the
// point is that the gate is consulted first, and the refusal reaches
// the model instead of the file.
func TestCodeRenameIsGated(t *testing.T) {
	t.Parallel()

	manager, err := code.NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	requester := &refusingRequester{}

	registry := NewRegistry()
	RegisterCodeTools(registry, manager, NewGate(PermissionModeAsk, requester))

	tool, err := registry.Get(ToolCodeRename)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	_, err = tool.Run(context.Background(), `{"path":"main.go","line":1,"column":1,"new_name":"x"}`)

	if !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("Run() error = %v, want it to be a refusal", err)
	}

	if requester.calls() != 1 {
		t.Errorf("the requester was called %d times, want 1", requester.calls())
	}
}

// TestCodeReplaceSymbolIsGated checks that the replacement tool asks
// the user before it changes the code.
func TestCodeReplaceSymbolIsGated(t *testing.T) {
	t.Parallel()

	manager, err := code.NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	requester := &refusingRequester{}

	registry := NewRegistry()
	RegisterCodeTools(registry, manager, NewGate(PermissionModeAsk, requester))

	tool, err := registry.Get(ToolCodeReplaceSymbol)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	input := `{"path":"main.go","symbol":"Foo","action":"delete"}`

	_, err = tool.Run(context.Background(), input)

	if !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("Run() error = %v, want it to be a refusal", err)
	}

	if requester.calls() != 1 {
		t.Errorf("the requester was called %d times, want 1", requester.calls())
	}
}

// TestCodeFileOperationsAreGated checks that the tools that touch the
// files themselves ask the user first.
//
// A file that appears, a file that moves and a file that disappears
// are changes to the code of the user, exactly like a rename is.
func TestCodeFileOperationsAreGated(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		tool  string
		input string
	}{
		{"create", ToolCodeCreateFile, `{"path":"new.go"}`},
		{"move", ToolCodeMoveFile, `{"from":"a.go","to":"b.go"}`},
		{"delete", ToolCodeDeleteFile, `{"path":"a.go"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			manager, err := code.NewManager(t.TempDir(), nil)
			if err != nil {
				t.Fatalf("NewManager() error = %v", err)
			}

			requester := &refusingRequester{}

			registry := NewRegistry()
			RegisterCodeTools(registry, manager, NewGate(PermissionModeAsk, requester))

			tool, err := registry.Get(tc.tool)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}

			_, err = tool.Run(context.Background(), tc.input)

			if !errors.Is(err, ErrPermissionDenied) {
				t.Errorf("Run() error = %v, want it to be a refusal", err)
			}

			if requester.calls() != 1 {
				t.Errorf("the requester was called %d times, want 1", requester.calls())
			}
		})
	}
}

// TestCodeFileOperationsRunWhenAllowed checks that the file operations
// happen when the user says yes.
func TestCodeFileOperationsRunWhenAllowed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	manager, err := code.NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	registry := NewRegistry()
	RegisterCodeTools(registry, manager, NewGate(PermissionModeAllow, nil))

	tool, err := registry.Get(ToolCodeCreateFile)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if _, err := tool.Run(context.Background(), `{"path":"sub/new.go"}`); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "sub", "new.go")); err != nil {
		t.Errorf("Stat() error = %v, want the file to be created", err)
	}
}

// refusingRequester refuses every request.
type refusingRequester struct {
	mu    sync.Mutex
	count int
}

// RequestPermission implements [PermissionRequester].
func (r *refusingRequester) RequestPermission(
	_ context.Context,
	_ PermissionRequest,
) (PermissionDecision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.count++

	return PermissionDeny, nil
}

// calls returns the number of requests.
func (r *refusingRequester) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.count
}
