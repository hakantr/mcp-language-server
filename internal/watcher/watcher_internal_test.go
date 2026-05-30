package watcher

import (
	"testing"

	"github.com/isaacphi/mcp-language-server/internal/protocol"
	"github.com/stretchr/testify/require"
)

func TestMatchesGlobDoubleStarSpecificFile(t *testing.T) {
	require.True(t, matchesGlob("**/Cargo.toml", "/repo/crates/gpui/Cargo.toml"))
	require.True(t, matchesGlob("**/Cargo.toml", "/repo/Cargo.toml"))
	require.False(t, matchesGlob("**/Cargo.toml", "/repo/crates/gpui/NotCargo.toml"))
	require.False(t, matchesGlob("**/Cargo.toml", "/repo/crates/gpui/src/lib.rs"))
}

func TestMatchesGlobDoubleStarExtension(t *testing.T) {
	require.True(t, matchesGlob("**/*.rs", "/repo/crates/gpui/src/lib.rs"))
	require.False(t, matchesGlob("**/*.rs", "/repo/Cargo.toml"))
}

func TestMatchesPatternRelativeBaseURI(t *testing.T) {
	watcher := NewWorkspaceWatcher(nil)
	pattern := protocol.GlobPattern{
		Value: protocol.RelativePattern{
			BaseURI: protocol.Or_RelativePattern_baseUri{
				Value: "file:///repo/crates/gpui",
			},
			Pattern: "**/*.rs",
		},
	}

	require.True(t, watcher.matchesPattern("/repo/crates/gpui/src/lib.rs", pattern))
	require.False(t, watcher.matchesPattern("/repo/crates/theme/theme.json", pattern))
}

func TestMatchesPatternRelativeWorkspaceFolder(t *testing.T) {
	watcher := NewWorkspaceWatcher(nil)
	pattern := protocol.GlobPattern{
		Value: protocol.RelativePattern{
			BaseURI: protocol.Or_RelativePattern_baseUri{
				Value: protocol.WorkspaceFolder{
					URI:  "file:///repo/crates/gpui",
					Name: "gpui",
				},
			},
			Pattern: "**/*.rs",
		},
	}

	require.True(t, watcher.matchesPattern("/repo/crates/gpui/src/lib.rs", pattern))
	require.False(t, watcher.matchesPattern("/repo/crates/theme/theme.json", pattern))
}

func TestDefaultWatcherConfigRegistrationScanEnv(t *testing.T) {
	t.Setenv("MCP_LSP_OPEN_MATCHING_FILES_ON_REGISTRATION", "false")

	config := DefaultWatcherConfig()

	require.False(t, config.OpenMatchingFilesOnRegistration)
}
