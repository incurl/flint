package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/incurl/flint/internal/code"
)

// The names of the code tools.
//
// They are the identifiers the model uses to call the tools, so they
// are part of the contract with the model and must stay stable.
const (
	// ToolCodeSymbols lists the declarations of a file.
	ToolCodeSymbols = "code_symbols"
	// ToolCodeDefinition finds where a symbol is defined.
	ToolCodeDefinition = "code_definition"
	// ToolCodeReferences finds where a symbol is used.
	ToolCodeReferences = "code_references"
	// ToolCodeRename renames a symbol.
	ToolCodeRename = "code_rename"
)

// The descriptions the model reads.
//
// They say what the tool does and when to use it, because a vague
// description produces a model that calls the wrong tool.
const (
	codeSymbolsDescription = "List the declarations of a file in the code of the application: " +
		"the functions, the types, the methods and the variables it holds. " +
		"Use it to see what a file declares before you read it, and to find " +
		"the file that holds something you are looking for."

	codeDefinitionDescription = "Find where a symbol is defined. " +
		"The symbol is the one at the given position of the given file: " +
		"the position of a use of it is enough. " +
		"Use it when you have the name of something and you need the code " +
		"that declares it."

	codeReferencesDescription = "Find every place where a symbol is used. " +
		"The symbol is the one at the given position of the given file: " +
		"the position of a use of it is enough. " +
		"Use it before you change a symbol, to see what depends on it."

	codeRenameDescription = "Rename a symbol in the code of the application. " +
		"The symbol is the one at the given position of the given file, and " +
		"every use of it is renamed with it. " +
		"Use it when the user asks for a rename."
)

// The parameter types of the code tools.
//
// The `json` and `description` tags are read by the model library to
// generate the schema of the tool, so they are part of the contract
// with the model.

// CodeSymbolsParams are the arguments of the code_symbols tool.
type CodeSymbolsParams struct {
	// Path is the path of the file, relative to the code directory.
	Path string `json:"path" description:"The path of the file, relative to the code directory of the application."`
}

// CodePositionParams are the arguments of the tools that take a
// position in a file.
type CodePositionParams struct {
	// Path is the path of the file, relative to the code directory.
	Path string `json:"path" description:"The path of the file, relative to the code directory of the application."`
	// Line is the line of the symbol, counted from 1.
	Line int `json:"line" description:"The line the symbol is at, counted from 1."`
	// Column is the column of the symbol, counted from 1.
	Column int `json:"column" description:"The column the symbol is at, counted from 1. Use 1 when the column is not known."`
}

// CodeRenameParams are the arguments of the code_rename tool.
type CodeRenameParams struct {
	// Path is the path of the file, relative to the code directory.
	Path string `json:"path" description:"The path of the file, relative to the code directory of the application."`
	// Line is the line of the symbol, counted from 1.
	Line int `json:"line" description:"The line the symbol is at, counted from 1."`
	// Column is the column of the symbol, counted from 1.
	Column int `json:"column" description:"The column the symbol is at, counted from 1. Use 1 when the column is not known."`
	// NewName is the name the symbol gets.
	NewName string `json:"new_name" description:"The new name of the symbol."`
}

// RegisterCodeTools registers the tools that read and change the code
// of the application.
//
// The reading tools come first, so that the model sees them before the
// one that changes the code: the order of the tool list is the order
// the model reads.
//
// The renaming tool is wrapped in a permission gate. A nil gate means
// that it runs unguarded, which is only appropriate for a tool that
// the caller trusts: a rename changes the code of the user.
func RegisterCodeTools(registry *Registry, manager *code.Manager, gate *Gate) {
	if registry == nil || manager == nil {
		return
	}

	registry.Register(NewCodeSymbolsTool(manager))
	registry.Register(NewCodeDefinitionTool(manager))
	registry.Register(NewCodeReferencesTool(manager))

	registry.Register(NewGuardedTool(NewCodeRenameTool(manager), gate, codeRenameApproval))
}

// codeRenameApproval is the approval the user reads.
//
// It says what the call would do, because the user has to decide
// without reading the arguments.
const codeRenameApproval = "Rename a symbol in the code of the application."

// codeTool is a tool that answers a question about the code.
//
// The arguments are decoded from the JSON the model produced, and the
// values are checked before they reach the language server: a model
// that invents a line number must not be able to read outside the
// code directory.
type codeTool[P any] struct {
	// manager owns the language server.
	manager *code.Manager
	// name is the tool name.
	name string
	// description is the prose the model reads.
	description string
	// run executes the tool with the decoded arguments.
	run func(ctx context.Context, manager *code.Manager, params *P) (string, error)
}

// Name implements [Tool].
func (t *codeTool[P]) Name() string { return t.name }

// Description implements [Tool].
func (t *codeTool[P]) Description() string { return t.description }

// Schema implements [Schemer].
func (t *codeTool[P]) Schema() (map[string]any, []string) {
	return schemaOf[P]()
}

// Run implements [Tool].
func (t *codeTool[P]) Run(ctx context.Context, input string) (string, error) {
	if t.manager == nil {
		return "", errors.New("agent: no code manager configured")
	}

	params := new(P)

	if strings.TrimSpace(input) != "" {
		if err := json.Unmarshal([]byte(input), params); err != nil {
			return "", fmt.Errorf("%w: %w", ErrInvalidToolInput, err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, toolTimeout)
	defer cancel()

	return t.run(ctx, t.manager, params)
}

// NewCodeSymbolsTool returns the tool that lists the declarations of a
// file.
func NewCodeSymbolsTool(manager *code.Manager) Tool {
	return &codeTool[CodeSymbolsParams]{
		manager:     manager,
		name:        ToolCodeSymbols,
		description: codeSymbolsDescription,
		run: func(ctx context.Context, manager *code.Manager, params *CodeSymbolsParams) (string, error) {
			symbols, err := manager.Symbols(ctx, params.Path)
			if err != nil {
				return "", err
			}

			if len(symbols) == 0 {
				return "the file declares nothing", nil
			}

			return formatSymbols(symbols, 0), nil
		},
	}
}

// NewCodeDefinitionTool returns the tool that finds where a symbol is
// defined.
func NewCodeDefinitionTool(manager *code.Manager) Tool {
	return &codeTool[CodePositionParams]{
		manager:     manager,
		name:        ToolCodeDefinition,
		description: codeDefinitionDescription,
		run: func(ctx context.Context, manager *code.Manager, params *CodePositionParams) (string, error) {
			locations, err := manager.Definition(ctx, params.Path, params.Line, params.Column)
			if err != nil {
				return "", err
			}

			return formatLocations("definition", locations), nil
		},
	}
}

// NewCodeReferencesTool returns the tool that finds where a symbol is
// used.
func NewCodeReferencesTool(manager *code.Manager) Tool {
	return &codeTool[CodePositionParams]{
		manager:     manager,
		name:        ToolCodeReferences,
		description: codeReferencesDescription,
		run: func(ctx context.Context, manager *code.Manager, params *CodePositionParams) (string, error) {
			locations, err := manager.References(ctx, params.Path, params.Line, params.Column)
			if err != nil {
				return "", err
			}

			return formatLocations("reference", locations), nil
		},
	}
}

// NewCodeRenameTool returns the tool that renames a symbol.
//
// The tool asks the language server for the edits, writes them, and
// reports what changed. The gate is in front of it, so the user has
// approved the rename before the file is touched.
func NewCodeRenameTool(manager *code.Manager) Tool {
	return &codeTool[CodeRenameParams]{
		manager:     manager,
		name:        ToolCodeRename,
		description: codeRenameDescription,
		run: func(ctx context.Context, manager *code.Manager, params *CodeRenameParams) (string, error) {
			edits, err := manager.Rename(ctx, params.Path, params.Line, params.Column, params.NewName)
			if err != nil {
				return "", err
			}

			if err := manager.Apply(edits); err != nil {
				return "", err
			}

			return formatEdits(edits, params.NewName), nil
		},
	}
}

// formatSymbols renders the declarations of a file as an outline.
func formatSymbols(symbols []code.Symbol, depth int) string {
	var b strings.Builder

	prefix := strings.Repeat("  ", depth)

	for _, symbol := range symbols {
		fmt.Fprintf(&b, "%s%s %s (line %d)", prefix, symbol.Kind, symbol.Name, symbol.Line)

		if symbol.Detail != "" {
			fmt.Fprintf(&b, " — %s", symbol.Detail)
		}

		b.WriteString("\n")

		if len(symbol.Children) > 0 {
			b.WriteString(formatSymbols(symbol.Children, depth+1))
		}
	}

	return b.String()
}

// formatLocations renders the places a symbol was found.
func formatLocations(kind string, locations []code.Location) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%d %s(s):\n\n", len(locations), kind)

	for _, location := range locations {
		fmt.Fprintf(&b, "%s\n", location)

		if location.Text != "" {
			fmt.Fprintf(&b, "  %s\n", strings.TrimSpace(location.Text))
		}
	}

	return b.String()
}

// formatEdits renders the changes a rename would make.
//
// The edits are shown before they are applied: the user reads what
// changes, and the change is a change to their code.
func formatEdits(edits []code.Edit, newName string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "the rename to %q changes %d place(s):\n\n", newName, len(edits))

	for _, edit := range edits {
		fmt.Fprintf(&b, "%s\n", edit)
	}

	return b.String()
}
