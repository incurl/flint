// Package code gives the agent a view of the code of the application.
//
// It wraps a language server — gopls, for Go — and answers the questions
// a model asks about a codebase: what a file declares, where a symbol is
// defined, who uses it.
//
// The language server is a separate process, and it is started lazily:
// an application that never asks a question about its code never pays
// for it. The server is looked up on the PATH, and its absence is
// reported to the caller rather than guessed at.
//
// The package is deliberately free of any dependency on the agent: it
// knows about files and symbols, not about models and permissions. The
// agent package wraps it in tools.
package code

import (
	"errors"
	"fmt"
)

// The errors returned by the package.
var (
	// ErrNoServer is returned when no language server handles a file.
	//
	// It is the error the caller sees when the server is not installed:
	// the message says so, because the fix is to install it.
	ErrNoServer = errors.New("code: no language server for this file")

	// ErrServerMissing is returned when the language server executable
	// cannot be found on the PATH.
	ErrServerMissing = errors.New("code: the language server is not installed")

	// ErrOutsideRoot is returned when a path is outside the directory
	// the package is allowed to read.
	ErrOutsideRoot = errors.New("code: the path is outside the code directory")

	// ErrNotFound is returned when the server has no answer: the symbol
	// does not exist, or the file has no symbols.
	ErrNotFound = errors.New("code: not found")
)

// Symbol is a declaration found in a file.
//
// It is the flattened form of the LSP document symbols: the hierarchy
// is kept in the [Symbol.Children] of the top level symbols, and the
// model reads them as an outline.
type Symbol struct {
	// Name is the name of the declaration.
	Name string
	// Kind is the kind of the declaration: "function", "type", "struct",
	// "method", "variable", "constant", "field" or "interface".
	Kind string
	// Line is the line the declaration starts at, counted from 1.
	//
	// The LSP counts from 0, and a line the reader cannot point at is
	// not a line: the count starts at 1 here.
	Line int
	// Detail is the signature of the declaration, when the server
	// reports one.
	Detail string
	// Children holds the declarations nested in this one.
	Children []Symbol
}

// Location is a place in a file.
type Location struct {
	// Path is the absolute path of the file.
	Path string
	// Line is the line the place is at, counted from 1.
	Line int
	// Column is the column the place is at, counted from 1.
	Column int
	// Text is the line the place is at, without its newline.
	Text string
}

// String returns the location as "path:line:column", the form an
// editor understands.
func (l Location) String() string {
	return fmt.Sprintf("%s:%d:%d", l.Path, l.Line, l.Column)
}

// Edit is a change to a file.
//
// The change is a replacement of a range of bytes: the range is given
// by its offsets in the file, and the text replaces it. The offsets are
// bytes because that is what the language server speaks, and a
// conversion to lines would lose the part of the line that the edit
// does not touch.
type Edit struct {
	// Path is the absolute path of the file.
	Path string
	// Start is the offset of the first byte the edit replaces.
	Start int
	// End is the offset past the last byte the edit replaces.
	End int
	// Line is the line the edit starts at, counted from 1.
	//
	// It is the line a reader points at; the offsets are what the
	// edit actually uses.
	Line int
	// Text is the text that replaces the range.
	Text string
}

// String returns the edit as a description a model can read.
func (e Edit) String() string {
	return fmt.Sprintf("%s:%d", e.Path, e.Line)
}
