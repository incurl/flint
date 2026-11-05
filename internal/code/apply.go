package code

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Apply writes the edits to the disk.
//
// The edits are applied file by file, and a file that cannot be written
// leaves the others alone: a rename that changes two files and fails
// on the second leaves the code half renamed, which is worse than not
// renaming it at all.
//
// The file is written through a temporary file and a rename, so that a
// crash in the middle does not leave a truncated source file behind.
func (m *Manager) Apply(edits []Edit) error {
	if len(edits) == 0 {
		return nil
	}

	byFile := make(map[string][]Edit)

	for _, edit := range edits {
		// The edit must be inside the root: the server is rooted
		// there, but the check is cheap and the boundary is the one
		// thing that must hold.
		if !withinRoot(m.root, edit.Path) {
			return fmt.Errorf("%w: %s", ErrOutsideRoot, edit.Path)
		}

		byFile[edit.Path] = append(byFile[edit.Path], edit)
	}

	paths := make([]string, 0, len(byFile))
	for path := range byFile {
		paths = append(paths, path)
	}

	// The files are written in a stable order, so that a failure is
	// the same failure every time.
	sort.Strings(paths)

	for _, path := range paths {
		if err := applyToFile(path, byFile[path]); err != nil {
			return err
		}
	}

	return nil
}

// applyToFile writes the edits of one file.
//
// The edits are applied from the end of the file backwards, so that the
// offsets of the ones that are not applied yet are still the ones of
// the file on the disk.
func applyToFile(path string, edits []Edit) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("code: read %s: %w", path, err)
	}

	// Two edits that touch the same bytes cannot both be applied: the
	// second one would land in the middle of the first. The server is
	// not supposed to send them, and a rename that produces one is
	// refused rather than applied.
	if err := checkOverlapping(edits); err != nil {
		return fmt.Errorf("code: %s: %w", path, err)
	}

	sorted := make([]Edit, len(edits))
	copy(sorted, edits)

	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Start > sorted[j].Start
	})

	content := source

	for _, edit := range sorted {
		if edit.Start < 0 || edit.End > len(content) || edit.Start > edit.End {
			return fmt.Errorf("code: the edit of %s is outside the file", path)
		}

		updated := make([]byte, 0, len(content)-(edit.End-edit.Start)+len(edit.Text))
		updated = append(updated, content[:edit.Start]...)
		updated = append(updated, edit.Text...)
		updated = append(updated, content[edit.End:]...)

		content = updated
	}

	return writeFileAtomic(path, content)
}

// checkOverlapping refuses the edits that touch the same bytes.
//
// The ranges are half-open intervals, as the specification says: an
// edit that ends where the next one begins does not overlap it.
func checkOverlapping(edits []Edit) error {
	for i := range edits {
		for j := i + 1; j < len(edits); j++ {
			if editsOverlap(edits[i], edits[j]) {
				return fmt.Errorf("the edits overlap at bytes %d and %d", edits[i].Start, edits[j].Start)
			}
		}
	}

	return nil
}

// editsOverlap reports whether two edits touch the same bytes.
func editsOverlap(a, b Edit) bool {
	if a.Start > b.End || b.Start > a.End {
		return false
	}

	// The ranges are half-open: an edit that ends where the other
	// begins does not overlap it.
	if a.Start == b.End || b.Start == a.End {
		return false
	}

	return true
}

// writeFileAtomic writes a file through a temporary one.
//
// The temporary file is in the same directory, so that the rename is
// atomic: it cannot fail with a cross-device link, and the file is
// never half written.
func writeFileAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".flint-code-*")
	if err != nil {
		return fmt.Errorf("code: create the temporary file in %s: %w", dir, err)
	}

	name := tmp.Name()

	defer func() {
		// The removal is a no-op when the file was renamed already.
		_ = os.Remove(name)
	}()

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("code: write %s: %w", name, err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("code: close %s: %w", name, err)
	}

	// The permissions of the file are kept: the source of a program
	// is not executable because of the agent, and a file that loses
	// its mode is a surprise.
	info, err := os.Stat(path)
	if err == nil {
		_ = os.Chmod(name, info.Mode().Perm())
	}

	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("code: replace %s: %w", path, err)
	}

	return nil
}
