package code

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

// DocumentSymbols returns the declarations of a file.
func (c *client) DocumentSymbols(ctx context.Context, path string) ([]protocol.DocumentSymbolResult, error) {
	params := protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: protocol.URIFromPath(path),
		},
	}

	var result protocol.Or_Result_textDocument_documentSymbol

	if err := c.Call(ctx, methodDocumentSymbol, params, &result); err != nil {
		return nil, fmt.Errorf("code: the symbols of %s: %w", path, err)
	}

	return result.Results() //nolint:wrapcheck
}

// Definition returns the places where a symbol is defined.
func (c *client) Definition(ctx context.Context, path string, line, character int) ([]protocol.Location, error) {
	var result json.RawMessage

	if err := c.Call(ctx, methodDefinition, c.positionParams(path, line, character), &result); err != nil {
		return nil, fmt.Errorf("code: the definition in %s: %w", path, err)
	}

	if string(result) == "null" {
		return nil, nil
	}

	// The server answers with a list of places, or with one.
	var locations []protocol.Location

	if err := json.Unmarshal(result, &locations); err == nil && len(locations) > 0 {
		return locations, nil
	}

	var location protocol.Location

	if err := json.Unmarshal(result, &location); err == nil && location.URI != "" {
		return []protocol.Location{location}, nil
	}

	return nil, nil
}

// References returns the places where a symbol is used.
//
// The search covers the whole workspace: the server knows the module,
// not only the file.
func (c *client) References(
	ctx context.Context,
	path string,
	line, character int,
	includeDeclaration bool,
) ([]protocol.Location, error) {
	params := protocol.ReferenceParams{
		TextDocumentPositionParams: c.positionParams(path, line, character),
		Context: protocol.ReferenceContext{
			IncludeDeclaration: includeDeclaration,
		},
	}

	var result []protocol.Location

	if err := c.Call(ctx, methodReferences, params, &result); err != nil {
		return nil, fmt.Errorf("code: the references in %s: %w", path, err)
	}

	return result, nil
}

// Rename returns the changes that rename a symbol.
func (c *client) Rename(
	ctx context.Context,
	path string,
	line, character int,
	newName string,
) (*protocol.WorkspaceEdit, error) {
	params := protocol.RenameParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: protocol.URIFromPath(path),
		},
		Position: protocol.Position{Line: uint32(line), Character: uint32(character)}, //nolint:gosec
		NewName:  newName,
	}

	var result protocol.WorkspaceEdit

	if err := c.Call(ctx, methodRename, params, &result); err != nil {
		return nil, fmt.Errorf("code: the rename in %s: %w", path, err)
	}

	return &result, nil
}

// WorkspaceSymbols returns the symbols of the workspace that match a
// query.
//
// The query is a name or a part of one: the server matches it in a
// relaxed way, and the results are the symbols of every file of the
// workspace.
//
// This is the request the client of the powernap package does not
// expose, and the reason the connection is owned here.
func (c *client) WorkspaceSymbols(
	ctx context.Context,
	query string,
) ([]protocol.WorkspaceSymbolResult, error) {
	params := protocol.WorkspaceSymbolParams{Query: query}

	var result protocol.Or_Result_workspace_symbol

	if err := c.Call(ctx, methodWorkspaceSymbol, params, &result); err != nil {
		return nil, fmt.Errorf("code: the symbols of the workspace: %w", err)
	}

	return result.Results() //nolint:wrapcheck
}

// PrepareCallHierarchy asks the server about the symbol at a place.
//
// The parameters are the ones of the protocol, and not the ones of a
// plain position: the call hierarchy carries the data of the symbol
// from the preparation to the calls.
func (c *client) PrepareCallHierarchy(
	ctx context.Context,
	path string,
	line, character int,
) ([]protocol.CallHierarchyItem, error) {
	params := protocol.CallHierarchyPrepareParams{
		TextDocumentPositionParams: c.positionParams(path, line, character),
	}

	var result []protocol.CallHierarchyItem

	err := c.Call(ctx, methodPrepareCallHierarc, params, &result)
	if err != nil {
		return nil, fmt.Errorf("code: the call hierarchy in %s: %w", path, err)
	}

	return result, nil
}

// IncomingCalls returns the calls that reach a symbol.
//
// The item is wrapped in the parameters of the protocol: the server
// reads the name, the kind and the place of the symbol out of it, and
// an item that is sent bare is an empty one.
func (c *client) IncomingCalls(
	ctx context.Context,
	item protocol.CallHierarchyItem,
) ([]protocol.CallHierarchyIncomingCall, error) {
	params := protocol.CallHierarchyIncomingCallsParams{Item: item}

	var result []protocol.CallHierarchyIncomingCall

	if err := c.Call(ctx, methodIncomingCalls, params, &result); err != nil {
		return nil, fmt.Errorf("code: the callers: %w", err)
	}

	return result, nil
}

// OutgoingCalls returns the calls a symbol makes.
func (c *client) OutgoingCalls(
	ctx context.Context,
	item protocol.CallHierarchyItem,
) ([]protocol.CallHierarchyOutgoingCall, error) {
	params := protocol.CallHierarchyOutgoingCallsParams{Item: item}

	var result []protocol.CallHierarchyOutgoingCall

	if err := c.Call(ctx, methodOutgoingCalls, params, &result); err != nil {
		return nil, fmt.Errorf("code: the callees: %w", err)
	}

	return result, nil
}

// positionParams returns the parameters of a request that carries a
// position in a file.
func (c *client) positionParams(path string, line, character int) protocol.TextDocumentPositionParams {
	return protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: protocol.URIFromPath(path),
		},
		Position: protocol.Position{
			Line:      uint32(line),      //nolint:gosec
			Character: uint32(character), //nolint:gosec
		},
	}
}
