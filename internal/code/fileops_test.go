package code

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFileOperations checks the three operations on the files.
//
// They are the operations the language server may ask for alongside a
// rename, and the ones the agent asks for on its own.
func TestFileOperations(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// The creation of a file, with its parent directory.
	if err := manager.CreateFile("sub/dir/new.go"); err != nil {
		t.Fatalf("CreateFile() error = %v", err)
	}

	created := filepath.Join(root, "sub", "dir", "new.go")

	info, err := os.Stat(created)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	if info.Size() != 0 {
		t.Errorf("the file holds %d bytes, want an empty one", info.Size())
	}

	// The move of a file.
	if err := manager.MoveFile("sub/dir/new.go", "sub/moved.go"); err != nil {
		t.Fatalf("MoveFile() error = %v", err)
	}

	if _, err := os.Stat(created); err == nil {
		t.Error("the file is still at its old path, want it moved")
	}

	if _, err := os.Stat(filepath.Join(root, "sub", "moved.go")); err != nil {
		t.Errorf("Stat() error = %v, want the file at its new path", err)
	}

	// The removal of a file.
	if err := manager.DeleteFile("sub/moved.go", false); err != nil {
		t.Fatalf("DeleteFile() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "sub", "moved.go")); err == nil {
		t.Error("the file is still there, want it removed")
	}
}

// TestFileOperationsRefuseTheOutside checks the boundary.
//
// The root is the boundary: a file that is created, moved or removed
// outside of it is refused, and the file is left alone.
func TestFileOperationsRefuseTheOutside(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()

	secret := filepath.Join(outside, "secret.go")
	if err := os.WriteFile(secret, []byte("secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	// The creation of a file outside of the root.
	err = manager.CreateFile(filepath.Join(outside, "new.go"))
	if err == nil {
		t.Error("CreateFile() outside the root: no error, want one")
	}

	if _, err := os.Stat(filepath.Join(outside, "new.go")); err == nil {
		t.Error("the file was created outside the root")
	}

	// The removal of a file outside of the root.
	err = manager.DeleteFile(secret, false)
	if err == nil {
		t.Error("DeleteFile() outside the root: no error, want one")
	}

	if _, err := os.Stat(secret); err != nil {
		t.Errorf("Stat() error = %v, want the file untouched", err)
	}

	// The move of a file outside of the root.
	err = manager.MoveFile("a.go", filepath.Join(outside, "b.go"))
	if err == nil {
		t.Error("MoveFile() outside the root: no error, want one")
	}
}

// TestMoveFileRefusesToReplace checks that a file is not replaced by
// accident.
func TestMoveFileRefusesToReplace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("a\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("b\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := manager.MoveFile("a.go", "b.go"); err == nil {
		t.Error("MoveFile() over an existing file: no error, want one")
	}

	got, err := os.ReadFile(filepath.Join(root, "b.go"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(got) != "b\n" {
		t.Errorf("the file is %q, want it untouched", string(got))
	}
}

// TestApplyFileOperationsRefusesTheOutside checks that a set of
// operations is refused as a whole when one of them is outside.
//
// A half-applied set is worse than none: the files of the user are
// left in a state nobody asked for.
func TestApplyFileOperationsRefusesTheOutside(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()

	manager, err := NewManager(root, nil)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	err = manager.ApplyFileOperations([]FileOperation{
		{Kind: "create", Path: filepath.Join(root, "ok.go")},
		{Kind: "delete", Path: filepath.Join(outside, "secret.go")},
	})
	if err == nil {
		t.Fatal("ApplyFileOperations(): no error, want one")
	}

	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("the error is %v, want it to mention the boundary", err)
	}

	// The first operation was not applied either.
	if _, err := os.Stat(filepath.Join(root, "ok.go")); err == nil {
		t.Error("the first operation was applied, want the set refused as a whole")
	}
}
