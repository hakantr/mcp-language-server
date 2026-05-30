package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/isaacphi/mcp-language-server/internal/lsp"
	"github.com/isaacphi/mcp-language-server/internal/protocol"
)

// GetDiagnosticsForFile retrieves diagnostics for a specific file from the language server
func GetDiagnosticsForFile(ctx context.Context, client *lsp.Client, filePath string, contextLines int, showLineNumbers bool) (string, error) {
	// Override with environment variable if specified
	if envLines := os.Getenv("LSP_CONTEXT_LINES"); envLines != "" {
		if val, err := strconv.Atoi(envLines); err == nil && val >= 0 {
			contextLines = val
		}
	}

	err := client.OpenFile(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("could not open file: %v", err)
	}

	// Convert the file path to URI format
	uri := protocol.URIFromPath(filePath)

	// Request fresh diagnostics
	diagParams := protocol.DocumentDiagnosticParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
	}
	report, err := client.Diagnostic(ctx, diagParams)
	if err != nil {
		toolsLogger.Error("Failed to get diagnostics: %v", err)
		if waitErr := waitForServerProcessing(ctx, time.Second); waitErr != nil {
			return "", waitErr
		}
	} else {
		diagnostics, relatedDiagnostics, hasPrimaryDiagnostics := collectDiagnosticReports(uri, report)
		if hasPrimaryDiagnostics {
			client.SetFileDiagnostics(uri, diagnostics)
		}
		for relatedURI, related := range relatedDiagnostics {
			client.SetFileDiagnostics(relatedURI, related)
		}
	}

	// Get diagnostics from the cache
	diagnostics := client.GetFileDiagnostics(uri)

	if len(diagnostics) == 0 {
		return "No diagnostics found for " + filePath, nil
	}

	// Format file header
	fileInfo := fmt.Sprintf("%s\nDiagnostics in File: %d\n",
		filePath,
		len(diagnostics),
	)

	// Create a summary of all the diagnostics
	var diagSummaries []string
	var diagLocations []protocol.Location

	for _, diag := range diagnostics {
		severity := getSeverityString(diag.Severity)
		location := fmt.Sprintf("L%d:C%d",
			diag.Range.Start.Line+1,
			diag.Range.Start.Character+1)

		summary := fmt.Sprintf("%s at %s: %s",
			severity,
			location,
			diag.Message)

		// Add source and code if available
		if diag.Source != "" {
			summary += fmt.Sprintf(" (Source: %s", diag.Source)
			if diag.Code != nil {
				summary += fmt.Sprintf(", Code: %v", diag.Code)
			}
			summary += ")"
		} else if diag.Code != nil {
			summary += fmt.Sprintf(" (Code: %v)", diag.Code)
		}

		diagSummaries = append(diagSummaries, summary)

		// Create a location for this diagnostic to use with line ranges
		diagLocations = append(diagLocations, protocol.Location{
			URI:   uri,
			Range: diag.Range,
		})
	}

	// Format content with context
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return fileInfo + "\nError reading file: " + err.Error(), nil
	}

	lines := strings.Split(string(fileContent), "\n")

	// Collect lines to display
	var linesToShow map[int]bool
	if contextLines > 0 {
		// Use GetLineRangesToDisplay for context
		linesToShow, err = GetLineRangesToDisplay(ctx, client, diagLocations, len(lines), contextLines)
		if err != nil {
			// If error, just show the diagnostic lines
			linesToShow = make(map[int]bool)
			for _, diag := range diagnostics {
				linesToShow[int(diag.Range.Start.Line)] = true
			}
		}
	} else {
		// Just show the diagnostic lines
		linesToShow = make(map[int]bool)
		for _, diag := range diagnostics {
			linesToShow[int(diag.Range.Start.Line)] = true
		}
	}

	// Convert to line ranges
	lineRanges := ConvertLinesToRanges(linesToShow, len(lines))

	// Format with diagnostics summary in header
	result := fileInfo
	if len(diagSummaries) > 0 {
		result += strings.Join(diagSummaries, "\n") + "\n"
	}

	// Format the content with ranges
	if showLineNumbers {
		result += "\n" + FormatLinesWithRanges(lines, lineRanges)
	}

	return result, nil
}

func collectDiagnosticReports(uri protocol.DocumentUri, report protocol.DocumentDiagnosticReport) ([]protocol.Diagnostic, map[protocol.DocumentUri][]protocol.Diagnostic, bool) {
	related := make(map[protocol.DocumentUri][]protocol.Diagnostic)

	switch value := report.Value.(type) {
	case protocol.RelatedFullDocumentDiagnosticReport:
		for relatedURI, raw := range value.RelatedDocuments {
			if diagnostics, ok := diagnosticsFromRelatedReport(raw); ok {
				related[relatedURI] = diagnostics
			}
		}
		return value.Items, related, true
	case protocol.RelatedUnchangedDocumentDiagnosticReport:
		for relatedURI, raw := range value.RelatedDocuments {
			if diagnostics, ok := diagnosticsFromRelatedReport(raw); ok {
				related[relatedURI] = diagnostics
			}
		}
		return nil, related, false
	case protocol.FullDocumentDiagnosticReport:
		return value.Items, related, true
	case protocol.UnchangedDocumentDiagnosticReport:
		return nil, related, false
	default:
		toolsLogger.Debug("Unsupported diagnostic report type for %s: %T", uri, report.Value)
		return nil, related, false
	}
}

func diagnosticsFromRelatedReport(raw any) ([]protocol.Diagnostic, bool) {
	switch value := raw.(type) {
	case protocol.FullDocumentDiagnosticReport:
		return value.Items, true
	case protocol.UnchangedDocumentDiagnosticReport:
		return nil, false
	case protocol.RelatedFullDocumentDiagnosticReport:
		return value.Items, true
	case protocol.RelatedUnchangedDocumentDiagnosticReport:
		return nil, false
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}

	var full protocol.FullDocumentDiagnosticReport
	if err := json.Unmarshal(data, &full); err == nil && full.Kind == string(protocol.DiagnosticFull) {
		return full.Items, true
	}

	var unchanged protocol.UnchangedDocumentDiagnosticReport
	if err := json.Unmarshal(data, &unchanged); err == nil && unchanged.Kind == string(protocol.DiagnosticUnchanged) {
		return nil, false
	}

	return nil, false
}

func getSeverityString(severity protocol.DiagnosticSeverity) string {
	switch severity {
	case protocol.SeverityError:
		return "ERROR"
	case protocol.SeverityWarning:
		return "WARNING"
	case protocol.SeverityInformation:
		return "INFO"
	case protocol.SeverityHint:
		return "HINT"
	default:
		return "UNKNOWN"
	}
}
