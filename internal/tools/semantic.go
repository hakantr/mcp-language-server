package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/isaacphi/mcp-language-server/internal/lsp"
	"github.com/isaacphi/mcp-language-server/internal/protocol"
	"github.com/isaacphi/mcp-language-server/internal/utilities"
)

func DefinitionAtPosition(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, error) {
	filePath, params, err := textDocumentPosition(ctx, client, filePath, line, column)
	if err != nil {
		return "", err
	}

	result, err := client.Definition(ctx, protocol.DefinitionParams{TextDocumentPositionParams: params})
	if err != nil {
		return "", fmt.Errorf("failed to get definition: %w", err)
	}

	locations, err := definitionLikeLocations(result.Value)
	if err != nil {
		return "", err
	}

	return formatDefinitionLocations(ctx, client, fmt.Sprintf("Definitions for %s L%d:C%d", filePath, line, column), locations)
}

func TypeDefinitionAtPosition(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, error) {
	filePath, params, err := textDocumentPosition(ctx, client, filePath, line, column)
	if err != nil {
		return "", err
	}

	result, err := client.TypeDefinition(ctx, protocol.TypeDefinitionParams{TextDocumentPositionParams: params})
	if err != nil {
		return "", fmt.Errorf("failed to get type definition: %w", err)
	}

	locations, err := definitionLikeLocations(result.Value)
	if err != nil {
		return "", err
	}

	return formatDefinitionLocations(ctx, client, fmt.Sprintf("Type definitions for %s L%d:C%d", filePath, line, column), locations)
}

func ImplementationAtPosition(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, error) {
	filePath, params, err := textDocumentPosition(ctx, client, filePath, line, column)
	if err != nil {
		return "", err
	}

	result, err := client.Implementation(ctx, protocol.ImplementationParams{TextDocumentPositionParams: params})
	if err != nil {
		return "", fmt.Errorf("failed to get implementation: %w", err)
	}

	locations, err := definitionLikeLocations(result.Value)
	if err != nil {
		return "", err
	}

	return formatDefinitionLocations(ctx, client, fmt.Sprintf("Implementations for %s L%d:C%d", filePath, line, column), locations)
}

func FindReferencesAtPosition(ctx context.Context, client *lsp.Client, filePath string, line, column int, includeDeclaration bool, contextLines int) (string, error) {
	if envLines := os.Getenv("LSP_CONTEXT_LINES"); envLines != "" {
		if val, err := strconv.Atoi(envLines); err == nil && val >= 0 {
			contextLines = val
		}
	}

	filePath, params, err := textDocumentPosition(ctx, client, filePath, line, column)
	if err != nil {
		return "", err
	}

	refs, err := client.References(ctx, protocol.ReferenceParams{
		TextDocumentPositionParams: params,
		Context: protocol.ReferenceContext{
			IncludeDeclaration: includeDeclaration,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to get references: %w", err)
	}

	if len(refs) == 0 {
		return fmt.Sprintf("No references found for %s L%d:C%d", filePath, line, column), nil
	}

	return formatReferenceLocations(ctx, client, refs, contextLines)
}

func ListDocumentSymbols(ctx context.Context, client *lsp.Client, filePath string) (string, error) {
	filePath, err := utilities.ValidatePathInWorkspace(filePath)
	if err != nil {
		return "", err
	}
	if err := client.OpenFile(ctx, filePath); err != nil {
		return "", fmt.Errorf("could not open file: %w", err)
	}

	result, err := client.DocumentSymbol(ctx, protocol.DocumentSymbolParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.URIFromPath(filePath)},
	})
	if err != nil {
		return "", fmt.Errorf("failed to get document symbols: %w", err)
	}

	symbols, err := result.Results()
	if err != nil {
		return "", err
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("Document symbols for %s\n", filePath))
	sortDocumentSymbolResults(symbols)
	for _, symbol := range symbols {
		writeDocumentSymbol(&out, symbol, 0)
	}
	if len(symbols) == 0 {
		out.WriteString("No document symbols found.\n")
	}
	return out.String(), nil
}

func ListWorkspaceSymbols(ctx context.Context, client *lsp.Client, query string, limit int) (string, error) {
	result, err := client.Symbol(ctx, protocol.WorkspaceSymbolParams{Query: query})
	if err != nil {
		return "", fmt.Errorf("failed to fetch workspace symbols: %w", err)
	}

	symbols, err := result.Results()
	if err != nil {
		return "", err
	}

	sortWorkspaceSymbols(symbols)

	total := len(symbols)
	if limit > 0 && len(symbols) > limit {
		symbols = symbols[:limit]
	}

	var out strings.Builder
	if limit > 0 && total > len(symbols) {
		out.WriteString(fmt.Sprintf("Workspace symbols for query %q: showing %d of %d\n", query, len(symbols), total))
	} else {
		out.WriteString(fmt.Sprintf("Workspace symbols for query %q: %d\n", query, len(symbols)))
	}
	for _, symbol := range symbols {
		loc := symbol.GetLocation()
		out.WriteString(fmt.Sprintf("- %s", symbol.GetName()))
		if kind := workspaceSymbolKind(symbol); kind != "" {
			out.WriteString(fmt.Sprintf(" (%s)", kind))
		}
		if container := workspaceSymbolContainer(symbol); container != "" {
			out.WriteString(fmt.Sprintf(" in %s", container))
		}
		if tags := workspaceSymbolTags(symbol); len(tags) > 0 {
			out.WriteString(fmt.Sprintf(" [%s]", strings.Join(tags, ", ")))
		}
		if loc.URI != "" {
			out.WriteString(fmt.Sprintf(" - %s L%d:C%d\n", loc.URI.Path(), loc.Range.Start.Line+1, loc.Range.Start.Character+1))
		} else {
			out.WriteString("\n")
		}
	}
	return out.String(), nil
}

func GetSignatureHelp(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, error) {
	filePath, params, err := textDocumentPosition(ctx, client, filePath, line, column)
	if err != nil {
		return "", err
	}

	result, err := client.SignatureHelp(ctx, protocol.SignatureHelpParams{
		TextDocumentPositionParams: params,
		Context: &protocol.SignatureHelpContext{
			TriggerKind: protocol.SigInvoked,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to get signature help: %w", err)
	}

	if len(result.Signatures) == 0 {
		return fmt.Sprintf("No signature help found for %s L%d:C%d", filePath, line, column), nil
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("Signature help for %s L%d:C%d\n", filePath, line, column))
	out.WriteString(fmt.Sprintf("Active signature: %d\nActive parameter: %d\n\n", result.ActiveSignature+1, result.ActiveParameter+1))
	for i, sig := range result.Signatures {
		out.WriteString(fmt.Sprintf("[%d] %s\n", i+1, sig.Label))
		if doc := markupText(sig.Documentation); doc != "" {
			out.WriteString(doc + "\n")
		}
		for j, param := range sig.Parameters {
			out.WriteString(fmt.Sprintf("  - param %d: %s", j+1, parameterLabel(param.Label)))
			if doc := markupText(param.Documentation); doc != "" {
				out.WriteString(" - " + singleLine(doc))
			}
			out.WriteString("\n")
		}
	}
	return out.String(), nil
}

func GetInlayHints(ctx context.Context, client *lsp.Client, filePath string, startLine, endLine int) (string, error) {
	filePath, err := utilities.ValidatePathInWorkspace(filePath)
	if err != nil {
		return "", err
	}
	if err := client.OpenFile(ctx, filePath); err != nil {
		return "", fmt.Errorf("could not open file: %w", err)
	}
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}
	endPosition := inclusiveEndLinePosition(string(content), endLine, client.PositionEncoding())

	result, err := client.InlayHint(ctx, protocol.InlayHintParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.URIFromPath(filePath)},
		Range: protocol.Range{
			Start: protocol.Position{Line: uint32(startLine - 1), Character: 0},
			End:   endPosition,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to get inlay hints: %w", err)
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("Inlay hints for %s L%d-L%d: %d\n", filePath, startLine, endLine, len(result)))
	for _, hint := range result {
		out.WriteString(fmt.Sprintf("- L%d:C%d %s\n", hint.Position.Line+1, hint.Position.Character+1, inlayLabel(hint.Label)))
	}
	return out.String(), nil
}

func GetCompletions(ctx context.Context, client *lsp.Client, filePath string, line, column, limit int) (string, error) {
	filePath, params, err := textDocumentPosition(ctx, client, filePath, line, column)
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		limit = 50
	}

	result, err := client.Completion(ctx, protocol.CompletionParams{TextDocumentPositionParams: params})
	if err != nil {
		return "", fmt.Errorf("failed to get completions: %w", err)
	}

	items := completionItems(result)
	if len(items) > limit {
		items = items[:limit]
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("Completions for %s L%d:C%d: %d\n", filePath, line, column, len(items)))
	for _, item := range items {
		out.WriteString(fmt.Sprintf("- %s", item.Label))
		if item.Detail != "" {
			out.WriteString(" - " + singleLine(item.Detail))
		}
		out.WriteString("\n")
	}
	return out.String(), nil
}

func GetCodeActions(ctx context.Context, client *lsp.Client, filePath string, startLine, startColumn, endLine, endColumn int) (string, error) {
	filePath, err := utilities.ValidatePathInWorkspace(filePath)
	if err != nil {
		return "", err
	}
	if err := client.OpenFile(ctx, filePath); err != nil {
		return "", fmt.Errorf("could not open file: %w", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("error reading file: %w", err)
	}
	start, err := utilities.LineColumnToPosition(string(content), startLine, startColumn, client.PositionEncoding())
	if err != nil {
		return "", err
	}
	end, err := utilities.LineColumnToPosition(string(content), endLine, endColumn, client.PositionEncoding())
	if err != nil {
		return "", err
	}

	uri := protocol.URIFromPath(filePath)
	actions, err := client.CodeAction(ctx, protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        protocol.Range{Start: start, End: end},
		Context: protocol.CodeActionContext{
			Diagnostics: client.GetFileDiagnostics(uri),
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to get code actions: %w", err)
	}

	var out strings.Builder
	out.WriteString(fmt.Sprintf("Code actions for %s L%d:C%d-L%d:C%d: %d\n", filePath, startLine, startColumn, endLine, endColumn, len(actions)))
	for i, action := range actions {
		out.WriteString(fmt.Sprintf("[%d] %s\n", i+1, codeActionTitle(action)))
	}
	return out.String(), nil
}

func textDocumentPosition(ctx context.Context, client *lsp.Client, filePath string, line, column int) (string, protocol.TextDocumentPositionParams, error) {
	filePath, err := utilities.ValidatePathInWorkspace(filePath)
	if err != nil {
		return "", protocol.TextDocumentPositionParams{}, err
	}
	if err := client.OpenFile(ctx, filePath); err != nil {
		return "", protocol.TextDocumentPositionParams{}, fmt.Errorf("could not open file: %w", err)
	}

	position, err := client.PositionFromLineColumn(filePath, line, column)
	if err != nil {
		return "", protocol.TextDocumentPositionParams{}, err
	}

	return filePath, protocol.TextDocumentPositionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: protocol.URIFromPath(filePath)},
		Position:     position,
	}, nil
}

func definitionLikeLocations(value any) ([]protocol.Location, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case protocol.Definition:
		return definitionLocations(v)
	case protocol.Location:
		return []protocol.Location{v}, nil
	case []protocol.Location:
		return v, nil
	case []protocol.DefinitionLink:
		locations := make([]protocol.Location, 0, len(v))
		for _, link := range v {
			locations = append(locations, protocol.Location{
				URI:   link.TargetURI,
				Range: link.TargetSelectionRange,
			})
		}
		return locations, nil
	default:
		return nil, fmt.Errorf("unsupported definition result type: %T", value)
	}
}

func definitionLocations(def protocol.Definition) ([]protocol.Location, error) {
	switch v := def.Value.(type) {
	case nil:
		return nil, nil
	case protocol.Location:
		return []protocol.Location{v}, nil
	case []protocol.Location:
		return v, nil
	default:
		return nil, fmt.Errorf("unsupported definition type: %T", def.Value)
	}
}

func formatDefinitionLocations(ctx context.Context, client *lsp.Client, title string, locations []protocol.Location) (string, error) {
	if len(locations) == 0 {
		return title + "\nNo locations found.", nil
	}

	var out strings.Builder
	out.WriteString(title + "\n")
	for _, loc := range locations {
		filePath := loc.URI.Path()
		if err := client.OpenFile(ctx, filePath); err != nil {
			toolsLogger.Warn("could not open definition file %s: %v", filePath, err)
		}

		definition, fullLoc, err := GetFullDefinition(ctx, client, loc)
		if err != nil {
			definition, err = ExtractTextFromLocation(loc)
			fullLoc = loc
		}
		if err != nil {
			out.WriteString(fmt.Sprintf("---\n\nFile: %s\nRange: L%d:C%d - L%d:C%d\nError extracting source: %v\n",
				filePath,
				loc.Range.Start.Line+1,
				loc.Range.Start.Character+1,
				loc.Range.End.Line+1,
				loc.Range.End.Character+1,
				err,
			))
			continue
		}

		out.WriteString(fmt.Sprintf("---\n\nFile: %s\nRange: L%d:C%d - L%d:C%d\n\n%s\n",
			filePath,
			fullLoc.Range.Start.Line+1,
			fullLoc.Range.Start.Character+1,
			fullLoc.Range.End.Line+1,
			fullLoc.Range.End.Character+1,
			addLineNumbers(definition, int(fullLoc.Range.Start.Line)+1),
		))
	}
	return out.String(), nil
}

func formatReferenceLocations(ctx context.Context, client *lsp.Client, refs []protocol.Location, contextLines int) (string, error) {
	refsByFile := make(map[protocol.DocumentUri][]protocol.Location)
	for _, ref := range refs {
		refsByFile[ref.URI] = append(refsByFile[ref.URI], ref)
	}

	uris := make([]string, 0, len(refsByFile))
	for uri := range refsByFile {
		uris = append(uris, string(uri))
	}
	sort.Strings(uris)

	var out []string
	for _, uriStr := range uris {
		uri := protocol.DocumentUri(uriStr)
		fileRefs := refsByFile[uri]
		filePath := uri.Path()

		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			out = append(out, fmt.Sprintf("---\n\n%s\nReferences in File: %d\n\nError reading file: %v", filePath, len(fileRefs), err))
			continue
		}

		lines := strings.Split(string(fileContent), "\n")
		linesToShow, err := GetLineRangesToDisplay(ctx, client, fileRefs, len(lines), contextLines)
		if err != nil {
			linesToShow = make(map[int]bool)
			for _, ref := range fileRefs {
				linesToShow[int(ref.Range.Start.Line)] = true
			}
		}

		locStrings := make([]string, 0, len(fileRefs))
		for _, ref := range fileRefs {
			locStrings = append(locStrings, fmt.Sprintf("L%d:C%d", ref.Range.Start.Line+1, ref.Range.Start.Character+1))
		}

		formatted := fmt.Sprintf("---\n\n%s\nReferences in File: %d\nAt: %s\n\n%s",
			filePath,
			len(fileRefs),
			strings.Join(locStrings, ", "),
			FormatLinesWithRanges(lines, ConvertLinesToRanges(linesToShow, len(lines))),
		)
		out = append(out, formatted)
	}

	return strings.Join(out, "\n"), nil
}

func writeDocumentSymbol(out *strings.Builder, symbol protocol.DocumentSymbolResult, depth int) {
	indent := strings.Repeat("  ", depth)
	rng := symbol.GetRange()
	out.WriteString(fmt.Sprintf("%s- %s", indent, symbol.GetName()))

	if kind := documentSymbolKind(symbol); kind != "" {
		out.WriteString(fmt.Sprintf(" (%s)", kind))
	}
	if detail := documentSymbolDetail(symbol); detail != "" {
		out.WriteString(" - " + singleLine(detail))
	}
	if container := documentSymbolContainer(symbol); container != "" {
		out.WriteString(fmt.Sprintf(" in %s", container))
	}
	if tags := documentSymbolTags(symbol); len(tags) > 0 {
		out.WriteString(fmt.Sprintf(" [%s]", strings.Join(tags, ", ")))
	}

	out.WriteString(fmt.Sprintf(" - range L%d:C%d-L%d:C%d",
		rng.Start.Line+1,
		rng.Start.Character+1,
		rng.End.Line+1,
		rng.End.Character+1,
	))

	if selection, ok := documentSymbolSelectionRange(symbol); ok {
		out.WriteString(fmt.Sprintf(" selection L%d:C%d-L%d:C%d",
			selection.Start.Line+1,
			selection.Start.Character+1,
			selection.End.Line+1,
			selection.End.Character+1,
		))
	}
	out.WriteString("\n")

	if ds, ok := symbol.(*protocol.DocumentSymbol); ok {
		sortDocumentSymbols(ds.Children)
		for i := range ds.Children {
			writeDocumentSymbol(out, &ds.Children[i], depth+1)
		}
	}
}

func sortWorkspaceSymbols(symbols []protocol.WorkspaceSymbolResult) {
	sort.SliceStable(symbols, func(i, j int) bool {
		left := symbols[i]
		right := symbols[j]
		leftLoc := left.GetLocation()
		rightLoc := right.GetLocation()

		keys := []struct {
			left  string
			right string
		}{
			{string(leftLoc.URI), string(rightLoc.URI)},
			{fmt.Sprintf("%010d:%010d", leftLoc.Range.Start.Line, leftLoc.Range.Start.Character), fmt.Sprintf("%010d:%010d", rightLoc.Range.Start.Line, rightLoc.Range.Start.Character)},
			{workspaceSymbolKind(left), workspaceSymbolKind(right)},
			{left.GetName(), right.GetName()},
			{workspaceSymbolContainer(left), workspaceSymbolContainer(right)},
		}

		for _, key := range keys {
			if key.left != key.right {
				return key.left < key.right
			}
		}
		return false
	})
}

func sortDocumentSymbols(symbols []protocol.DocumentSymbol) {
	sort.SliceStable(symbols, func(i, j int) bool {
		left := symbols[i]
		right := symbols[j]
		if left.Range.Start.Line != right.Range.Start.Line {
			return left.Range.Start.Line < right.Range.Start.Line
		}
		if left.Range.Start.Character != right.Range.Start.Character {
			return left.Range.Start.Character < right.Range.Start.Character
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.Name < right.Name
	})
}

func sortDocumentSymbolResults(symbols []protocol.DocumentSymbolResult) {
	sort.SliceStable(symbols, func(i, j int) bool {
		left := symbols[i]
		right := symbols[j]
		leftRange := left.GetRange()
		rightRange := right.GetRange()
		if leftRange.Start.Line != rightRange.Start.Line {
			return leftRange.Start.Line < rightRange.Start.Line
		}
		if leftRange.Start.Character != rightRange.Start.Character {
			return leftRange.Start.Character < rightRange.Start.Character
		}
		if documentSymbolKind(left) != documentSymbolKind(right) {
			return documentSymbolKind(left) < documentSymbolKind(right)
		}
		return left.GetName() < right.GetName()
	})
}

func workspaceSymbolKind(symbol protocol.WorkspaceSymbolResult) string {
	switch value := symbol.(type) {
	case *protocol.WorkspaceSymbol:
		return symbolKind(value.Kind)
	case *protocol.SymbolInformation:
		return symbolKind(value.Kind)
	default:
		return ""
	}
}

func workspaceSymbolContainer(symbol protocol.WorkspaceSymbolResult) string {
	switch value := symbol.(type) {
	case *protocol.WorkspaceSymbol:
		return value.ContainerName
	case *protocol.SymbolInformation:
		return value.ContainerName
	default:
		return ""
	}
}

func workspaceSymbolTags(symbol protocol.WorkspaceSymbolResult) []string {
	switch value := symbol.(type) {
	case *protocol.WorkspaceSymbol:
		return symbolTags(value.Tags, false)
	case *protocol.SymbolInformation:
		return symbolTags(value.Tags, value.Deprecated)
	default:
		return nil
	}
}

func documentSymbolKind(symbol protocol.DocumentSymbolResult) string {
	switch value := symbol.(type) {
	case *protocol.DocumentSymbol:
		return symbolKind(value.Kind)
	case *protocol.SymbolInformation:
		return symbolKind(value.Kind)
	default:
		return ""
	}
}

func documentSymbolDetail(symbol protocol.DocumentSymbolResult) string {
	if value, ok := symbol.(*protocol.DocumentSymbol); ok {
		return value.Detail
	}
	return ""
}

func documentSymbolContainer(symbol protocol.DocumentSymbolResult) string {
	if value, ok := symbol.(*protocol.SymbolInformation); ok {
		return value.ContainerName
	}
	return ""
}

func documentSymbolTags(symbol protocol.DocumentSymbolResult) []string {
	switch value := symbol.(type) {
	case *protocol.DocumentSymbol:
		return symbolTags(value.Tags, value.Deprecated)
	case *protocol.SymbolInformation:
		return symbolTags(value.Tags, value.Deprecated)
	default:
		return nil
	}
}

func documentSymbolSelectionRange(symbol protocol.DocumentSymbolResult) (protocol.Range, bool) {
	if value, ok := symbol.(*protocol.DocumentSymbol); ok {
		return value.SelectionRange, true
	}
	return protocol.Range{}, false
}

func symbolKind(kind protocol.SymbolKind) string {
	if name := protocol.TableKindMap[kind]; name != "" {
		return name
	}
	return fmt.Sprintf("Kind%d", kind)
}

func symbolTags(tags []protocol.SymbolTag, deprecated bool) []string {
	out := make([]string, 0, len(tags)+1)
	seenDeprecated := false
	for _, tag := range tags {
		switch tag {
		case protocol.DeprecatedSymbol:
			out = append(out, "deprecated")
			seenDeprecated = true
		default:
			out = append(out, fmt.Sprintf("tag%d", tag))
		}
	}
	if deprecated && !seenDeprecated {
		out = append(out, "deprecated")
	}
	return out
}

func inclusiveEndLinePosition(content string, endLine int, encoding protocol.PositionEncodingKind) protocol.Position {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 {
		return protocol.Position{}
	}
	if endLine < 1 {
		endLine = 1
	}

	if endLine < len(lines) {
		return protocol.Position{Line: uint32(endLine), Character: 0}
	}

	lastLineIndex := len(lines) - 1
	lastLine := lines[lastLineIndex]
	return protocol.Position{
		Line:      uint32(lastLineIndex),
		Character: utilities.ByteOffsetToCharacterForEncoding(lastLine, len(lastLine), encoding),
	}
}

func markupText(raw any) string {
	if raw == nil {
		return ""
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Sprint(raw)
	}

	var wrapper struct {
		Value any `json:"value"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Value != nil {
		return markupText(wrapper.Value)
	}

	var markup protocol.MarkupContent
	if err := json.Unmarshal(data, &markup); err == nil && markup.Value != "" {
		return markup.Value
	}

	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		return text
	}

	return strings.TrimSpace(string(data))
}

func parameterLabel(label protocol.Or_ParameterInformation_label) string {
	switch v := label.Value.(type) {
	case string:
		return v
	case protocol.Tuple_ParameterInformation_label_Item1:
		return fmt.Sprintf("[%d,%d]", v.Fld0, v.Fld1)
	default:
		return fmt.Sprint(v)
	}
}

func inlayLabel(parts []protocol.InlayHintLabelPart) string {
	var out strings.Builder
	for _, part := range parts {
		out.WriteString(part.Value)
	}
	return out.String()
}

func completionItems(result protocol.Or_Result_textDocument_completion) []protocol.CompletionItem {
	switch v := result.Value.(type) {
	case protocol.CompletionList:
		return v.Items
	case []protocol.CompletionItem:
		return v
	default:
		return nil
	}
}

func codeActionTitle(action protocol.Or_Result_textDocument_codeAction_Item0_Elem) string {
	switch v := action.Value.(type) {
	case protocol.CodeAction:
		if v.Kind != "" {
			return fmt.Sprintf("%s (%s)", v.Title, v.Kind)
		}
		return v.Title
	case protocol.Command:
		return v.Title
	default:
		return fmt.Sprint(v)
	}
}

func singleLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
