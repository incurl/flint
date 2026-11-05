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

// TestLiveReplaceSymbol checks the replacement of a declaration.
//
// The four actions are checked: the declaration is replaced, the text
// is inserted before it and after it, and it is removed.
func TestLiveReplaceSymbol(t *testing.T) {
	if _, err := exec.LookPath(serverCommand); err != nil {
		t.Skipf("%s is not on the PATH", serverCommand)
	}

	root := t.TempDir()

	source := "package sample\n\n// Speak says something.\nfunc Speak() string {\n\treturn \"hello\"\n}\n"

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

	// The replacement of the declaration.
	content, err := manager.ReplaceSymbol(
		ctx,
		"sample.go",
		"Speak",
		ReplaceAction,
		"func Salute() string {\n\treturn \"hello\"\n}",
	)
	if err != nil {
		t.Fatalf("ReplaceSymbol() error = %v", err)
	}

	if !strings.Contains(content, "func Salute() string") {
		t.Errorf("the declaration was not replaced:\n%s", content)
	}

	if strings.Contains(content, "func Speak() string") {
		t.Errorf("the old declaration is still there:\n%s", content)
	}

	// The insertion before the declaration.
	content, err = manager.ReplaceSymbol(
		ctx,
		"sample.go",
		"Speak",
		AddBeforeAction,
		"// A comment.",
	)
	if err != nil {
		t.Fatalf("ReplaceSymbol() error = %v", err)
	}

	if !strings.Contains(content, "// A comment.\nfunc Speak()") {
		t.Errorf("the text was not inserted before:\n%s", content)
	}

	// The insertion after the declaration.
	content, err = manager.ReplaceSymbol(
		ctx,
		"sample.go",
		"Speak",
		AddAfterAction,
		"// After.",
	)
	if err != nil {
		t.Fatalf("ReplaceSymbol() error = %v", err)
	}

	if !strings.Contains(content, "}\n// After.") {
		t.Errorf("the text was not inserted after:\n%s", content)
	}

	// The removal of the declaration.
	content, err = manager.ReplaceSymbol(ctx, "sample.go", "Speak", DeleteAction, "")
	if err != nil {
		t.Fatalf("ReplaceSymbol() error = %v", err)
	}

	if strings.Contains(content, "func Speak()") {
		t.Errorf("the declaration was not removed:\n%s", content)
	}

	// The write of the result.
	if err := manager.ApplyContent("sample.go", content); err != nil {
		t.Fatalf("ApplyContent() error = %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if strings.Contains(string(got), "func Speak()") {
		t.Errorf("the file still holds the declaration:\n%s", string(got))
	}
}

// TestReplaceSymbolRefusesABadAction checks the actions that are not
// the four.
func TestReplaceSymbolRefusesABadAction(t *testing.T) {
	t.Parallel()

	manager, err := NewManager(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	_, err = manager.ReplaceSymbol(context.Background(), "main.go", "Foo", "rewrite", "x")
	if err == nil {
		t.Error("ReplaceSymbol() with a bad action: no error, want one")
	}

	if !strings.Contains(err.Error(), "invalid action") {
		t.Errorf("ReplaceSymbol() error = %v, want it to mention the action", err)
	}
}

// TestEditsOverlap checks the refusal of the edits that touch the same
// bytes.
//
// The ranges are half-open: an edit that ends where the next one
// begins does not overlap it.
func TestEditsOverlap(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		a     Edit
		b     Edit
		overl bool
	}{
		{"apart", Edit{Start: 0, End: 5}, Edit{Start: 10, End: 15}, false},
		{"touching", Edit{Start: 0, End: 5}, Edit{Start: 5, End: 10}, false},
		{"the same", Edit{Start: 0, End: 5}, Edit{Start: 2, End: 8}, true},
		{"inside", Edit{Start: 0, End: 10}, Edit{Start: 3, End: 5}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := editsOverlap(tc.a, tc.b); got != tc.overl {
				t.Errorf("editsOverlap(%v, %v) = %t, want %t", tc.a, tc.b, got, tc.overl)
			}
		})
	}
}
