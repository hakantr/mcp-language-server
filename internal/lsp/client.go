package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/isaacphi/mcp-language-server/internal/protocol"
	"github.com/isaacphi/mcp-language-server/internal/utilities"
	"github.com/isaacphi/mcp-language-server/internal/version"
)

type Client struct {
	Cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  io.ReadCloser
	writeMu sync.Mutex

	// Request ID counter
	nextID atomic.Int32

	// Response handlers
	handlers   map[string]chan *Message
	handlersMu sync.RWMutex

	// Server request handlers
	serverRequestHandlers map[string]ServerRequestHandler
	serverHandlersMu      sync.RWMutex

	// Notification handlers
	notificationHandlers map[string]NotificationHandler
	notificationMu       sync.RWMutex

	// Diagnostic cache
	diagnostics   map[protocol.DocumentUri][]protocol.Diagnostic
	diagnosticsMu sync.RWMutex

	// Files are currently opened by the LSP
	openFiles   map[string]*OpenFileInfo
	openFilesMu sync.RWMutex

	workspaceDir       string
	positionEncoding   protocol.PositionEncodingKind
	serverCapabilities protocol.ServerCapabilities
}

func NewClient(command string, args ...string) (*Client, error) {
	cmd := exec.Command(command, args...)
	// Copy env
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	client := &Client{
		Cmd:                   cmd,
		stdin:                 stdin,
		stdout:                bufio.NewReader(stdout),
		stderr:                stderr,
		handlers:              make(map[string]chan *Message),
		notificationHandlers:  make(map[string]NotificationHandler),
		serverRequestHandlers: make(map[string]ServerRequestHandler),
		diagnostics:           make(map[protocol.DocumentUri][]protocol.Diagnostic),
		openFiles:             make(map[string]*OpenFileInfo),
		positionEncoding:      protocol.UTF16,
	}

	// Start the LSP server process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start LSP server: %w", err)
	}

	// Handle stderr in a separate goroutine with proper logging
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			processLogger.Info("%s", line)
		}
		if err := scanner.Err(); err != nil {
			lspLogger.Error("Error reading LSP server stderr: %v", err)
		}
	}()

	// Start message handling loop
	go client.handleMessages()

	return client, nil
}

func (c *Client) RegisterNotificationHandler(method string, handler NotificationHandler) {
	c.notificationMu.Lock()
	defer c.notificationMu.Unlock()
	c.notificationHandlers[method] = handler
}

func (c *Client) RegisterServerRequestHandler(method string, handler ServerRequestHandler) {
	c.serverHandlersMu.Lock()
	defer c.serverHandlersMu.Unlock()
	c.serverRequestHandlers[method] = handler
}

func (c *Client) writeMessage(msg *Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WriteMessage(c.stdin, msg)
}

func (c *Client) InitializeLSPClient(ctx context.Context, workspaceDir string) (*protocol.InitializeResult, error) {
	// Register handlers before initialize. Some language servers send
	// server-to-client requests while the initialize request is still pending.
	c.RegisterServerRequestHandler("workspace/applyEdit", HandleApplyEdit)
	c.RegisterServerRequestHandler("workspace/configuration", HandleWorkspaceConfiguration)
	c.RegisterServerRequestHandler("client/registerCapability", HandleRegisterCapability)
	c.RegisterServerRequestHandler("workspace/diagnostic/refresh", HandleDiagnosticRefresh)
	c.RegisterNotificationHandler("window/showMessage", HandleServerMessage)
	c.RegisterNotificationHandler("textDocument/publishDiagnostics",
		func(params json.RawMessage) { HandleDiagnostics(c, params) })

	c.workspaceDir = workspaceDir
	if err := utilities.SetWorkspaceRoot(workspaceDir); err != nil {
		return nil, err
	}

	workspaceURI := protocol.URIFromPath(workspaceDir)
	initParams := &protocol.InitializeParams{
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
			WorkspaceFolders: []protocol.WorkspaceFolder{
				{
					URI:  string(workspaceURI),
					Name: workspaceDir,
				},
			},
		},

		XInitializeParams: protocol.XInitializeParams{
			ProcessID: int32(os.Getpid()),
			ClientInfo: &protocol.ClientInfo{
				Name:    version.Name,
				Version: version.Version,
			},
			RootPath: workspaceDir,
			RootURI:  workspaceURI,
			Capabilities: protocol.ClientCapabilities{
				General: &protocol.GeneralClientCapabilities{
					PositionEncodings: []protocol.PositionEncodingKind{
						protocol.UTF8,
						protocol.UTF16,
					},
				},
				Workspace: protocol.WorkspaceClientCapabilities{
					Configuration: true,
					DidChangeConfiguration: protocol.DidChangeConfigurationClientCapabilities{
						DynamicRegistration: true,
					},
					DidChangeWatchedFiles: protocol.DidChangeWatchedFilesClientCapabilities{
						DynamicRegistration:    true,
						RelativePatternSupport: true,
					},
					Diagnostics: &protocol.DiagnosticWorkspaceClientCapabilities{
						RefreshSupport: true,
					},
				},
				TextDocument: protocol.TextDocumentClientCapabilities{
					Synchronization: &protocol.TextDocumentSyncClientCapabilities{
						DynamicRegistration: true,
						DidSave:             true,
					},
					Completion: protocol.CompletionClientCapabilities{
						CompletionItem: protocol.ClientCompletionItemOptions{},
					},
					CodeLens: &protocol.CodeLensClientCapabilities{
						DynamicRegistration: true,
					},
					Hover: &protocol.HoverClientCapabilities{
						DynamicRegistration: true,
						ContentFormat: []protocol.MarkupKind{
							protocol.PlainText,
							protocol.Markdown,
						},
					},
					SignatureHelp: &protocol.SignatureHelpClientCapabilities{
						DynamicRegistration: true,
						ContextSupport:      true,
					},
					Definition: &protocol.DefinitionClientCapabilities{
						DynamicRegistration: true,
						LinkSupport:         true,
					},
					TypeDefinition: &protocol.TypeDefinitionClientCapabilities{
						DynamicRegistration: true,
						LinkSupport:         true,
					},
					Implementation: &protocol.ImplementationClientCapabilities{
						DynamicRegistration: true,
						LinkSupport:         true,
					},
					References: &protocol.ReferenceClientCapabilities{
						DynamicRegistration: true,
					},
					DocumentSymbol: protocol.DocumentSymbolClientCapabilities{},
					CodeAction: protocol.CodeActionClientCapabilities{
						CodeActionLiteralSupport: protocol.ClientCodeActionLiteralOptions{
							CodeActionKind: protocol.ClientCodeActionKindOptions{
								ValueSet: []protocol.CodeActionKind{},
							},
						},
					},
					PublishDiagnostics: protocol.PublishDiagnosticsClientCapabilities{
						VersionSupport: true,
					},
					Diagnostic: &protocol.DiagnosticClientCapabilities{
						DynamicRegistration:    true,
						RelatedDocumentSupport: true,
					},
					SemanticTokens: protocol.SemanticTokensClientCapabilities{
						Requests: protocol.ClientSemanticTokensRequestOptions{
							Range: &protocol.Or_ClientSemanticTokensRequestOptions_range{},
							Full:  &protocol.Or_ClientSemanticTokensRequestOptions_full{},
						},
						TokenTypes:     []string{},
						TokenModifiers: []string{},
						Formats:        []protocol.TokenFormat{},
					},
					InlayHint: &protocol.InlayHintClientCapabilities{
						DynamicRegistration: true,
					},
				},
				Window: protocol.WindowClientCapabilities{},
			},
			InitializationOptions: map[string]any{
				"codelenses": map[string]bool{
					"generate":           true,
					"regenerate_cgo":     true,
					"test":               true,
					"tidy":               true,
					"upgrade_dependency": true,
					"vendor":             true,
					"vulncheck":          false,
				},
			},
		},
	}

	var result protocol.InitializeResult
	if err := c.Call(ctx, "initialize", initParams, &result); err != nil {
		return nil, fmt.Errorf("initialize failed: %w", err)
	}

	c.serverCapabilities = result.Capabilities
	if result.Capabilities.PositionEncoding != nil && *result.Capabilities.PositionEncoding != "" {
		c.positionEncoding = *result.Capabilities.PositionEncoding
	} else {
		c.positionEncoding = protocol.UTF16
	}
	utilities.SetPositionEncoding(c.positionEncoding)
	lspLogger.Info("Using LSP position encoding: %s", c.positionEncoding)

	// Notify the LSP server
	err := c.Initialized(ctx, protocol.InitializedParams{})
	if err != nil {
		return nil, fmt.Errorf("initialization failed: %w", err)
	}

	// LSP sepecific Initialization
	path := strings.ToLower(c.Cmd.Path)
	switch {
	case strings.Contains(path, "typescript-language-server"):
		err := initializeTypescriptLanguageServer(ctx, c, workspaceDir)
		if err != nil {
			return nil, err
		}
	}

	return &result, nil
}

func (c *Client) Close() error {
	// Try to close all open files first
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Attempt to close files but continue shutdown regardless
	c.CloseAllFiles(ctx)

	// Force kill the LSP process if it doesn't exit within timeout.
	cancelForceKill := make(chan struct{})
	go func() {
		select {
		case <-time.After(2 * time.Second):
			lspLogger.Warn("LSP process did not exit within timeout, forcing kill")
			if c.Cmd.Process != nil {
				if err := c.Cmd.Process.Kill(); err != nil {
					lspLogger.Error("Failed to kill process: %v", err)
				} else {
					lspLogger.Info("Process killed successfully")
				}
			}
		case <-cancelForceKill:
			// Channel closed from completion path
			return
		}
	}()

	// Close stdin to signal the server
	if err := c.stdin.Close(); err != nil {
		lspLogger.Error("Failed to close stdin: %v", err)
	}

	// Wait for process to exit
	err := c.Cmd.Wait()
	close(cancelForceKill) // Stop the force kill goroutine

	return err
}

type ServerState int

const (
	StateStarting ServerState = iota
	StateReady
	StateError
)

func (c *Client) WaitForServerReady(ctx context.Context) error {
	timeout := 30 * time.Second
	if raw := os.Getenv("MCP_LSP_READY_TIMEOUT_MS"); raw != "" {
		if parsed, err := time.ParseDuration(raw + "ms"); err == nil && parsed >= 0 {
			timeout = parsed
		}
	}

	if timeout == 0 {
		return nil
	}

	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		probeCtx, probeCancel := context.WithTimeout(readyCtx, 2*time.Second)
		_, err := c.Symbol(probeCtx, protocol.WorkspaceSymbolParams{Query: "__mcp_language_server_ready_probe__"})
		probeCancel()
		if err == nil {
			return nil
		}

		if strings.Contains(err.Error(), "method not found") {
			lspLogger.Debug("workspace/symbol readiness probe unsupported: %v", err)
			return nil
		}

		select {
		case <-ticker.C:
		case <-readyCtx.Done():
			lspLogger.Warn("LSP readiness probe timed out after %s; continuing startup: %v", timeout, err)
			return nil
		}
	}
}

func (c *Client) PositionEncoding() protocol.PositionEncodingKind {
	if c.positionEncoding == "" {
		return protocol.UTF16
	}
	return c.positionEncoding
}

func (c *Client) SupportsDiagnosticPull() bool {
	return c.serverCapabilities.DiagnosticProvider != nil
}

func (c *Client) PositionFromLineColumn(filePath string, line, column int) (protocol.Position, error) {
	absPath, err := utilities.ValidatePathInWorkspace(filePath)
	if err != nil {
		return protocol.Position{}, err
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return protocol.Position{}, fmt.Errorf("error reading file: %w", err)
	}

	return utilities.LineColumnToPosition(string(content), line, column, c.PositionEncoding())
}

type OpenFileInfo struct {
	Version int32
	URI     protocol.DocumentUri
}

func (c *Client) OpenFile(ctx context.Context, filepath string) error {
	filepath, err := utilities.ValidatePathInWorkspace(filepath)
	if err != nil {
		return err
	}

	uri := protocol.URIFromPath(filepath)
	uriKey := string(uri)

	c.openFilesMu.Lock()
	if _, exists := c.openFiles[uriKey]; exists {
		c.openFilesMu.Unlock()
		return nil // Already open
	}
	c.openFilesMu.Unlock()

	// Skip files that do not exist or cannot be read
	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	params := protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        uri,
			LanguageID: DetectLanguageID(filepath),
			Version:    1,
			Text:       string(content),
		},
	}

	if err := c.Notify(ctx, "textDocument/didOpen", params); err != nil {
		return err
	}

	c.openFilesMu.Lock()
	c.openFiles[uriKey] = &OpenFileInfo{
		Version: 1,
		URI:     uri,
	}
	c.openFilesMu.Unlock()

	lspLogger.Debug("Opened file: %s", filepath)

	return nil
}

func (c *Client) NotifyChange(ctx context.Context, filepath string) error {
	filepath, err := utilities.ValidatePathInWorkspace(filepath)
	if err != nil {
		return err
	}

	uri := protocol.URIFromPath(filepath)
	uriKey := string(uri)

	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	c.openFilesMu.Lock()
	fileInfo, isOpen := c.openFiles[uriKey]
	if !isOpen {
		c.openFilesMu.Unlock()
		return fmt.Errorf("cannot notify change for unopened file: %s", filepath)
	}

	// Increment version
	fileInfo.Version++
	version := fileInfo.Version
	c.openFilesMu.Unlock()

	params := protocol.DidChangeTextDocumentParams{
		TextDocument: protocol.VersionedTextDocumentIdentifier{
			TextDocumentIdentifier: protocol.TextDocumentIdentifier{
				URI: uri,
			},
			Version: version,
		},
		ContentChanges: []protocol.TextDocumentContentChangeEvent{
			{
				Value: protocol.TextDocumentContentChangeWholeDocument{
					Text: string(content),
				},
			},
		},
	}

	return c.Notify(ctx, "textDocument/didChange", params)
}

func (c *Client) CloseFile(ctx context.Context, filepath string) error {
	filepath, err := utilities.ValidatePathInWorkspace(filepath)
	if err != nil {
		return err
	}

	uri := protocol.URIFromPath(filepath)
	uriKey := string(uri)

	c.openFilesMu.Lock()
	if _, exists := c.openFiles[uriKey]; !exists {
		c.openFilesMu.Unlock()
		return nil // Already closed
	}
	c.openFilesMu.Unlock()

	params := protocol.DidCloseTextDocumentParams{
		TextDocument: protocol.TextDocumentIdentifier{
			URI: uri,
		},
	}
	lspLogger.Debug("Closing file: %s", params.TextDocument.URI.Dir())
	if err := c.Notify(ctx, "textDocument/didClose", params); err != nil {
		return err
	}

	c.openFilesMu.Lock()
	delete(c.openFiles, uriKey)
	c.openFilesMu.Unlock()

	return nil
}

func (c *Client) IsFileOpen(filepath string) bool {
	filepath, err := utilities.ValidatePathInWorkspace(filepath)
	if err != nil {
		return false
	}

	uri := protocol.URIFromPath(filepath)
	c.openFilesMu.RLock()
	defer c.openFilesMu.RUnlock()
	_, exists := c.openFiles[string(uri)]
	return exists
}

// CloseAllFiles closes all currently open files
func (c *Client) CloseAllFiles(ctx context.Context) {
	c.openFilesMu.Lock()
	filesToClose := make([]string, 0, len(c.openFiles))

	// First collect all URIs that need to be closed
	for uri := range c.openFiles {
		filePath := protocol.DocumentUri(uri).Path()
		filesToClose = append(filesToClose, filePath)
	}
	c.openFilesMu.Unlock()

	// Then close them all
	for _, filePath := range filesToClose {
		err := c.CloseFile(ctx, filePath)
		if err != nil {
			lspLogger.Error("Error closing file %s: %v", filePath, err)
		}
	}

	lspLogger.Debug("Closed %d files", len(filesToClose))
}

func (c *Client) GetFileDiagnostics(uri protocol.DocumentUri) []protocol.Diagnostic {
	c.diagnosticsMu.RLock()
	defer c.diagnosticsMu.RUnlock()

	diagnostics := c.diagnostics[uri]
	out := make([]protocol.Diagnostic, len(diagnostics))
	copy(out, diagnostics)
	return out
}

func (c *Client) SetFileDiagnostics(uri protocol.DocumentUri, diagnostics []protocol.Diagnostic) {
	c.diagnosticsMu.Lock()
	defer c.diagnosticsMu.Unlock()

	out := make([]protocol.Diagnostic, len(diagnostics))
	copy(out, diagnostics)
	c.diagnostics[uri] = out
}
