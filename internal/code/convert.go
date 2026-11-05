package code

import (
	"fmt"
	"os"
	"strings"

	lsp "github.com/charmbracelet/x/powernap/pkg/lsp"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// symbolFromResult converts a document symbol into a [Symbol].
//
// The LSP returns two shapes: a hierarchical one, which keeps the
// children, and a flat one, which does not. Both are accepted.
func symbolFromResult(result protocol.DocumentSymbolResult) Symbol {
	switch typed := result.(type) {
	case *protocol.DocumentSymbol:
		symbol := Symbol{
			Name:   typed.Name,
			Kind:   symbolKind(typed.Kind),
			Line:   int(typed.Range.Start.Line) + 1,
			Detail: typed.Detail,
		}

		for _, child := range typed.Children {
			symbol.Children = append(symbol.Children, symbolFromResult(&child))
		}

		return symbol

	case *protocol.SymbolInformation:
		return Symbol{
			Name:   typed.Name,
			Kind:   symbolKind(typed.Kind),
			Line:   int(typed.Location.Range.Start.Line) + 1,
			Detail: typed.ContainerName,
		}

	default:
		return Symbol{Kind: "symbol"}
	}
}

// symbolKind names a kind of declaration.
//
// The LSP numbers them; the model reads names.
func symbolKind(kind protocol.SymbolKind) string {
	switch kind {
	case protocol.File:
		return "file"
	case protocol.Module:
		return "module"
	case protocol.Namespace:
		return "namespace"
	case protocol.Package:
		return "package"
	case protocol.Class:
		return "class"
	case protocol.Method:
		return "method"
	case protocol.Property:
		return "property"
	case protocol.Field:
		return "field"
	case protocol.Constructor:
		return "constructor"
	case protocol.Enum:
		return "enum"
	case protocol.Interface:
		return "interface"
	case protocol.Function:
		return "function"
	case protocol.Variable:
		return "variable"
	case protocol.Constant:
		return "constant"
	case protocol.String:
		return "string"
	case protocol.Number:
		return "number"
	case protocol.Boolean:
		return "boolean"
	case protocol.Array:
		return "array"
	case protocol.Object:
		return "object"
	case protocol.Key:
		return "key"
	case protocol.Null:
		return "null"
	case protocol.EnumMember:
		return "enum member"
	case protocol.Struct:
		return "struct"
	case protocol.Event:
		return "event"
	case protocol.Operator:
		return "operator"
	case protocol.TypeParameter:
		return "type parameter"
	default:
		return "symbol"
	}
}

// locate converts LSP locations into [Location] values.
//
// The line of each place is read from the file, so that the model sees
// the code and not only its address.
func (m *Manager) locate(locations []protocol.Location) []Location {
	out := make([]Location, 0, len(locations))

	for _, location := range locations {
		path, err := location.URI.Path()
		if err != nil {
			continue
		}

		line := int(location.Range.Start.Line) + 1

		out = append(out, Location{
			Path:   path,
			Line:   line,
			Column: int(location.Range.Start.Character) + 1,
			Text:   readLine(path, line),
		})
	}

	return out
}

// readLine returns the line of a file, without its newline.
//
// An unreadable file gives an empty line: the address is still useful,
// and a file that cannot be read is not a reason to fail the question.
func readLine(path string, line int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		return ""
	}

	return strings.TrimRight(lines[line-1], "\r")
}

// editsFromWorkspaceEdit converts a workspace edit into [Edit] values.
//
// The server answers a rename in one of two shapes: a map of changes
// per file, or a list of document changes. The client declares that it
// understands the second one, so that is the shape that comes back —
// but the first one is accepted as well, because a server is free to
// answer with either.
//
// A rename that creates, renames or deletes a file is refused: the
// caller asked for a rename, and a file that moves is a change the
// model did not describe.
func editsFromWorkspaceEdit(edit protocol.WorkspaceEdit) ([]Edit, error) {
	var out []Edit

	for uri, edits := range edit.Changes {
		path, err := uri.Path()
		if err != nil {
			return nil, fmt.Errorf("code: the edit of %q: %w", uri, err)
		}

		converted, err := editsFromTextEdits(path, edits)
		if err != nil {
			return nil, err
		}

		out = append(out, converted...)
	}

	for _, change := range edit.DocumentChanges {
		if change.TextDocumentEdit == nil {
			return nil, fmt.Errorf(
				"code: the server asked to %s a file, which a rename does not do",
				resourceOperation(change),
			)
		}

		path, err := change.TextDocumentEdit.TextDocument.URI.Path()
		if err != nil {
			return nil, fmt.Errorf("code: the edit of %q: %w", path, err)
		}

		textEdits := make([]protocol.TextEdit, 0, len(change.TextDocumentEdit.Edits))

		for _, elem := range change.TextDocumentEdit.Edits {
			textEdit, err := elem.AsTextEdit()
			if err != nil {
				return nil, fmt.Errorf("code: the edit of %s: %w", path, err)
			}

			textEdits = append(textEdits, textEdit)
		}

		converted, err := editsFromTextEdits(path, textEdits)
		if err != nil {
			return nil, err
		}

		out = append(out, converted...)
	}

	return out, nil
}

// editsFromTextEdits converts the edits of one file.
//
// The LSP edits are ranges of characters, counted in UTF-16 code units
// by default. They are converted into byte offsets, because that is
// what a Go string is indexed by.
//
// The conversion is done per line: the character offset is relative to
// the start of the line, and the byte offset of the line is found by
// counting the lines before it.
func editsFromTextEdits(path string, edits []protocol.TextEdit) ([]Edit, error) {
	if len(edits) == 0 {
		return nil, nil
	}

	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("code: read %s: %w", path, err)
	}

	lines := strings.Split(string(source), "\n")

	// The offset of the start of every line, so that a character
	// offset can be turned into a byte offset.
	lineOffsets := make([]int, len(lines)+1)

	offset := 0

	for i, line := range lines {
		lineOffsets[i] = offset
		offset += len(line) + 1 // the newline
	}

	lineOffsets[len(lines)] = offset

	var out []Edit

	for _, edit := range edits {
		start, err := byteOffset(lines, lineOffsets, edit.Range.Start)
		if err != nil {
			return nil, fmt.Errorf("code: the edit of %s: %w", path, err)
		}

		end, err := byteOffset(lines, lineOffsets, edit.Range.End)
		if err != nil {
			return nil, fmt.Errorf("code: the edit of %s: %w", path, err)
		}

		if start > end {
			return nil, fmt.Errorf("code: the edit of %s is inverted", path)
		}

		out = append(out, Edit{
			Path:  path,
			Start: start,
			End:   end,
			Line:  int(edit.Range.Start.Line) + 1,
			Text:  edit.NewText,
		})
	}

	return out, nil
}

// byteOffset turns an LSP position into a byte offset in the file.
//
// The position gives a line and a character offset in that line. The
// character offset is a count of UTF-16 code units, which is not the
// same as a count of bytes: a character outside the basic plane takes
// two code units and up to four bytes.
func byteOffset(lines []string, lineOffsets []int, position protocol.Position) (int, error) {
	line := int(position.Line)

	if line < 0 || line >= len(lines) {
		return 0, fmt.Errorf("the position is on line %d, outside the file", position.Line+1)
	}

	// The characters of the line are counted in UTF-16 code units.
	character := lsp.PositionToByteOffset(lines[line], position.Character)

	return lineOffsets[line] + character, nil
}

// resourceOperation names the file operation a document change carries.
//
// It is only used for the error message of a refused operation.
func resourceOperation(change protocol.DocumentChange) string {
	switch {
	case change.CreateFile != nil:
		return "create"
	case change.RenameFile != nil:
		return "rename"
	case change.DeleteFile != nil:
		return "delete"
	default:
		return "change"
	}
}
