package lsp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/isaacphi/mcp-language-server/internal/protocol"
	"github.com/isaacphi/mcp-language-server/internal/utilities"
)

// FileWatchHandler is called when file watchers are registered by the server
type FileWatchHandler func(id string, watchers []protocol.FileSystemWatcher)

// fileWatchHandler holds the current file watch handler
var fileWatchHandler FileWatchHandler

// RegisterFileWatchHandler registers a handler for file watcher registrations
func RegisterFileWatchHandler(handler FileWatchHandler) {
	fileWatchHandler = handler
}

// Requests

func HandleWorkspaceConfiguration(params json.RawMessage) (any, error) {
	var configParams protocol.ConfigurationParams
	if err := json.Unmarshal(params, &configParams); err != nil {
		return nil, fmt.Errorf("failed to unmarshal configuration params: %w", err)
	}

	configuration, err := configuredWorkspaceSettings()
	if err != nil {
		return nil, err
	}

	results := make([]any, len(configParams.Items))
	for i, item := range configParams.Items {
		if item.Section == "" {
			results[i] = configuration
			continue
		}
		if section, ok := lookupConfigurationSection(configuration, item.Section); ok {
			results[i] = section
			continue
		}
		results[i] = map[string]any{}
	}

	return results, nil
}

func configuredWorkspaceSettings() (map[string]any, error) {
	raw := os.Getenv("MCP_LSP_CONFIGURATION")
	if raw == "" {
		return map[string]any{}, nil
	}

	var configuration map[string]any
	if err := json.Unmarshal([]byte(raw), &configuration); err != nil {
		return nil, fmt.Errorf("failed to parse MCP_LSP_CONFIGURATION: %w", err)
	}
	return configuration, nil
}

func lookupConfigurationSection(configuration map[string]any, section string) (any, bool) {
	if value, ok := configuration[section]; ok {
		return value, true
	}

	var current any = configuration
	for _, part := range strings.Split(section, ".") {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}

		current, ok = currentMap[part]
		if !ok {
			return nil, false
		}
	}

	return current, true
}

func HandleRegisterCapability(params json.RawMessage) (any, error) {
	var registerParams protocol.RegistrationParams
	if err := json.Unmarshal(params, &registerParams); err != nil {
		lspLogger.Error("Error unmarshaling registration params: %v", err)
		return nil, err
	}

	for _, reg := range registerParams.Registrations {
		lspLogger.Info("Registration received for method: %s, id: %s", reg.Method, reg.ID)

		// Special handling for file watcher registrations
		if reg.Method == "workspace/didChangeWatchedFiles" {
			// Parse the options into the appropriate type
			var opts protocol.DidChangeWatchedFilesRegistrationOptions
			optJson, err := json.Marshal(reg.RegisterOptions)
			if err != nil {
				lspLogger.Error("Error marshaling registration options: %v", err)
				continue
			}

			err = json.Unmarshal(optJson, &opts)
			if err != nil {
				lspLogger.Error("Error unmarshaling registration options: %v", err)
				continue
			}

			// Notify file watchers
			if fileWatchHandler != nil {
				fileWatchHandler(reg.ID, opts.Watchers)
			}
		}
	}

	return nil, nil
}

func HandleApplyEdit(params json.RawMessage) (any, error) {
	var workspaceEdit protocol.ApplyWorkspaceEditParams
	if err := json.Unmarshal(params, &workspaceEdit); err != nil {
		return protocol.ApplyWorkspaceEditResult{Applied: false}, err
	}

	// Apply the edits
	err := utilities.ApplyWorkspaceEdit(workspaceEdit.Edit)
	if err != nil {
		lspLogger.Error("Error applying workspace edit: %v", err)
		return protocol.ApplyWorkspaceEditResult{
			Applied:       false,
			FailureReason: workspaceEditFailure(err),
		}, nil
	}

	return protocol.ApplyWorkspaceEditResult{
		Applied: true,
	}, nil
}

func workspaceEditFailure(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Notifications

// HandleServerMessage processes window/showMessage notifications from the server
func HandleServerMessage(params json.RawMessage) {
	var msg protocol.ShowMessageParams
	if err := json.Unmarshal(params, &msg); err != nil {
		lspLogger.Error("Error unmarshaling server message: %v", err)
		return
	}

	// Log the message with appropriate level
	switch msg.Type {
	case protocol.Error:
		lspLogger.Error("Server error: %s", msg.Message)
	case protocol.Warning:
		lspLogger.Warn("Server warning: %s", msg.Message)
	case protocol.Info:
		lspLogger.Info("Server info: %s", msg.Message)
	default:
		lspLogger.Debug("Server message: %s", msg.Message)
	}
}

// HandleDiagnostics processes textDocument/publishDiagnostics notifications
func HandleDiagnostics(client *Client, params json.RawMessage) {
	var diagParams protocol.PublishDiagnosticsParams
	if err := json.Unmarshal(params, &diagParams); err != nil {
		lspLogger.Error("Error unmarshaling diagnostic params: %v", err)
		return
	}

	// Save diagnostics in client
	client.diagnosticsMu.Lock()
	client.diagnostics[diagParams.URI] = diagParams.Diagnostics
	client.diagnosticsMu.Unlock()

	lspLogger.Info("Received diagnostics for %s: %d items", diagParams.URI, len(diagParams.Diagnostics))
}
