package code

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// FileOperation is a change to the files themselves: a file that is
// created, one that is moved, or one that is removed.
//
// The language server may ask for them alongside the edits of a
// rename. They are rare — gopls does not send them for a symbol —
// but they are part of the protocol, and a server is free to send
// them.
//
// They are never applied on their own: the caller shows them to the
// user, who decides.
type FileOperation struct {
	// Kind is the operation: "create", "rename" or "delete".
	Kind string
	// Path is the file the operation is about.
	//
	// For a rename it is the file that moves.
	Path string
	// OldPath is the file a rename moves from.
	//
	// It is empty for the other operations.
	OldPath string
	// Overwrite reports whether the operation replaces a file that is
	// already there.
	Overwrite bool
	// Recursive reports whether a delete removes a directory and
	// everything in it.
	Recursive bool
}

// String returns the operation as a description a model can read.
func (o FileOperation) String() string {
	switch o.Kind {
	case "create":
		return "create " + o.Path
	case "rename":
		return "rename " + o.OldPath + " to " + o.Path
	case "delete":
		return "delete " + o.Path
	default:
		return o.Kind + " " + o.Path
	}
}

// fileOperations extracts the file operations of a workspace edit.
//
// The operations are returned, not applied: a rename of a symbol must
// not move a file behind the user's back, and the caller needs to see
// them to ask.
func fileOperations(edit protocol.WorkspaceEdit) []FileOperation {
	var out []FileOperation

	for _, change := range edit.DocumentChanges {
		switch {
		case change.CreateFile != nil:
			path, err := change.CreateFile.URI.Path()
			if err != nil {
				continue
			}

			operation := FileOperation{Kind: "create", Path: path}

			if change.CreateFile.Options != nil {
				operation.Overwrite = change.CreateFile.Options.Overwrite
			}

			out = append(out, operation)

		case change.RenameFile != nil:
			oldPath, err := change.RenameFile.OldURI.Path()
			if err != nil {
				continue
			}

			newPath, err := change.RenameFile.NewURI.Path()
			if err != nil {
				continue
			}

			operation := FileOperation{Kind: "rename", Path: newPath, OldPath: oldPath}

			if change.RenameFile.Options != nil {
				operation.Overwrite = change.RenameFile.Options.Overwrite
			}

			out = append(out, operation)

		case change.DeleteFile != nil:
			path, err := change.DeleteFile.URI.Path()
			if err != nil {
				continue
			}

			operation := FileOperation{Kind: "delete", Path: path}

			if change.DeleteFile.Options != nil {
				operation.Recursive = change.DeleteFile.Options.Recursive
			}

			out = append(out, operation)
		}
	}

	return out
}

// ApplyFileOperations carries out the file operations.
//
// Every path is checked against the root: an operation that reaches
// outside of it is refused, and nothing is applied.
//
// The operations are applied in the order they were given, and the
// first failure stops the rest: a half-applied set of file operations
// is worse than none.
func (m *Manager) ApplyFileOperations(operations []FileOperation) error {
	for _, operation := range operations {
		if !withinRoot(m.root, operation.Path) {
			return fmt.Errorf("%w: %s", ErrOutsideRoot, operation.Path)
		}

		if operation.OldPath != "" && !withinRoot(m.root, operation.OldPath) {
			return fmt.Errorf("%w: %s", ErrOutsideRoot, operation.OldPath)
		}
	}

	for _, operation := range operations {
		if err := m.applyFileOperation(operation); err != nil {
			return err
		}
	}

	return nil
}

// applyFileOperation carries out one file operation.
func (m *Manager) applyFileOperation(operation FileOperation) error {
	switch operation.Kind {
	case "create":
		if _, err := os.Stat(operation.Path); err == nil && !operation.Overwrite {
			return fmt.Errorf("code: %s already exists", operation.Path)
		}

		if err := os.MkdirAll(filepath.Dir(operation.Path), 0o750); err != nil {
			return fmt.Errorf("code: create the directory of %s: %w", operation.Path, err)
		}

		file, err := os.OpenFile(operation.Path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return fmt.Errorf("code: create %s: %w", operation.Path, err)
		}

		return file.Close()

	case "rename":
		if _, err := os.Stat(operation.Path); err == nil && !operation.Overwrite {
			return fmt.Errorf("code: %s already exists", operation.Path)
		}

		if err := os.MkdirAll(filepath.Dir(operation.Path), 0o750); err != nil {
			return fmt.Errorf("code: create the directory of %s: %w", operation.Path, err)
		}

		if err := os.Rename(operation.OldPath, operation.Path); err != nil {
			return fmt.Errorf("code: move %s to %s: %w", operation.OldPath, operation.Path, err)
		}

		return nil

	case "delete":
		if operation.Recursive {
			if err := os.RemoveAll(operation.Path); err != nil {
				return fmt.Errorf("code: remove %s: %w", operation.Path, err)
			}

			return nil
		}

		if err := os.Remove(operation.Path); err != nil {
			return fmt.Errorf("code: remove %s: %w", operation.Path, err)
		}

		return nil

	default:
		return fmt.Errorf("code: unknown file operation %q", operation.Kind)
	}
}
