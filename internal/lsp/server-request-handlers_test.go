package lsp

import (
	"encoding/json"
	"testing"

	"github.com/isaacphi/mcp-language-server/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestHandleWorkspaceConfigurationReturnsOneResultPerItem(t *testing.T) {
	params, err := json.Marshal(protocol.ConfigurationParams{
		Items: []protocol.ConfigurationItem{
			{Section: "rust-analyzer"},
			{Section: "unknown"},
		},
	})
	require.NoError(t, err)

	result, err := HandleWorkspaceConfiguration(params)
	require.NoError(t, err)

	items, ok := result.([]any)
	require.True(t, ok)
	require.Len(t, items, 2)
	require.Equal(t, map[string]any{}, items[0])
	require.Equal(t, map[string]any{}, items[1])
}

func TestHandleWorkspaceConfigurationUsesEnvironmentConfiguration(t *testing.T) {
	t.Setenv("MCP_LSP_CONFIGURATION", `{"rust-analyzer":{"cargo":{"allTargets":false}}}`)

	params, err := json.Marshal(protocol.ConfigurationParams{
		Items: []protocol.ConfigurationItem{
			{Section: "rust-analyzer"},
			{Section: ""},
		},
	})
	require.NoError(t, err)

	result, err := HandleWorkspaceConfiguration(params)
	require.NoError(t, err)

	items, ok := result.([]any)
	require.True(t, ok)
	require.Len(t, items, 2)

	rustAnalyzerConfig, ok := items[0].(map[string]any)
	require.True(t, ok)
	require.Contains(t, rustAnalyzerConfig, "cargo")

	allConfig, ok := items[1].(map[string]any)
	require.True(t, ok)
	require.Contains(t, allConfig, "rust-analyzer")
}

func TestHandleWorkspaceConfigurationUsesDottedSection(t *testing.T) {
	t.Setenv("MCP_LSP_CONFIGURATION", `{"rust-analyzer":{"cargo":{"allTargets":false}}}`)

	params, err := json.Marshal(protocol.ConfigurationParams{
		Items: []protocol.ConfigurationItem{
			{Section: "rust-analyzer.cargo"},
		},
	})
	require.NoError(t, err)

	result, err := HandleWorkspaceConfiguration(params)
	require.NoError(t, err)

	items, ok := result.([]any)
	require.True(t, ok)
	require.Len(t, items, 1)

	cargoConfig, ok := items[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, false, cargoConfig["allTargets"])
}

func TestHandleWorkspaceConfigurationRejectsInvalidEnvironmentConfiguration(t *testing.T) {
	t.Setenv("MCP_LSP_CONFIGURATION", `{`)

	params, err := json.Marshal(protocol.ConfigurationParams{
		Items: []protocol.ConfigurationItem{{Section: "rust-analyzer"}},
	})
	require.NoError(t, err)

	_, err = HandleWorkspaceConfiguration(params)
	require.Error(t, err)
}
