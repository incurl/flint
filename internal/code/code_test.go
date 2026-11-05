package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWithinRoot guards the boundary of the code tools.
func TestWithinRoot(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "srv", "code")

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"the root itself", root, true},
		{"a file under the root", filepath.Join(root, "main.go"), true},
		{"a directory under the root", filepath.Join(root, "internal", "app"), true},
		{"the parent of the root", filepath.Join(string(filepath.Separator), "srv"), false},
		{"a sibling", filepath.Join(string(filepath.Separator), "srv", "other"), false},
		{"an escape through a parent", filepath.Join(root, "..", "other"), false},
		{"a deeper escape", filepath.Join(root, "internal", "..", "..", "etc"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := withinRoot(root, tc.path); got != tc.want {
				t.Errorf("withinRoot(%q, %q) = %t, want %t", root, tc.path, got, tc.want)
			}
		})
	}
}

// TestResolveRefusesTheOutside guards the boundary against the paths a
// model produces.
func TestResolveRefusesTheOutside(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// A path inside the root is resolved against the root.
	resolved, err := manager.resolve("main.go")
	if err != nil {
		t.Fatalf("resolve() error = %v", err)
	}

	if resolved != filepath.Join(root, "main.go") {
		t.Errorf("resolve() = %q, want %q", resolved, filepath.Join(root, "main.go"))
	}

	// A path that escapes the root is refused.
	escape := filepath.Join("..", filepath.Base(outside), "secret.go")

	if _, err := manager.resolve(escape); err == nil {
		t.Error("resolve() of an escaping path: no error, want one")
	}

	// An absolute path outside the root is refused too.
	if _, err := manager.resolve(filepath.Join(outside, "secret.go")); err == nil {
		t.Error("resolve() of an absolute path outside the root: no error, want one")
	}

	// An empty path is refused: it is not a path.
	if _, err := manager.resolve("  "); err == nil {
		t.Error("resolve() of an empty path: no error, want one")
	}
}

// TestNewManagerRefusesAnEmptyRoot guards the boundary itself.
func TestNewManagerRefusesAnEmptyRoot(t *testing.T) {
	t.Parallel()

	if _, err := NewManager("", nil); err == nil {
		t.Error("NewManager(\"\") : no error, want one")
	}
}

// TestApplyWritesTheEdits checks that the edits reach the file.
func TestApplyWritesTheEdits(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")

	content := "package main\n\nfunc old() {}\n\nfunc caller() { old() }\n"

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// Two edits: the declaration and the use. The second one is on a
	// later line, and the first one is applied after it.
	err = manager.Apply([]Edit{
		{Path: path, Start: 45, End: 48, Line: 5, Text: "renamed"},
		{Path: path, Start: 19, End: 22, Line: 3, Text: "renamed"},
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	want := "package main\n\nfunc renamed() {}\n\nfunc caller() { renamed() }\n"

	if string(got) != want {
		t.Errorf("the file is\n%q\nwant\n%q", string(got), want)
	}
}

// TestApplyRefusesTheOutside checks that the boundary holds when the
// edits are written.
func TestApplyRefusesTheOutside(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.go")

	if err := os.WriteFile(outside, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager.Apply([]Edit{{Path: outside, Start: 0, End: 6, Line: 1, Text: "gone"}})
	if err == nil {
		t.Fatal("Apply() of an edit outside the root: no error, want one")
	}

	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("Apply() error = %v, want it to mention the boundary", err)
	}

	// The file is untouched.
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(got) != "secret\n" {
		t.Errorf("the file is %q, want it untouched", string(got))
	}
}

// TestApplyKeepsThePermissions checks that a rewritten source file
// keeps its mode.
func TestApplyKeepsThePermissions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "main.go")

	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager.Apply([]Edit{{Path: path, Start: 0, End: 12, Line: 1, Text: "package other"}})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if info.Mode().Perm() != 0o644 {
		t.Errorf("the mode is %v, want %v", info.Mode().Perm(), os.FileMode(0o644))
	}
}
