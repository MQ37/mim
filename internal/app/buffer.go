package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// openFile reads a file and returns a Buf with lines split by '\n'.
// Detects binary files: if first 8KB contains a null byte, return error.
// Normalizes line endings: \r\n → \n.
func openFile(path string) (*Buf, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Binary detection: check first 8KB for null bytes.
	checkLen := len(data)
	if checkLen > 8192 {
		checkLen = 8192
	}
	if bytes.Contains(data[:checkLen], []byte{0}) {
		return nil, errors.New("binary file")
	}

	// Normalize \r\n → \n.
	s := strings.ReplaceAll(string(data), "\r\n", "\n")

	// Resolve absolute path.
	absPath, err := filepath.Abs(path)
	if err != nil {
		absPath = path
	}

	lines := strings.Split(s, "\n")

	// Detect language and pre-compute syntax highlighting.
	lang := detectLang(absPath)
	var segments [][]Segment
	if lang != langNone {
		segments = highlightAll(lines, lang)
	}

	return &Buf{
		path:         absPath,
		lines:        lines,
		selStartLine: -1,
		hlLang:       lang,
		hlSegments:   segments,
	}, nil
}

// OpenFile is the exported entry point for opening a file from outside the
// app package (e.g. from main.go when the user passes a file path on the
// command line). It reads the file, sets a.Buf, and switches Focus to the
// viewer.
func (a *App) OpenFile(path string) error {
	buf, err := openFile(path)
	if err != nil {
		return err
	}
	a.Buf = buf
	a.Focus = ViewerFocus
	return nil
}

// Line returns the n-th line (0-indexed). Clamped to valid range.
func (b *Buf) Line(n int) string {
	if n < 0 || n >= len(b.lines) {
		return ""
	}
	return b.lines[n]
}

// LineCount returns the number of lines.
func (b *Buf) LineCount() int {
	return len(b.lines)
}

// clampCursor keeps cx/cy within valid bounds.
// cx clamped to [0, len(line)]. cy clamped to [0, len(lines)-1].
func (b *Buf) clampCursor() {
	if len(b.lines) == 0 {
		b.cx = 0
		b.cy = 0
		return
	}

	// Clamp cy.
	if b.cy < 0 {
		b.cy = 0
	}
	if b.cy >= len(b.lines) {
		b.cy = len(b.lines) - 1
	}

	// Clamp cx to line length.
	lineLen := len(b.lines[b.cy])
	if b.cx < 0 {
		b.cx = 0
	}
	if b.cx > lineLen {
		b.cx = lineLen
	}
}

// ensureVisible adjusts b.scr so that b.cy is visible within a viewport
// of vpHeight rows. Called after any cursor movement.
func (b *Buf) ensureVisible(vpHeight int) {
	clampScroll(b.cy, &b.scr, vpHeight, b.LineCount())
}

// cursorCol returns the visual column of the cursor, expanding tabs
// to 4-space tab stops.
func (b *Buf) cursorCol() int {
	if b.cy < 0 || b.cy >= len(b.lines) {
		return 0
	}

	line := b.lines[b.cy]
	col := 0
	for i := 0; i < b.cx && i < len(line); i++ {
		if line[i] == '\t' {
			col = (col/4 + 1) * 4
		} else {
			col++
		}
	}
	return col
}

// visualText returns the currently selected text.
// If no selection (selStartLine == -1), returns "".
// For linewise (V): returns joined selected lines with newlines.
// For charwise (v): returns the substring spanning the selection.
// Handles forward and backward selections correctly.
func (b *Buf) visualText() string {
	if b.selStartLine == -1 {
		return ""
	}

	// Normalize start/end so start < end.
	startLine, startCol := b.selStartLine, b.selStartCol
	endLine, endCol := b.selEndLine, b.selEndCol

	if startLine > endLine || (startLine == endLine && startCol > endCol) {
		startLine, startCol, endLine, endCol = endLine, endCol, startLine, startCol
	}

	if b.selLinewise {
		// Linewise (V): join selected lines with newline.
		return strings.Join(b.lines[startLine:endLine+1], "\n")
	}

	// Charwise (v).
	if startLine == endLine {
		// Single line.
		line := b.lines[startLine]
		if startCol > len(line) {
			startCol = len(line)
		}
		if endCol > len(line) {
			endCol = len(line)
		}
		return line[startCol:endCol]
	}

	// Multi-line charwise.
	var parts []string

	// First line: from startCol to end.
	firstLine := b.lines[startLine]
	if startCol > len(firstLine) {
		startCol = len(firstLine)
	}
	parts = append(parts, firstLine[startCol:])

	// Middle lines.
	for i := startLine + 1; i < endLine; i++ {
		parts = append(parts, b.lines[i])
	}

	// Last line: from 0 to endCol.
	lastLine := b.lines[endLine]
	if endCol > len(lastLine) {
		endCol = len(lastLine)
	}
	parts = append(parts, lastLine[:endCol])

	return strings.Join(parts, "\n")
}

// yankText sends text to the system clipboard via OSC 52 escape sequence.
func yankText(text string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "\033]52;c;%s\033\\", encoded)
	return err
}

// yankToClipboard copies the visual selection to system clipboard.
func (b *Buf) yankToClipboard() error {
	text := b.visualText()
	if err := yankText(text); err != nil {
		return err
	}
	b.selStartLine = -1
	return nil
}
