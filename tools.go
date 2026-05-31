package main

import (
	"context"
	"fmt"

	"github.com/isaacphi/mcp-language-server/internal/tools"
	"github.com/mark3labs/mcp-go/mcp"
)

func readOnlyTool() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		ReadOnlyHint: true,
	})
}

func destructiveTool() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{
		DestructiveHint: true,
	})
}

func (s *mcpServer) registerTools() error {
	coreLogger.Debug("Registering MCP tools")

	applyTextEditTool := mcp.NewTool("edit_file",
		mcp.WithDescription("Apply multiple text edits to a file."),
		destructiveTool(),
		mcp.WithArray("edits",
			mcp.Required(),
			mcp.Description("List of edits to apply"),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"startLine": map[string]any{
						"type":        "number",
						"description": "Start line to replace, inclusive, one-indexed",
					},
					"endLine": map[string]any{
						"type":        "number",
						"description": "End line to replace, inclusive, one-indexed",
					},
					"newText": map[string]any{
						"type":        "string",
						"description": "Replacement text. Replace with the new text. Leave blank to remove lines.",
					},
				},
				"required": []string{"startLine", "endLine"},
			}),
		),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("Path to the file to edit"),
		),
	)

	s.mcpServer.AddTool(applyTextEditTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract arguments
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}

		// Extract edits array
		editsArg, ok := request.Params.Arguments["edits"]
		if !ok {
			return mcp.NewToolResultError("edits is required"), nil
		}

		// Type assert and convert the edits
		editsArray, ok := editsArg.([]any)
		if !ok {
			return mcp.NewToolResultError("edits must be an array"), nil
		}

		var edits []tools.TextEdit
		for _, editItem := range editsArray {
			editMap, ok := editItem.(map[string]any)
			if !ok {
				return mcp.NewToolResultError("each edit must be an object"), nil
			}

			startLine, ok := editMap["startLine"].(float64)
			if !ok {
				return mcp.NewToolResultError("startLine must be a number"), nil
			}

			endLine, ok := editMap["endLine"].(float64)
			if !ok {
				return mcp.NewToolResultError("endLine must be a number"), nil
			}

			newText, _ := editMap["newText"].(string) // newText can be empty

			edits = append(edits, tools.TextEdit{
				StartLine: int(startLine),
				EndLine:   int(endLine),
				NewText:   newText,
			})
		}

		coreLogger.Debug("Executing edit_file for file: %s", filePath)
		response, err := tools.ApplyTextEdits(ctx, s.lspClient, filePath, edits)
		if err != nil {
			coreLogger.Error("Failed to apply edits: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to apply edits: %v", err)), nil
		}
		return mcp.NewToolResultText(response), nil
	})

	readDefinitionTool := mcp.NewTool("definition",
		mcp.WithDescription("Read the source code definition of a symbol (function, type, constant, etc.) from the codebase. Returns the complete implementation code where the symbol is defined."),
		readOnlyTool(),
		mcp.WithString("symbolName",
			mcp.Required(),
			mcp.Description("The name of the symbol whose definition you want to find (e.g. 'mypackage.MyFunction', 'MyType.MyMethod')"),
		),
	)

	s.mcpServer.AddTool(readDefinitionTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract arguments
		symbolName, ok := request.Params.Arguments["symbolName"].(string)
		if !ok {
			return mcp.NewToolResultError("symbolName must be a string"), nil
		}

		coreLogger.Debug("Executing definition for symbol: %s", symbolName)
		text, err := tools.ReadDefinition(ctx, s.lspClient, symbolName)
		if err != nil {
			coreLogger.Error("Failed to get definition: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to get definition: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	findReferencesTool := mcp.NewTool("references",
		mcp.WithDescription("Find all usages and references of a symbol throughout the codebase. Returns a list of all files and locations where the symbol appears."),
		readOnlyTool(),
		mcp.WithString("symbolName",
			mcp.Required(),
			mcp.Description("The name of the symbol to search for (e.g. 'mypackage.MyFunction', 'MyType')"),
		),
	)

	s.mcpServer.AddTool(findReferencesTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract arguments
		symbolName, ok := request.Params.Arguments["symbolName"].(string)
		if !ok {
			return mcp.NewToolResultError("symbolName must be a string"), nil
		}

		coreLogger.Debug("Executing references for symbol: %s", symbolName)
		text, err := tools.FindReferences(ctx, s.lspClient, symbolName)
		if err != nil {
			coreLogger.Error("Failed to find references: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to find references: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	definitionAtPositionTool := mcp.NewTool("definition_at_position",
		mcp.WithDescription("Read the source code definition for the symbol at an exact file position."),
		readOnlyTool(),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Path to the file containing the symbol")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("1-indexed line number")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("1-indexed column number")),
	)

	s.mcpServer.AddTool(definitionAtPositionTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := requiredStringArg(request, "filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		line, err := intArg(request, "line", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		column, err := intArg(request, "column", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		text, err := tools.DefinitionAtPosition(ctx, s.lspClient, filePath, line, column)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get definition: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	typeDefinitionTool := mcp.NewTool("type_definition_at_position",
		mcp.WithDescription("Find the type definition for the symbol at an exact file position."),
		readOnlyTool(),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Path to the file containing the symbol")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("1-indexed line number")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("1-indexed column number")),
	)

	s.mcpServer.AddTool(typeDefinitionTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := requiredStringArg(request, "filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		line, err := intArg(request, "line", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		column, err := intArg(request, "column", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		text, err := tools.TypeDefinitionAtPosition(ctx, s.lspClient, filePath, line, column)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get type definition: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	implementationTool := mcp.NewTool("implementation_at_position",
		mcp.WithDescription("Find implementations for the symbol at an exact file position."),
		readOnlyTool(),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Path to the file containing the symbol")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("1-indexed line number")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("1-indexed column number")),
	)

	s.mcpServer.AddTool(implementationTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := requiredStringArg(request, "filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		line, err := intArg(request, "line", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		column, err := intArg(request, "column", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		text, err := tools.ImplementationAtPosition(ctx, s.lspClient, filePath, line, column)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get implementation: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	referencesAtPositionTool := mcp.NewTool("references_at_position",
		mcp.WithDescription("Find references for the symbol at an exact file position."),
		readOnlyTool(),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Path to the file containing the symbol")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("1-indexed line number")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("1-indexed column number")),
		mcp.WithBoolean("includeDeclaration", mcp.Description("If true, includes the symbol declaration in results"), mcp.DefaultBool(false)),
		mcp.WithNumber("contextLines", mcp.Description("Lines to include around each reference")),
	)

	s.mcpServer.AddTool(referencesAtPositionTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := requiredStringArg(request, "filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		line, err := intArg(request, "line", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		column, err := intArg(request, "column", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		contextLines, err := intArg(request, "contextLines", 5)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		includeDeclaration := boolArg(request, "includeDeclaration", false)

		text, err := tools.FindReferencesAtPosition(ctx, s.lspClient, filePath, line, column, includeDeclaration, contextLines)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to find references: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	documentSymbolsTool := mcp.NewTool("document_symbols",
		mcp.WithDescription("List the symbol tree for a file."),
		readOnlyTool(),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Path to the file to inspect")),
	)

	s.mcpServer.AddTool(documentSymbolsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := requiredStringArg(request, "filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.ListDocumentSymbols(ctx, s.lspClient, filePath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get document symbols: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	workspaceSymbolsTool := mcp.NewTool("workspace_symbols",
		mcp.WithDescription("Search workspace symbols by query. Omit query or pass an empty string to request the full workspace symbol surface supported by the language server."),
		readOnlyTool(),
		mcp.WithString("query", mcp.Description("Symbol query string. Empty requests all workspace symbols from servers that support it.")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of symbols to return. Omit or pass 0 to return all results.")),
	)

	s.mcpServer.AddTool(workspaceSymbolsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query := ""
		if rawQuery, ok := request.Params.Arguments["query"]; ok {
			query, ok = rawQuery.(string)
			if !ok {
				return mcp.NewToolResultError("query must be a string"), nil
			}
		}
		limit, err := intArg(request, "limit", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.ListWorkspaceSymbols(ctx, s.lspClient, query, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get workspace symbols: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	signatureHelpTool := mcp.NewTool("signature_help",
		mcp.WithDescription("Get callable signature help at an exact file position."),
		readOnlyTool(),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Path to the file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("1-indexed line number")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("1-indexed column number")),
	)

	s.mcpServer.AddTool(signatureHelpTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := requiredStringArg(request, "filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		line, err := intArg(request, "line", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		column, err := intArg(request, "column", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetSignatureHelp(ctx, s.lspClient, filePath, line, column)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get signature help: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	inlayHintsTool := mcp.NewTool("inlay_hints",
		mcp.WithDescription("Get inlay hints for a line range in a file."),
		readOnlyTool(),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Path to the file")),
		mcp.WithNumber("startLine", mcp.Required(), mcp.Description("1-indexed start line")),
		mcp.WithNumber("endLine", mcp.Required(), mcp.Description("1-indexed end line")),
	)

	s.mcpServer.AddTool(inlayHintsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := requiredStringArg(request, "filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		startLine, err := intArg(request, "startLine", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		endLine, err := intArg(request, "endLine", startLine)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetInlayHints(ctx, s.lspClient, filePath, startLine, endLine)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get inlay hints: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	completionsTool := mcp.NewTool("completions",
		mcp.WithDescription("Get completion candidates at an exact file position."),
		readOnlyTool(),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Path to the file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("1-indexed line number")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("1-indexed column number")),
		mcp.WithNumber("limit", mcp.Description("Maximum number of completions to return. Defaults to 50 when omitted or set to 0.")),
	)

	s.mcpServer.AddTool(completionsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := requiredStringArg(request, "filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		line, err := intArg(request, "line", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		column, err := intArg(request, "column", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		limit, err := intArg(request, "limit", 50)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetCompletions(ctx, s.lspClient, filePath, line, column, limit)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get completions: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	codeActionsTool := mcp.NewTool("code_actions",
		mcp.WithDescription("List code actions available for a file range."),
		readOnlyTool(),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Path to the file")),
		mcp.WithNumber("startLine", mcp.Required(), mcp.Description("1-indexed start line")),
		mcp.WithNumber("startColumn", mcp.Required(), mcp.Description("1-indexed start column")),
		mcp.WithNumber("endLine", mcp.Required(), mcp.Description("1-indexed end line")),
		mcp.WithNumber("endColumn", mcp.Required(), mcp.Description("1-indexed end column")),
	)

	s.mcpServer.AddTool(codeActionsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, err := requiredStringArg(request, "filePath")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		startLine, err := intArg(request, "startLine", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		startColumn, err := intArg(request, "startColumn", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		endLine, err := intArg(request, "endLine", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		endColumn, err := intArg(request, "endColumn", 0)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := tools.GetCodeActions(ctx, s.lspClient, filePath, startLine, startColumn, endLine, endColumn)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to get code actions: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	getDiagnosticsTool := mcp.NewTool("diagnostics",
		mcp.WithDescription("Get diagnostic information for a specific file from the language server."),
		readOnlyTool(),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file to get diagnostics for"),
		),
		mcp.WithNumber("contextLines",
			mcp.Description("Lines to include around each diagnostic."),
		),
		mcp.WithBoolean("showLineNumbers",
			mcp.Description("If true, adds line numbers to the output"),
			mcp.DefaultBool(true),
		),
	)

	s.mcpServer.AddTool(getDiagnosticsTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract arguments
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}

		contextLines := 5 // default value
		switch contextLinesArg := request.Params.Arguments["contextLines"].(type) {
		case float64:
			contextLines = int(contextLinesArg)
		case int:
			contextLines = contextLinesArg
		}

		showLineNumbers := true // default value
		if showLineNumbersArg, ok := request.Params.Arguments["showLineNumbers"].(bool); ok {
			showLineNumbers = showLineNumbersArg
		}

		coreLogger.Debug("Executing diagnostics for file: %s", filePath)
		text, err := tools.GetDiagnosticsForFile(ctx, s.lspClient, filePath, contextLines, showLineNumbers)
		if err != nil {
			coreLogger.Error("Failed to get diagnostics: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to get diagnostics: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	getCodeLensTool := mcp.NewTool("get_codelens",
		mcp.WithDescription("Get read-only code lens hints for a given file from the language server."),
		readOnlyTool(),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file to get code lens information for"),
		),
	)

	s.mcpServer.AddTool(getCodeLensTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}

		coreLogger.Debug("Executing get_codelens for file: %s", filePath)
		text, err := tools.GetCodeLens(ctx, s.lspClient, filePath)
		if err != nil {
			coreLogger.Error("Failed to get code lens: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to get code lens: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	// execute_codelens remains disabled because it runs LSP commands and may mutate the workspace.
	// executeCodeLensTool := mcp.NewTool("execute_codelens",
	// 	mcp.WithDescription("Execute a code lens command for a given file and lens index."),
	// 	mcp.WithString("filePath",
	// 		mcp.Required(),
	// 		mcp.Description("The path to the file containing the code lens to execute"),
	// 	),
	// 	mcp.WithNumber("index",
	// 		mcp.Required(),
	// 		mcp.Description("The index of the code lens to execute (from get_codelens output), 1 indexed"),
	// 	),
	// )
	//
	// s.mcpServer.AddTool(executeCodeLensTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// 	// Extract arguments
	// 	filePath, ok := request.Params.Arguments["filePath"].(string)
	// 	if !ok {
	// 		return mcp.NewToolResultError("filePath must be a string"), nil
	// 	}
	//
	// 	// Handle both float64 and int for index due to JSON parsing
	// 	var index int
	// 	switch v := request.Params.Arguments["index"].(type) {
	// 	case float64:
	// 		index = int(v)
	// 	case int:
	// 		index = v
	// 	default:
	// 		return mcp.NewToolResultError("index must be a number"), nil
	// 	}
	//
	// 	coreLogger.Debug("Executing execute_codelens for file: %s index: %d", filePath, index)
	// 	text, err := tools.ExecuteCodeLens(s.ctx, s.lspClient, filePath, index)
	// 	if err != nil {
	// 		coreLogger.Error("Failed to execute code lens: %v", err)
	// 		return mcp.NewToolResultError(fmt.Sprintf("failed to execute code lens: %v", err)), nil
	// 	}
	// 	return mcp.NewToolResultText(text), nil
	// })

	hoverTool := mcp.NewTool("hover",
		mcp.WithDescription("Get hover information (type, documentation) for a symbol at the specified position."),
		readOnlyTool(),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file to get hover information for"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number where the hover is requested (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number where the hover is requested (1-indexed)"),
		),
	)

	s.mcpServer.AddTool(hoverTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract arguments
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}

		// Handle both float64 and int for line and column due to JSON parsing
		var line, column int
		switch v := request.Params.Arguments["line"].(type) {
		case float64:
			line = int(v)
		case int:
			line = v
		default:
			return mcp.NewToolResultError("line must be a number"), nil
		}

		switch v := request.Params.Arguments["column"].(type) {
		case float64:
			column = int(v)
		case int:
			column = v
		default:
			return mcp.NewToolResultError("column must be a number"), nil
		}

		coreLogger.Debug("Executing hover for file: %s line: %d column: %d", filePath, line, column)
		text, err := tools.GetHoverInfo(ctx, s.lspClient, filePath, line, column)
		if err != nil {
			coreLogger.Error("Failed to get hover information: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to get hover information: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	renameSymbolTool := mcp.NewTool("rename_symbol",
		mcp.WithDescription("Rename a symbol (variable, function, class, etc.) at the specified position and update all references throughout the codebase."),
		destructiveTool(),
		mcp.WithString("filePath",
			mcp.Required(),
			mcp.Description("The path to the file containing the symbol to rename"),
		),
		mcp.WithNumber("line",
			mcp.Required(),
			mcp.Description("The line number where the symbol is located (1-indexed)"),
		),
		mcp.WithNumber("column",
			mcp.Required(),
			mcp.Description("The column number where the symbol is located (1-indexed)"),
		),
		mcp.WithString("newName",
			mcp.Required(),
			mcp.Description("The new name for the symbol"),
		),
	)

	s.mcpServer.AddTool(renameSymbolTool, func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Extract arguments
		filePath, ok := request.Params.Arguments["filePath"].(string)
		if !ok {
			return mcp.NewToolResultError("filePath must be a string"), nil
		}

		newName, ok := request.Params.Arguments["newName"].(string)
		if !ok {
			return mcp.NewToolResultError("newName must be a string"), nil
		}

		// Handle both float64 and int for line and column due to JSON parsing
		var line, column int
		switch v := request.Params.Arguments["line"].(type) {
		case float64:
			line = int(v)
		case int:
			line = v
		default:
			return mcp.NewToolResultError("line must be a number"), nil
		}

		switch v := request.Params.Arguments["column"].(type) {
		case float64:
			column = int(v)
		case int:
			column = v
		default:
			return mcp.NewToolResultError("column must be a number"), nil
		}

		coreLogger.Debug("Executing rename_symbol for file: %s line: %d column: %d newName: %s", filePath, line, column, newName)
		text, err := tools.RenameSymbol(ctx, s.lspClient, filePath, line, column, newName)
		if err != nil {
			coreLogger.Error("Failed to rename symbol: %v", err)
			return mcp.NewToolResultError(fmt.Sprintf("failed to rename symbol: %v", err)), nil
		}
		return mcp.NewToolResultText(text), nil
	})

	coreLogger.Info("Successfully registered all MCP tools")
	return nil
}

func requiredStringArg(request mcp.CallToolRequest, name string) (string, error) {
	value, ok := request.Params.Arguments[name].(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return value, nil
}

func intArg(request mcp.CallToolRequest, name string, defaultValue int) (int, error) {
	raw, ok := request.Params.Arguments[name]
	if !ok {
		return defaultValue, nil
	}

	switch v := raw.(type) {
	case float64:
		return int(v), nil
	case int:
		return v, nil
	case int64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("%s must be a number", name)
	}
}

func boolArg(request mcp.CallToolRequest, name string, defaultValue bool) bool {
	value, ok := request.Params.Arguments[name].(bool)
	if !ok {
		return defaultValue
	}
	return value
}
