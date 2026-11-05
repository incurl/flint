package code

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// liveTest is a Go project the language server can answer questions
// about.
//
// It is written to a temporary directory, so that the test does not
// depend on the project it lives in.
const liveTest = `package sample

// Greeting is what the sample says.
type Greeting struct {
	// Text is the message.
	Text string
}

// Speak returns the greeting.
func Speak(g Greeting) string {
	return g.Text
}

func main() {
	g := Greeting{Text: "hello"}
	_ = Speak(g)
}
`

// TestLiveServer exercises the manager against a real language server.
//
// The test is skipped when gopls is not on the PATH: the code tools are
// an addition, and the tests of the template must pass on a machine
// that does not have the server installed.
func TestLiveServer(t *testing.T) {
	if _, err := exec.LookPath(serverCommand); err != nil {
		t.Skipf("%s is not on the PATH", serverCommand)
	}

	root := t.TempDir()

	path := filepath.Join(root, "sample.go")
	if err := os.WriteFile(path, []byte(liveTest), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// A module, so that the server treats the directory as a Go
	// package rather than as a file outside of one.
	goMod := filepath.Join(root, "go.mod")
	if err := os.WriteFile(goMod, []byte("module sample\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	defer manager.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The declarations of the file.
	symbols, err := manager.Symbols(ctx, "sample.go")
	if err != nil {
		t.Fatalf("Symbols() error = %v", err)
	}

	names := symbolNames(symbols)

	for _, want := range []string{"Greeting", "Speak", "main"} {
		if !names[want] {
			t.Errorf("the symbols are %v, want %q among them", names, want)
		}
	}

	// The definition of a symbol, found from the place it is used.
	//
	// The point is the call in main: a declaration has no definition,
	// and a server that is asked for one answers that it has none.
	line, column := findSymbol(t, path, "Speak(g)")

	definitions, err := manager.Definition(ctx, "sample.go", line, column)
	if err != nil {
		t.Fatalf("Definition() error = %v", err)
	}

	if len(definitions) == 0 {
		t.Fatal("Definition() returned no location")
	}

	if !strings.HasSuffix(definitions[0].Path, "sample.go") {
		t.Errorf("the definition is in %q, want the sample file", definitions[0].Path)
	}

	// The declaration of Speak is on the line that follows its
	// comment, and the comment is not part of the declaration.
	if definitions[0].Line != speakLine {
		t.Errorf("the definition is on line %d, want %d", definitions[0].Line, speakLine)
	}

	// The references of a symbol.
	references, err := manager.References(ctx, "sample.go", line, column)
	if err != nil {
		t.Fatalf("References() error = %v", err)
	}

	if len(references) < 2 {
		t.Errorf("the references are %d, want at least 2", len(references))
	}
}

// speakLine is the line of the declaration of Speak in the sample.
//
// The comment above it is not part of the declaration, so the line is
// one more than the line of the comment.
const speakLine = 10

// TestLiveRename exercises the rename against a real language server.
//
// The rename must change every use of the symbol, and the file must
// still be valid Go.
func TestLiveRename(t *testing.T) {
	if _, err := exec.LookPath(serverCommand); err != nil {
		t.Skipf("%s is not on the PATH", serverCommand)
	}

	root := t.TempDir()

	path := filepath.Join(root, "sample.go")
	if err := os.WriteFile(path, []byte(liveTest), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	goMod := filepath.Join(root, "go.mod")
	if err := os.WriteFile(goMod, []byte("module sample\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	defer manager.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The rename is asked for at the call in main, so that the server
	// renames the symbol and every use of it.
	line, column := findSymbol(t, path, "Speak(g)")

	edits, err := manager.Rename(ctx, "sample.go", line, column, "Utter")
	if err != nil {
		t.Fatalf("Rename() error = %v", err)
	}

	if len(edits) == 0 {
		t.Fatal("Rename() returned no edit")
	}

	if err := manager.Apply(edits); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !strings.Contains(string(got), "func Utter(g Greeting) string") {
		t.Errorf("the declaration was not renamed:\n%s", string(got))
	}

	if !strings.Contains(string(got), "_ = Utter(g)") {
		t.Errorf("the use was not renamed:\n%s", string(got))
	}

	if strings.Contains(string(got), "Speak") {
		t.Errorf("the old name is still there:\n%s", string(got))
	}

	// The file must still compile: a rename that breaks the code is
	// not a rename.
	build := exec.CommandContext(ctx, "go", "build", "./...")
	build.Dir = root

	if out, err := build.CombinedOutput(); err != nil {
		t.Errorf("the renamed file does not build: %v\n%s", err, string(out))
	}
}

// TestLiveRenameWithNonASCII checks the rename of a symbol whose name
// is not in ASCII.
//
// The offsets of the protocol are counted in the encoding the server
// negotiated, and the bytes of the file are counted in UTF-8. The two
// only agree on ASCII, so a name with an accent is the case where the
// conversion is wrong.
func TestLiveRenameWithNonASCII(t *testing.T) {
	if _, err := exec.LookPath(serverCommand); err != nil {
		t.Skipf("%s is not on the PATH", serverCommand)
	}

	root := t.TempDir()

	// "Grüß" is five bytes in UTF-8 and four UTF-16 code units.
	source := "package sample\n\n// Grüß greets.\nfunc Grüß() string {\n\treturn \"hello\"\n}\n\nfunc main() {\n\t_ = Grüß()\n}\n"

	path := filepath.Join(root, "sample.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	goMod := filepath.Join(root, "go.mod")
	if err := os.WriteFile(goMod, []byte("module sample\n\ngo 1.27\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	defer manager.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The use of the symbol is on the last line of the file.
	line, column := findSymbol(t, path, "Grüß()")

	edits, err := manager.Rename(ctx, "sample.go", line, column, "Salute")
	if err != nil {
		t.Fatalf("Rename() error = %v", err)
	}

	if err := manager.Apply(edits); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if !strings.Contains(string(got), "func Salute() string") {
		t.Errorf("the declaration was not renamed:\n%s", string(got))
	}

	if !strings.Contains(string(got), "_ = Salute()") {
		t.Errorf("the use was not renamed:\n%s", string(got))
	}

	// The comment above the declaration is not renamed: a comment is
	// not a symbol, and the server leaves it alone.
	if strings.Contains(string(got), "func Grüß") {
		t.Errorf("the declaration was not renamed:\n%s", string(got))
	}

	// The file must still compile: a rename that breaks the code is
	// not a rename.
	build := exec.CommandContext(ctx, "go", "build", "./...")
	build.Dir = root

	if out, err := build.CombinedOutput(); err != nil {
		t.Errorf("the renamed file does not build: %v\n%s", err, string(out))
	}
}

// symbolNames returns the names of the symbols and of their children.
func symbolNames(symbols []Symbol) map[string]bool {
	names := make(map[string]bool)

	for _, symbol := range symbols {
		names[symbol.Name] = true

		for name := range symbolNames(symbol.Children) {
			names[name] = true
		}
	}

	return names
}

// findSymbol returns the line and the column of the first occurrence
// of a text.
//
// The column is counted from 1, which is the convention of the tools:
// a model points at a place in a file with the line and the column it
// reads.
func findSymbol(t *testing.T, path, text string) (int, int) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	for i, line := range strings.Split(string(data), "\n") {
		if column := strings.Index(line, text); column >= 0 {
			return i + 1, column + 1
		}
	}

	t.Fatalf("the text %q is not in %s", text, path)

	return 0, 0
}
