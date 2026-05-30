package utilities

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/davecgh/go-spew/spew"
	"github.com/isaacphi/mcp-language-server/internal/protocol"
)

var (
	osReadFile  = os.ReadFile
	osWriteFile = os.WriteFile
	osStat      = os.Stat
	osRemove    = os.Remove
	osRemoveAll = os.RemoveAll
	osRename    = os.Rename
)

var (
	configMu         sync.RWMutex
	workspaceRoot    string
	positionEncoding = protocol.UTF16
)

// SetWorkspaceRoot restricts future file edits to paths under root. An empty
// root disables the restriction, which keeps low-level unit tests lightweight.
func SetWorkspaceRoot(root string) error {
	configMu.Lock()
	defer configMu.Unlock()

	if root == "" {
		workspaceRoot = ""
		return nil
	}

	absRoot, err := cleanAbsPath(root)
	if err != nil {
		return err
	}
	workspaceRoot = absRoot
	return nil
}

// SetPositionEncoding records the LSP position encoding selected by the server.
func SetPositionEncoding(encoding protocol.PositionEncodingKind) {
	configMu.Lock()
	defer configMu.Unlock()

	if encoding == "" {
		encoding = protocol.UTF16
	}
	positionEncoding = encoding
}

func currentPositionEncoding() protocol.PositionEncodingKind {
	configMu.RLock()
	defer configMu.RUnlock()

	if positionEncoding == "" {
		return protocol.UTF16
	}
	return positionEncoding
}

func currentWorkspaceRoot() string {
	configMu.RLock()
	defer configMu.RUnlock()
	return workspaceRoot
}

func cleanAbsPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path is empty")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = realPath
	}

	return filepath.Clean(absPath), nil
}

// ValidatePathInWorkspace returns an absolute clean path if it is inside the
// configured workspace root.
func ValidatePathInWorkspace(path string) (string, error) {
	absPath, err := cleanAbsPath(path)
	if err != nil {
		return "", err
	}

	root := currentWorkspaceRoot()
	if root == "" {
		return absPath, nil
	}

	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to compare path with workspace: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("path is outside workspace: %s", path)
	}

	return absPath, nil
}

// ByteOffsetToCharacter converts a byte offset in line to the current LSP
// position encoding.
func ByteOffsetToCharacter(line string, byteOffset int) uint32 {
	return ByteOffsetToCharacterForEncoding(line, byteOffset, currentPositionEncoding())
}

func ByteOffsetToCharacterForEncoding(line string, byteOffset int, encoding protocol.PositionEncodingKind) uint32 {
	if byteOffset < 0 {
		byteOffset = 0
	}
	if byteOffset > len(line) {
		byteOffset = len(line)
	}

	switch encoding {
	case protocol.UTF8:
		return uint32(byteOffset)
	case protocol.UTF32:
		return uint32(utf8.RuneCountInString(line[:byteOffset]))
	default:
		units := 0
		for idx, r := range line {
			if idx >= byteOffset {
				break
			}
			if r > 0xFFFF {
				units += 2
			} else {
				units++
			}
		}
		return uint32(units)
	}
}

// PositionCharacterToByteOffset converts an LSP character offset to a byte
// offset in line using the currently selected position encoding.
func PositionCharacterToByteOffset(line string, character uint32) int {
	return CharacterToByteOffsetForEncoding(line, character, currentPositionEncoding())
}

func CharacterToByteOffsetForEncoding(line string, character uint32, encoding protocol.PositionEncodingKind) int {
	target := int(character)
	if target <= 0 {
		return 0
	}

	switch encoding {
	case protocol.UTF8:
		if target > len(line) {
			return len(line)
		}
		return target
	case protocol.UTF32:
		count := 0
		for idx := range line {
			if count == target {
				return idx
			}
			count++
		}
		return len(line)
	default:
		units := 0
		for idx, r := range line {
			if units >= target {
				return idx
			}
			if r > 0xFFFF {
				units += 2
			} else {
				units++
			}
			if units >= target {
				return idx + utf8.RuneLen(r)
			}
		}
		return len(line)
	}
}

func ColumnToByteOffset(line string, column int) int {
	if column <= 1 {
		return 0
	}

	target := column - 1
	count := 0
	for idx := range line {
		if count == target {
			return idx
		}
		count++
	}
	return len(line)
}

func LineColumnToPosition(content string, line, column int, encoding protocol.PositionEncodingKind) (protocol.Position, error) {
	if line < 1 {
		return protocol.Position{}, fmt.Errorf("line must be >= 1, got %d", line)
	}
	if column < 1 {
		return protocol.Position{}, fmt.Errorf("column must be >= 1, got %d", column)
	}

	lines := strings.Split(content, "\n")
	if line > len(lines) {
		return protocol.Position{}, fmt.Errorf("line %d is outside file with %d lines", line, len(lines))
	}

	lineText := lines[line-1]
	byteOffset := ColumnToByteOffset(lineText, column)
	return protocol.Position{
		Line:      uint32(line - 1),
		Character: ByteOffsetToCharacterForEncoding(lineText, byteOffset, encoding),
	}, nil
}

// ApplyTextEdits applies a sequence of text edits to a file specified by URI
func ApplyTextEdits(uri protocol.DocumentUri, edits []protocol.TextEdit) error {
	path, err := ValidatePathInWorkspace(uri.Path())
	if err != nil {
		return err
	}

	// Read the file content
	content, err := osReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Detect line ending style
	var lineEnding string
	if bytes.Contains(content, []byte("\r\n")) {
		lineEnding = "\r\n"
	} else {
		lineEnding = "\n"
	}

	// Track if file ends with a newline
	endsWithNewline := len(content) > 0 && bytes.HasSuffix(content, []byte(lineEnding))

	// Split into lines without the endings
	lines := strings.Split(string(content), lineEnding)

	// Check for overlapping edits
	for i, edit1 := range edits {
		for j := i + 1; j < len(edits); j++ {
			if RangesOverlap(edit1.Range, edits[j].Range) {
				return fmt.Errorf("overlapping edits detected between edit %d and %d", i, j)
			}
		}
	}

	// Sort edits in reverse order
	sortedEdits := make([]protocol.TextEdit, len(edits))
	copy(sortedEdits, edits)
	sort.Slice(sortedEdits, func(i, j int) bool {
		if sortedEdits[i].Range.Start.Line != sortedEdits[j].Range.Start.Line {
			return sortedEdits[i].Range.Start.Line > sortedEdits[j].Range.Start.Line
		}
		return sortedEdits[i].Range.Start.Character > sortedEdits[j].Range.Start.Character
	})

	// Apply each edit
	for _, edit := range sortedEdits {
		newLines, err := ApplyTextEdit(lines, edit, lineEnding)
		if err != nil {
			return fmt.Errorf("failed to apply edit: %w", err)
		}
		lines = newLines
	}

	// Join lines with proper line endings
	var newContent strings.Builder
	for i, line := range lines {
		if i > 0 {
			newContent.WriteString(lineEnding)
		}
		newContent.WriteString(line)
	}

	// Only add a newline if the original file had one and we haven't already added it
	if endsWithNewline && !strings.HasSuffix(newContent.String(), lineEnding) {
		newContent.WriteString(lineEnding)
	}

	if err := osWriteFile(path, []byte(newContent.String()), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// ApplyTextEdit applies a single text edit to a set of lines
func ApplyTextEdit(lines []string, edit protocol.TextEdit, lineEnding string) ([]string, error) {
	startLine := int(edit.Range.Start.Line)
	endLine := int(edit.Range.End.Line)

	// Validate positions
	if startLine < 0 || startLine >= len(lines) {
		return nil, fmt.Errorf("invalid start line: %d", startLine)
	}
	if endLine < 0 || endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	// Create result slice with initial capacity
	result := make([]string, 0, len(lines))

	// Copy lines before edit
	result = append(result, lines[:startLine]...)

	// Get the prefix of the start line
	startLineContent := lines[startLine]
	startChar := PositionCharacterToByteOffset(startLineContent, edit.Range.Start.Character)
	if startChar < 0 || startChar > len(startLineContent) {
		startChar = len(startLineContent)
	}
	prefix := startLineContent[:startChar]

	// Get the suffix of the end line
	endLineContent := lines[endLine]
	endChar := PositionCharacterToByteOffset(endLineContent, edit.Range.End.Character)
	if endChar < 0 || endChar > len(endLineContent) {
		endChar = len(endLineContent)
	}
	suffix := endLineContent[endChar:]

	// Handle the edit
	if edit.NewText == "" {
		if prefix+suffix != "" {
			result = append(result, prefix+suffix)
		}
	} else {
		// Split new text into lines
		newLines := strings.Split(edit.NewText, "\n")

		if len(newLines) == 1 {
			// Single line change
			result = append(result, prefix+newLines[0]+suffix)
		} else if endLine == startLine {
			// Multi-line insertion within the same line
			result = append(result, prefix+newLines[0])
			if len(newLines) > 2 {
				result = append(result, newLines[1:len(newLines)-1]...)
			}
			result = append(result, newLines[len(newLines)-1]+suffix)
		} else {
			// Multi-line change across different lines
			result = append(result, prefix+newLines[0])
			if len(newLines) > 2 {
				result = append(result, newLines[1:len(newLines)-1]...)
			}
			// Only append the final line with suffix if we're not replacing the entire content
			if len(suffix) > 0 || endLine < len(lines)-1 {
				result = append(result, newLines[len(newLines)-1]+suffix)
			} else {
				result = append(result, newLines[len(newLines)-1])
			}
		}
	}

	// Add remaining lines
	if endLine+1 < len(lines) {
		result = append(result, lines[endLine+1:]...)
	}

	return result, nil
}

// ApplyDocumentChange applies a DocumentChange (create/rename/delete operations)
func ApplyDocumentChange(change protocol.DocumentChange) error {
	if change.CreateFile != nil {
		path, err := ValidatePathInWorkspace(change.CreateFile.URI.Path())
		if err != nil {
			return err
		}
		if change.CreateFile.Options != nil {
			if change.CreateFile.Options.Overwrite {
				// Proceed with overwrite
			} else if change.CreateFile.Options.IgnoreIfExists {
				if _, err := osStat(path); err == nil {
					return nil // File exists and we're ignoring it
				}
			}
		}
		if err := osWriteFile(path, []byte(""), 0644); err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}
	}

	if change.DeleteFile != nil {
		path, err := ValidatePathInWorkspace(change.DeleteFile.URI.Path())
		if err != nil {
			return err
		}
		if change.DeleteFile.Options != nil && change.DeleteFile.Options.Recursive {
			if err := osRemoveAll(path); err != nil {
				return fmt.Errorf("failed to delete directory recursively: %w", err)
			}
		} else {
			if err := osRemove(path); err != nil {
				return fmt.Errorf("failed to delete file: %w", err)
			}
		}
	}

	if change.RenameFile != nil {
		oldPath, err := ValidatePathInWorkspace(change.RenameFile.OldURI.Path())
		if err != nil {
			return err
		}
		newPath, err := ValidatePathInWorkspace(change.RenameFile.NewURI.Path())
		if err != nil {
			return err
		}
		if change.RenameFile.Options != nil {
			if !change.RenameFile.Options.Overwrite {
				if _, err := osStat(newPath); err == nil {
					return fmt.Errorf("target file already exists and overwrite is not allowed: %s", newPath)
				}
			}
		}
		if err := osRename(oldPath, newPath); err != nil {
			return fmt.Errorf("failed to rename file: %w", err)
		}
	}

	if change.TextDocumentEdit != nil {
		textEdits := make([]protocol.TextEdit, len(change.TextDocumentEdit.Edits))
		for i, edit := range change.TextDocumentEdit.Edits {
			var err error
			textEdits[i], err = edit.AsTextEdit()
			if err != nil {
				return fmt.Errorf("invalid edit type: %w", err)
			}
		}
		return ApplyTextEdits(change.TextDocumentEdit.TextDocument.URI, textEdits)
	}

	return nil
}

// ApplyWorkspaceEdit applies the given WorkspaceEdit to the filesystem
func ApplyWorkspaceEdit(edit protocol.WorkspaceEdit) error {
	// Handle Changes field
	for uri, textEdits := range edit.Changes {
		if err := ApplyTextEdits(uri, textEdits); err != nil {
			return fmt.Errorf("failed to apply text edits: %w", err)
		}
	}

	// Handle DocumentChanges field
	for _, change := range edit.DocumentChanges {
		coreLogger.Warn("Document change: %v", spew.Sdump(change))
		if err := ApplyDocumentChange(change); err != nil {
			return fmt.Errorf("failed to apply document change: %w", err)
		}
	}

	return nil
}

// RangesOverlap checks if two ranges overlap in position
func RangesOverlap(r1, r2 protocol.Range) bool {
	if r1.Start.Line > r2.End.Line || r2.Start.Line > r1.End.Line {
		return false
	}
	if r1.Start.Line == r2.End.Line && r1.Start.Character > r2.End.Character {
		return false
	}
	if r2.Start.Line == r1.End.Line && r2.Start.Character > r1.End.Character {
		return false
	}
	return true
}
