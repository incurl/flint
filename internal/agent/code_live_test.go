package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/incurl/flint/internal/code"
)

// TestCodeToolsLiveAgainstTheServer exercises the code tools against a
// real language server.
//
// The test is skipped when gopls is not on the PATH: the code tools are
// an addition, and the tests of the template must pass on a machine
// that does not have the server installed.
func TestCodeToolsLiveAgainstTheServer(t *testing.T) {
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls is not on the PATH")
	}

	root := t.TempDir()

	source := "package sample\n\n// Speak says something.\nfunc Speak() string {\n\treturn \"hello\"\n}\n\nfunc main() {\n\t_ = Speak()\n}\n"

	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module sample\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager, err := code.NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	defer manager.Close()

	registry := NewRegistry()
	RegisterCodeTools(registry, manager, nil)

	// The declarations of the file.
	symbols, err := registry.Get(ToolCodeSymbols)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	out, err := symbols.Run(context.Background(), `{"path":"sample.go"}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out, "Speak") {
		t.Errorf("the output is %q, want it to hold the symbol", out)
	}

	// The definition of the symbol, found from its use.
	definition, err := registry.Get(ToolCodeDefinition)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	out, err = definition.Run(context.Background(), `{"path":"sample.go","line":9,"column":7}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out, "sample.go") {
		t.Errorf("the output is %q, want it to hold the place", out)
	}

	// The references of the symbol.
	references, err := registry.Get(ToolCodeReferences)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	out, err = references.Run(context.Background(), `{"path":"sample.go","line":9,"column":7}`)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !strings.Contains(out, "reference") {
		t.Errorf("the output is %q, want it to hold the references", out)
	}
}
