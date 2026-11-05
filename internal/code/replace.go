package code

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// The actions a replacement can take.
const (
	// ReplaceAction replaces the whole declaration.
	ReplaceAction = "replace"
	// AddBeforeAction inserts the text before the declaration.
	AddBeforeAction = "add_before"
	// AddAfterAction inserts the text after the declaration.
	AddAfterAction = "add_after"
	// DeleteAction removes the declaration.
	DeleteAction = "delete"
)

// ErrInvalidAction is returned when the action of a replacement is not
// one of the four.
var ErrInvalidAction = errors.New("code: invalid action")

// ReplaceSymbol replaces a declaration of a file.
//
// The declaration is found by its name: the server answers with the
// symbols of the file, and the one that carries the name is the one
// that is replaced.
//
// The action says what to do with it: replace it, insert something
// before it, insert something after it, or remove it.
//
// The result is the new content of the file. It is not written: the
// caller decides whether it is, and the caller is behind the
// permission gate.
func (m *Manager) ReplaceSymbol(
	ctx context.Context,
	path, name, action, replacement string,
) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("code: the name of the symbol is required")
	}

	if !validAction(action) {
		return "", fmt.Errorf("%w: %q", ErrInvalidAction, action)
	}

	if action != DeleteAction && strings.TrimSpace(replacement) == "" {
		return "", fmt.Errorf("code: the replacement is required for the %q action", action)
	}

	abs, err := m.resolve(path)
	if err != nil {
		return "", err
	}

	srv, err := m.server(ctx)
	if err != nil {
		return "", err
	}

	reqCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	symbols, err := srv.DocumentSymbols(reqCtx, abs)
	if err != nil {
		return "", fmt.Errorf("code: the symbols of %s: %w", path, err)
	}

	target := findSymbolByName(symbols, name)
	if target == nil {
		return "", fmt.Errorf("%w: the symbol %q in %s", ErrNotFound, name, path)
	}

	source, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("code: read %s: %w", path, err)
	}

	lines := strings.Split(string(source), "\n")

	startLine := int(target.GetRange().Start.Line)
	endLine := int(target.GetRange().End.Line)

	if startLine < 0 || startLine >= len(lines) {
		return "", fmt.Errorf("code: the symbol %q is outside the file", name)
	}

	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	var updated []string

	switch action {
	case ReplaceAction:
		updated = append(updated, lines[:startLine]...)
		updated = append(updated, strings.Split(replacement, "\n")...)
		updated = append(updated, lines[endLine+1:]...)

	case AddBeforeAction:
		updated = append(updated, lines[:startLine]...)
		updated = append(updated, strings.Split(replacement, "\n")...)
		updated = append(updated, lines[startLine:]...)

	case AddAfterAction:
		updated = append(updated, lines[:endLine+1]...)
		updated = append(updated, strings.Split(replacement, "\n")...)
		updated = append(updated, lines[endLine+1:]...)

	case DeleteAction:
		updated = append(updated, lines[:startLine]...)
		updated = append(updated, lines[endLine+1:]...)
	}

	return strings.Join(updated, "\n"), nil
}

// ApplyContent writes a whole file.
//
// It is the write of a replacement: the content is the new content of
// the file, and it is written as a whole.
func (m *Manager) ApplyContent(path, content string) error {
	abs, err := m.resolve(path)
	if err != nil {
		return err
	}

	return writeFileAtomic(abs, []byte(content))
}

// validAction reports whether an action is one of the four.
func validAction(action string) bool {
	switch action {
	case ReplaceAction, AddBeforeAction, AddAfterAction, DeleteAction:
		return true
	default:
		return false
	}
}

// findSymbolByName looks for a symbol by name in the symbols of a
// file.
//
// The children are searched too: a method is a child of its type.
func findSymbolByName(symbols []protocol.DocumentSymbolResult, name string) protocol.DocumentSymbolResult {
	for _, symbol := range symbols {
		if symbol.GetName() == name {
			return symbol
		}

		document, ok := symbol.(*protocol.DocumentSymbol)
		if !ok || len(document.Children) == 0 {
			continue
		}

		children := make([]protocol.DocumentSymbolResult, len(document.Children))
		for i := range document.Children {
			children[i] = &document.Children[i]
		}

		if found := findSymbolByName(children, name); found != nil {
			return found
		}
	}

	return nil
}
