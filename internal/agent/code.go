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
	// ToolCodeDiagnostics reports the problems of a file.
	ToolCodeDiagnostics = "code_diagnostics"
	// ToolCodeCallHierarchy reports who calls a symbol, or what it
	// calls.
	ToolCodeCallHierarchy = "code_call_hierarchy"
	// ToolCodeReplaceSymbol replaces a declaration.
	ToolCodeReplaceSymbol = "code_replace_symbol"
	// ToolCodeCreateFile creates a file.
	ToolCodeCreateFile = "code_create_file"
	// ToolCodeMoveFile moves a file.
	ToolCodeMoveFile = "code_move_file"
	// ToolCodeDeleteFile removes a file.
	ToolCodeDeleteFile = "code_delete_file"
	// ToolCodeSearch searches the symbols of the workspace.
	ToolCodeSearch = "code_search"
	// ToolCodeFindReferences finds the uses of a symbol.
	ToolCodeFindReferences = "code_find_references"
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

	codeDiagnosticsDescription = "Report the problems the compiler found in the " +
		"code: the errors, the warnings and the hints. " +
		"Use it after a change to the code, to know whether it still builds."

	codeCallHierarchyDescription = "Report who calls a symbol, or what a symbol " +
		"calls. " +
		"The symbol is the one at the given position of the given file: " +
		"the position of a use of it is enough. " +
		"Use it when you need to know what depends on a function, or what " +
		"a function does."

	codeReplaceSymbolDescription = "Replace a declaration in the code of the " +
		"application. " +
		"The symbol is named, and the action says what to do with it: " +
		"\"replace\" writes the new text over it, \"add_before\" and " +
		"\"add_after\" insert the text around it, and \"delete\" removes it. " +
		"Use it when the user asks for a change that a rename cannot make."

	codeCreateFileDescription = "Create an empty file in the code of the " +
		"application. " +
		"Use it when the user asks for a new file. The parent directories " +
		"are created when they do not exist."

	codeMoveFileDescription = "Move a file of the code of the application to " +
		"another path. " +
		"Use it when a file has to be renamed or moved. The destination " +
		"must not exist: a file that is already there is not replaced."

	codeDeleteFileDescription = "Remove a file of the code of the application. " +
		"Use it when the user asks for a file to be deleted. " +
		"A directory is removed with everything in it when recursive is true."

	codeSearchDescription = "Search the symbols of the whole code of the " +
		"application by name: the functions, the types, the methods and the " +
		"variables. " +
		"The query is a name or a part of one, and it is matched in a " +
		"relaxed way. " +
		"Use it when you know what a symbol is called but not where it is."

	codeFindReferencesDescription = "Find every place where a symbol is used, " +
		"in every file of the code of the application. " +
		"The symbol is named, and the search covers the whole workspace. " +
		"Use it before you change a symbol, to see what depends on it."
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

// CodeDiagnosticsParams are the arguments of the code_diagnostics tool.
type CodeDiagnosticsParams struct {
	// Path is the path of the file, relative to the code directory.
	//
	// It is empty for the problems of every file.
	Path string `json:"path,omitempty" description:"The path of the file, relative to the code directory of the application. Leave it empty for the problems of every file."`
}

// CodeCallHierarchyParams are the arguments of the code_call_hierarchy
// tool.
type CodeCallHierarchyParams struct {
	// Path is the path of the file, relative to the code directory.
	Path string `json:"path" description:"The path of the file, relative to the code directory of the application."`
	// Line is the line of the symbol, counted from 1.
	Line int `json:"line" description:"The line the symbol is at, counted from 1."`
	// Column is the column of the symbol, counted from 1.
	Column int `json:"column" description:"The column the symbol is at, counted from 1. Use 1 when the column is not known."`
	// Direction says what to report: "incoming" for the callers of the
	// symbol, "outgoing" for what it calls.
	Direction string `json:"direction" description:"Either 'incoming' (who calls this) or 'outgoing' (what this calls)."`
}

// CodeReplaceSymbolParams are the arguments of the code_replace_symbol
// tool.
type CodeReplaceSymbolParams struct {
	// Path is the path of the file, relative to the code directory.
	Path string `json:"path" description:"The path of the file, relative to the code directory of the application."`
	// Symbol is the name of the declaration.
	Symbol string `json:"symbol" description:"The name of the declaration to replace: a function, a type, a method or a variable."`
	// Action says what to do with the declaration.
	Action string `json:"action" description:"What to do: 'replace' writes the text over it, 'add_before' and 'add_after' insert the text around it, and 'delete' removes it."`
	// Replacement is the text that is written.
	Replacement string `json:"replacement,omitempty" description:"The text to write. It is required by 'replace', 'add_before' and 'add_after', and ignored by 'delete'."`
}

// CodeCreateFileParams are the arguments of the code_create_file tool.
type CodeCreateFileParams struct {
	// Path is the path of the file, relative to the code directory.
	Path string `json:"path" description:"The path of the file to create, relative to the code directory of the application."`
}

// CodeMoveFileParams are the arguments of the code_move_file tool.
type CodeMoveFileParams struct {
	// From is the path of the file, relative to the code directory.
	From string `json:"from" description:"The path of the file to move, relative to the code directory of the application."`
	// To is the path of the destination, relative to the code
	// directory.
	To string `json:"to" description:"The path the file is moved to, relative to the code directory of the application."`
}

// CodeDeleteFileParams are the arguments of the code_delete_file tool.
type CodeDeleteFileParams struct {
	// Path is the path of the file, relative to the code directory.
	Path string `json:"path" description:"The path of the file to remove, relative to the code directory of the application."`
	// Recursive removes a directory with everything in it.
	Recursive bool `json:"recursive,omitempty" description:"Remove a directory with everything in it."`
}

// CodeSearchParams are the arguments of the code_search tool.
type CodeSearchParams struct {
	// Query is the name to look for.
	Query string `json:"query" description:"The name of the symbol to look for, or a part of it."`
}

// CodeFindReferencesParams are the arguments of the
// code_find_references tool.
type CodeFindReferencesParams struct {
	// Symbol is the name of the symbol.
	Symbol string `json:"symbol" description:"The name of the symbol to find the uses of."`
}

// RegisterCodeTools registers the tools that read and change the code
// of the application.
//
// The reading tools come first, so that the model sees them before the
// ones that change the code: the order of the tool list is the order
// the model reads.
//
// The tools that change the code are wrapped in a permission gate. A
// nil gate means that they run unguarded, which is only appropriate
// for a tool that the caller trusts: a rename or a replacement changes
// the code of the user.
func RegisterCodeTools(registry *Registry, manager *code.Manager, gate *Gate) {
	if registry == nil || manager == nil {
		return
	}

	registry.Register(NewCodeSymbolsTool(manager))
	registry.Register(NewCodeDefinitionTool(manager))
	registry.Register(NewCodeReferencesTool(manager))
	registry.Register(NewCodeDiagnosticsTool(manager))
	registry.Register(NewCodeCallHierarchyTool(manager))
	registry.Register(NewCodeSearchTool(manager))
	registry.Register(NewCodeFindReferencesTool(manager))

	registry.Register(NewGuardedTool(NewCodeRenameTool(manager), gate, codeRenameApproval))
	registry.Register(NewGuardedTool(NewCodeReplaceSymbolTool(manager), gate, codeReplaceApproval))
	registry.Register(NewGuardedTool(NewCodeCreateFileTool(manager), gate, codeCreateFileApproval))
	registry.Register(NewGuardedTool(NewCodeMoveFileTool(manager), gate, codeMoveFileApproval))
	registry.Register(NewGuardedTool(NewCodeDeleteFileTool(manager), gate, codeDeleteFileApproval))
}

// The approvals the user reads.
//
// They say what the call would do, because the user has to decide
// without reading the arguments.
const (
	codeRenameApproval     = "Rename a symbol in the code of the application."
	codeReplaceApproval    = "Change a declaration in the code of the application."
	codeCreateFileApproval = "Create a file in the code of the application."
	codeMoveFileApproval   = "Move a file of the code of the application."
	codeDeleteFileApproval = "Remove a file of the code of the application."
)

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
// The tool asks the language server for the changes, writes them, and
// reports what changed. The gate is in front of it, so the user has
// approved the rename before the file is touched.
//
// The server may ask for a file to be created, moved or removed along
// with the rename. Those operations are gated as well: they change
// the files of the user, exactly like the text does.
func NewCodeRenameTool(manager *code.Manager) Tool {
	return &codeTool[CodeRenameParams]{
		manager:     manager,
		name:        ToolCodeRename,
		description: codeRenameDescription,
		run: func(ctx context.Context, manager *code.Manager, params *CodeRenameParams) (string, error) {
			edits, operations, err := manager.Rename(
				ctx,
				params.Path,
				params.Line,
				params.Column,
				params.NewName,
			)
			if err != nil {
				return "", err
			}

			if err := manager.Apply(edits); err != nil {
				return "", err
			}

			if err := manager.ApplyFileOperations(operations); err != nil {
				return "", err
			}

			return formatEdits(edits, operations, params.NewName), nil
		},
	}
}

// NewCodeDiagnosticsTool returns the tool that reports the problems
// of the code.
//
// The problems are the ones the language server found: the errors of
// the compiler, and the warnings it holds.
func NewCodeDiagnosticsTool(manager *code.Manager) Tool {
	return &codeTool[CodeDiagnosticsParams]{
		manager:     manager,
		name:        ToolCodeDiagnostics,
		description: codeDiagnosticsDescription,
		run: func(ctx context.Context, manager *code.Manager, params *CodeDiagnosticsParams) (string, error) {
			var items []code.Diagnostic

			var err error

			if params.Path == "" {
				items, err = manager.AllDiagnostics(ctx)
			} else {
				items, err = manager.Diagnostics(ctx, params.Path)
			}

			if err != nil {
				return "", err
			}

			if len(items) == 0 {
				return "the code has no problem", nil
			}

			return formatDiagnostics(items), nil
		},
	}
}

// NewCodeCallHierarchyTool returns the tool that reports who calls a
// symbol, or what a symbol calls.
func NewCodeCallHierarchyTool(manager *code.Manager) Tool {
	return &codeTool[CodeCallHierarchyParams]{
		manager:     manager,
		name:        ToolCodeCallHierarchy,
		description: codeCallHierarchyDescription,
		run: func(ctx context.Context, manager *code.Manager, params *CodeCallHierarchyParams) (string, error) {
			var calls []code.Call

			var err error

			switch params.Direction {
			case "incoming":
				calls, err = manager.Callers(ctx, params.Path, params.Line, params.Column)
			case "outgoing":
				calls, err = manager.Callees(ctx, params.Path, params.Line, params.Column)
			default:
				return "", fmt.Errorf(
					"%w: the direction is %q, want \"incoming\" or \"outgoing\"",
					ErrInvalidToolInput,
					params.Direction,
				)
			}

			if err != nil {
				return "", err
			}

			return formatCalls(params.Direction, calls), nil
		},
	}
}

// NewCodeReplaceSymbolTool returns the tool that replaces a
// declaration.
//
// The tool is behind the permission gate: a replacement is a change to
// the code of the user, exactly like a rename is.
func NewCodeReplaceSymbolTool(manager *code.Manager) Tool {
	return &codeTool[CodeReplaceSymbolParams]{
		manager:     manager,
		name:        ToolCodeReplaceSymbol,
		description: codeReplaceSymbolDescription,
		run: func(ctx context.Context, manager *code.Manager, params *CodeReplaceSymbolParams) (string, error) {
			content, err := manager.ReplaceSymbol(
				ctx,
				params.Path,
				params.Symbol,
				params.Action,
				params.Replacement,
			)
			if err != nil {
				return "", err
			}

			if err := manager.ApplyContent(params.Path, content); err != nil {
				return "", err
			}

			return fmt.Sprintf(
				"the %s of %q in %s is done",
				params.Action,
				params.Symbol,
				params.Path,
			), nil
		},
	}
}

// NewCodeCreateFileTool returns the tool that creates a file.
//
// The tool is behind the permission gate: a file that appears is a
// change to the code of the user.
func NewCodeCreateFileTool(manager *code.Manager) Tool {
	return &codeTool[CodeCreateFileParams]{
		manager:     manager,
		name:        ToolCodeCreateFile,
		description: codeCreateFileDescription,
		run: func(_ context.Context, manager *code.Manager, params *CodeCreateFileParams) (string, error) {
			if err := manager.CreateFile(params.Path); err != nil {
				return "", err
			}

			return fmt.Sprintf("the file %q is created", params.Path), nil
		},
	}
}

// NewCodeMoveFileTool returns the tool that moves a file.
//
// The tool is behind the permission gate: a file that moves is a
// change to the code of the user.
func NewCodeMoveFileTool(manager *code.Manager) Tool {
	return &codeTool[CodeMoveFileParams]{
		manager:     manager,
		name:        ToolCodeMoveFile,
		description: codeMoveFileDescription,
		run: func(_ context.Context, manager *code.Manager, params *CodeMoveFileParams) (string, error) {
			if err := manager.MoveFile(params.From, params.To); err != nil {
				return "", err
			}

			return fmt.Sprintf("the file %q is moved to %q", params.From, params.To), nil
		},
	}
}

// NewCodeDeleteFileTool returns the tool that removes a file.
//
// The tool is behind the permission gate: a file that disappears is a
// change to the code of the user.
func NewCodeDeleteFileTool(manager *code.Manager) Tool {
	return &codeTool[CodeDeleteFileParams]{
		manager:     manager,
		name:        ToolCodeDeleteFile,
		description: codeDeleteFileDescription,
		run: func(_ context.Context, manager *code.Manager, params *CodeDeleteFileParams) (string, error) {
			if err := manager.DeleteFile(params.Path, params.Recursive); err != nil {
				return "", err
			}

			return fmt.Sprintf("the file %q is removed", params.Path), nil
		},
	}
}

// NewCodeSearchTool returns the tool that searches the symbols of the
// workspace.
//
// The search is the one of the language server: it reads the
// declarations of the code, and not the lines that hold the name.
func NewCodeSearchTool(manager *code.Manager) Tool {
	return &codeTool[CodeSearchParams]{
		manager:     manager,
		name:        ToolCodeSearch,
		description: codeSearchDescription,
		run: func(ctx context.Context, manager *code.Manager, params *CodeSearchParams) (string, error) {
			results, err := manager.Search(ctx, params.Query)
			if err != nil {
				return "", err
			}

			if len(results) == 0 {
				return "no symbol matches the query", nil
			}

			return formatSearchResults(results), nil
		},
	}
}

// NewCodeFindReferencesTool returns the tool that finds the uses of a
// symbol over the whole workspace.
func NewCodeFindReferencesTool(manager *code.Manager) Tool {
	return &codeTool[CodeFindReferencesParams]{
		manager:     manager,
		name:        ToolCodeFindReferences,
		description: codeFindReferencesDescription,
		run: func(ctx context.Context, manager *code.Manager, params *CodeFindReferencesParams) (string, error) {
			locations, err := manager.ReferencesOf(ctx, params.Symbol)
			if err != nil {
				return "", err
			}

			return formatLocations("reference", locations), nil
		},
	}
}

// formatSearchResults renders the symbols that match a query.
func formatSearchResults(results []code.SearchResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%d symbol(s):\n\n", len(results))

	for _, result := range results {
		fmt.Fprintf(&b, "%s %s (line %d)", result.Kind, result.Name, result.Line)

		if result.Container != "" {
			fmt.Fprintf(&b, " in %s", result.Container)
		}

		fmt.Fprintf(&b, " — %s\n", result.Path)
	}

	return b.String()
}

// formatDiagnostics renders the problems of the code.
func formatDiagnostics(items []code.Diagnostic) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%d problem(s):\n\n", len(items))

	for _, item := range items {
		fmt.Fprintf(&b, "%s\n", item)
	}

	return b.String()
}

// formatCalls renders the callers or the callees of a symbol.
func formatCalls(direction string, calls []code.Call) string {
	var b strings.Builder

	noun := "caller"
	if direction == "outgoing" {
		noun = "callee"
	}

	if len(calls) == 0 {
		fmt.Fprintf(&b, "the symbol has no %s", noun)

		return b.String()
	}

	fmt.Fprintf(&b, "%d %s(s):\n\n", len(calls), noun)

	for _, call := range calls {
		fmt.Fprintf(&b, "%s\n", call)
	}

	return b.String()
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
//
// The file operations are shown too: a file that is created, moved or
// removed is a change the user has to see.
func formatEdits(edits []code.Edit, operations []code.FileOperation, newName string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "the rename to %q changes %d place(s):\n\n", newName, len(edits))

	for _, edit := range edits {
		fmt.Fprintf(&b, "%s\n", edit)
	}

	if len(operations) > 0 {
		fmt.Fprintf(&b, "\nand %d file(s):\n\n", len(operations))

		for _, operation := range operations {
			fmt.Fprintf(&b, "%s\n", operation)
		}
	}

	return b.String()
}
