// render.go — ANSI escape helpers, pane renderers, and the main render loop.
// Renders into a bytes.Buffer and writes to stdout in a single syscall.
// Uses ABSOLUTE cursor positioning via cursorMove() to prevent pane overlap.

package app

import (
	"bytes"
	"os"
	"strconv"
	"strings"
)

// ANSI escape sequences.
const (
	ansiReset      = "\033[0m"
	ansiDim        = "\033[2m"
	ansiReverse    = "\033[7m"
	ansiHideCursor = "\033[?25l"
	ansiShowCursor = "\033[?25h"
)

// Color palette (ANSI 256-color indices).
const (
	colorYellow  = 3
	colorBlue    = 4
	colorWhite   = 7
	colorStatus  = 12
)

// --- ANSI escape helpers ---

// cursorMove returns the escape sequence to move cursor to (row, col).
// Both are 1-indexed per ANSI standard.
func cursorMove(row, col int) string {
	// \033[row;colH
	b := make([]byte, 0, 16)
	b = append(b, '\033', '[')
	b = strconv.AppendInt(b, int64(row), 10)
	b = append(b, ';')
	b = strconv.AppendInt(b, int64(col), 10)
	b = append(b, 'H')
	return string(b)
}

// clearToEOL returns the escape sequence to clear from cursor to end of line.
func clearToEOL() string {
	return "\033[K"
}

// setFg returns the escape sequence to set foreground color by 256-color index.
func setFg(color int) string {
	return "\033[38;5;" + strconv.Itoa(color) + "m"
}

// setBg returns the escape sequence to set background color by 256-color index.
func setBg(color int) string {
	return "\033[48;5;" + strconv.Itoa(color) + "m"
}

// --- String helpers ---

// truncate cuts s to maxWidth characters (not bytes). If s is already shorter,
// it is returned unchanged.
func truncate(s string, maxWidth int) string {
	if maxWidth < 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxWidth {
		return s
	}
	return string(runes[:maxWidth])
}

// padRight pads s with spaces on the right until it reaches totalWidth characters.
// If s is already longer, it is returned unchanged.
func padRight(s string, totalWidth int) string {
	runes := []rune(s)
	if len(runes) >= totalWidth {
		return s
	}
	return s + strings.Repeat(" ", totalWidth-len(runes))
}

// tabExpand replaces tabs with spaces (4-space tab stops) for display.
func tabExpand(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	col := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\t' {
			spaces := 4 - (col % 4)
			b.WriteString(strings.Repeat(" ", spaces))
			col += spaces
		} else {
			b.WriteByte(c)
			col++
		}
	}
	return b.String()
}

// tabExpandAnsi is like tabExpand but skips ANSI escape sequences
// (\033[...m) when counting columns. Preserves ANSI codes in output.
func tabExpandAnsi(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	col := 0
	i := 0
	for i < len(s) {
		c := s[i]
		// Skip ANSI escape sequences: \033[ ... m
		if c == '\033' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				b.WriteString(s[i : j+1])
				i = j + 1
				continue
			}
		}
		if c == '\t' {
			spaces := 4 - (col % 4)
			b.WriteString(strings.Repeat(" ", spaces))
			col += spaces
		} else {
			b.WriteByte(c)
			col++
		}
		i++
	}
	return b.String()
}

// truncateVisible truncates s to maxWidth visible characters, skipping
// ANSI escape sequences. Preserves ANSI codes that fall within the
// visible range.
func truncateVisible(s string, maxWidth int) string {
	if maxWidth < 0 {
		return ""
	}
	var result []byte
	vis := 0
	i := 0
	hadAnsi := false
	for i < len(s) && vis < maxWidth {
		c := s[i]
		if c == '\033' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				result = append(result, s[i:j+1]...)
				hadAnsi = true
				i = j + 1
				continue
			}
		}
		result = append(result, c)
		vis++
		i++
	}
	// If we truncated and had ANSI codes, close them to prevent color bleed.
	if vis >= maxWidth && i < len(s) && hadAnsi {
		result = append(result, ansiReset...)
	}
	return string(result)
}

// visualCol returns the display column (0-indexed) corresponding to the given
// byte offset into rawLine, accounting for 4-space tab expansion.
func visualCol(rawLine string, byteOffset int) int {
	col := 0
	for i := 0; i < byteOffset && i < len(rawLine); i++ {
		if rawLine[i] == '\t' {
			col = (col/4 + 1) * 4
		} else {
			col++
		}
	}
	return col
}

// --- Main render loop ---

// render draws the complete frame: tree pane, separator, viewer/find overlay,
// and status bar. Everything is built into a bytes.Buffer and written to
// stdout in a single Write call.
func (a *App) Render() {
	var buf bytes.Buffer

	buf.WriteString(ansiHideCursor)

	// Git mode overrides the standard layout.
	if a.Git != nil {
		a.renderGitView(&buf)
		a.renderStatus(&buf)
		// Cursor in git mode.
		if a.Git.diffLines != nil {
			visRow := a.Git.diffCursor - a.Git.diffScr
			if visRow < 0 {
				visRow = 0
			}
			if visRow >= a.TermH-1 {
				visRow = a.TermH - 2
			}
			buf.WriteString(cursorMove(visRow+1, a.TreeW+2))
		} else {
			visRow := a.Git.commitCur - a.Git.commitScr
			if visRow < 0 {
				visRow = 0
			}
			if visRow >= a.TermH-1 {
				visRow = a.TermH - 2
			}
			buf.WriteString(cursorMove(visRow+1, 1))
		}
		buf.WriteString(ansiShowCursor)
		os.Stdout.Write(buf.Bytes())
		return
	}

	// Row-major loop: tree pane, separator, viewer pane for each visible row.
	viewerCol := 1 // 1-indexed column where viewer starts
	if a.TreeVisible {
		viewerCol = a.TreeW + 2
	}

	for row := 0; row < a.TermH-1; row++ {
		if a.TreeVisible {
			// Tree pane: columns 0..TreeW-1 (0-indexed). Start at 1-indexed col 1.
			buf.WriteString(cursorMove(row+1, 1))
			a.renderTreeLine(&buf, row)

			// Vertical separator at 0-indexed column treeW = 1-indexed treeW+1.
			buf.WriteString(cursorMove(row+1, a.TreeW+1))
			buf.WriteString(ansiDim)
			buf.WriteString("│")
			buf.WriteString(ansiReset)
		}

		// Viewer / find pane.
		buf.WriteString(cursorMove(row+1, viewerCol))
		if a.Focus == FindInputFocus || a.Focus == FindResultsFocus {
			// Overlays are drawn separately below — skip viewer for these rows.
			buf.WriteString(clearToEOL()) // clear stale overlay area
		} else {
			a.renderViewerRow(&buf, row)
			buf.WriteString(clearToEOL()) // clear any trailing old text
		}
	}

	// Draw overlays on top of the viewer area (they use absolute positioning).
	if a.Focus == FindInputFocus {
		a.renderFindInput(&buf)
	} else if a.Focus == FindResultsFocus {
		a.renderFindResults(&buf)
	}

	// Status bar on the last row.
	a.renderStatus(&buf)

	// Final cursor position.
	switch a.Focus {
	case TreeFocus:
		visRow := a.Tree.cursor - a.Tree.scr
		if visRow < 0 {
			visRow = 0
		}
		if visRow >= a.TermH-1 {
			visRow = a.TermH - 2
		}
		buf.WriteString(cursorMove(visRow+1, 1))

	case ViewerFocus:
		if a.Buf != nil {
			visRow := a.Buf.cy - a.Buf.scr
			if visRow < 0 {
				visRow = 0
			}
			if visRow >= a.TermH-1 {
				visRow = a.TermH - 2
			}
			visCol := a.Buf.cursorCol()
			// Viewer starts at 1-indexed column 1 (hidden) or treeW+2 (visible).
			vc := 1
			if a.TreeVisible {
				vc = a.TreeW + 2
			}
			buf.WriteString(cursorMove(visRow+1, vc+visCol))
		}

	case FindInputFocus:
		// Handled by renderFindInput (search.go).
	case FindResultsFocus:
		// Handled by renderFindResults (search.go).
	}

	buf.WriteString(ansiShowCursor)
	os.Stdout.Write(buf.Bytes())
}

// --- Tree line rendering ---

// renderTreeLine writes one tree entry into out at the CURRENT cursor position.
// The line is padded to treeW-1 characters to prevent spill into the separator.
// Does NOT move the cursor; the caller (render()) positions it first.
func (a *App) renderTreeLine(out *bytes.Buffer, row int) {
	idx := a.Tree.scr + row
	treeContentW := a.TreeW // content width, fills to separator

	if idx >= len(a.Tree.flat) {
		out.WriteString(strings.Repeat(" ", treeContentW))
		return
	}

	node := a.Tree.flat[idx]

	// Compute indent depth: count path separators relative to root.
	rel := node.path
	if strings.HasPrefix(rel, a.Tree.RootPath) {
		rel = rel[len(a.Tree.RootPath):]
	}
	// Trim leading separator so root children are at indent 0.
	rel = strings.TrimPrefix(rel, string(os.PathSeparator))
	depth := 0
	if rel != "" {
		depth = strings.Count(rel, string(os.PathSeparator)) + 1
	}

	// Build prefix (indent + expand/collapse marker for dirs).
	prefix := strings.Repeat("  ", depth)
	if node.isDir {
		if node.open {
			prefix += "▼ "
		} else {
			prefix += "▶ "
		}
	} else {
		prefix += "  "
	}

	line := prefix + node.name

	// Highlight selected line.
	if idx == a.Tree.cursor {
		out.WriteString(ansiReverse)
	}

	// Pad to treeContentW and truncate.
	line = padRight(line, treeContentW)
	line = truncate(line, treeContentW)
	out.WriteString(line)

	if idx == a.Tree.cursor {
		out.WriteString(ansiReset)
	}
}

// --- Viewer row rendering ---

// renderViewerRow draws one viewer line at the CURRENT cursor position.
// Handles cursor-line highlight, visual selection, tab expansion, and the
// "no file open" placeholder.
func (a *App) renderViewerRow(out *bytes.Buffer, row int) {
	availW := a.TermW // available columns for viewer content
	if a.TreeVisible {
		availW = a.TermW - a.TreeW - 1
	}
	if availW < 1 {
		return
	}

	// No file open — show placeholder centered on screen.
	if a.Buf == nil {
		msg := "no file open"
		if row == a.TermH/2 {
			pad := (availW - len(msg)) / 2
			if pad < 0 {
				pad = 0
			}
			out.WriteString(strings.Repeat(" ", pad))
			out.WriteString(msg)
		}
		return
	}

	lineIdx := a.Buf.scr + row
	if lineIdx >= a.Buf.LineCount() {
		// Past EOF — show tilde on first empty line (like vim).
		if lineIdx == a.Buf.LineCount() {
			out.WriteString(ansiDim)
			out.WriteByte('~')
			out.WriteString(ansiReset)
		}
		return
	}

	rawLine := a.Buf.Line(lineIdx)
	displayLine := tabExpand(rawLine)
	displayLine = truncate(displayLine, availW)

	cursorLine := lineIdx == a.Buf.cy
	selActive := a.Buf.selStartLine != -1

	// If this is the cursor line, reverse-video the whole line.
	if cursorLine {
		out.WriteString(ansiReverse)
		out.WriteString(displayLine)
		out.WriteString(ansiReset)
		return
	}

	// Visual selection highlighting (only when not overridden by cursor line).
	if selActive {
		// Build highlighted version for display (preserve rawLine for offset math).
		hlDisplay := displayLine
		if a.Buf.hlLang != 0 && lineIdx < len(a.Buf.hlSegments) {
			hlLine := applyHighlight(rawLine, a.Buf.hlSegments[lineIdx])
			hlLine = tabExpandAnsi(hlLine)
			hlDisplay = truncateVisible(hlLine, availW)
		}
		a.renderViewerRowSelection(out, rawLine, hlDisplay, lineIdx)
		return
	}

	// Plain line — apply syntax highlighting if available.
	if a.Buf.hlLang != 0 && lineIdx < len(a.Buf.hlSegments) {
		hlLine := applyHighlight(rawLine, a.Buf.hlSegments[lineIdx])
		hlLine = tabExpandAnsi(hlLine)
		hlLine = truncateVisible(hlLine, availW)
		out.WriteString(hlLine)
	} else {
		out.WriteString(displayLine)
	}
}

// renderViewerRowSelection writes a viewer line with per-character selection
// highlighting (yellow background). Handles both charwise (v) and linewise (V)
// visual modes.
func (a *App) renderViewerRowSelection(out *bytes.Buffer, rawLine, displayLine string, lineIdx int) {
	// Normalize selection range so (lineFrom,colFrom) ≤ (lineTo,colTo).
	lineFrom, lineTo := a.Buf.selStartLine, a.Buf.selEndLine
	colFrom, colTo := a.Buf.selStartCol, a.Buf.selEndCol
	if lineFrom > lineTo || (lineFrom == lineTo && colFrom > colTo) {
		lineFrom, lineTo = lineTo, lineFrom
		colFrom, colTo = colTo, colFrom
	}

	// Check if this line is in the selection range.
	if lineIdx < lineFrom || lineIdx > lineTo {
		out.WriteString(displayLine)
		return
	}

	// Linewise selection: highlight the entire line.
	if a.Buf.selLinewise {
		out.WriteString(setBg(colorYellow))
		out.WriteString(displayLine)
		out.WriteString(ansiReset)
		return
	}

	// Charwise selection: highlight only the selected portion.
	var selStartVis, selEndVis int

	if lineFrom == lineTo {
		// Single-line selection.
		selStartVis = visualCol(rawLine, colFrom)
		selEndVis = visualCol(rawLine, colTo)
	} else if lineIdx == lineFrom {
		// First line of multi-line selection: from colFrom to end.
		selStartVis = visualCol(rawLine, colFrom)
		selEndVis = len([]rune(displayLine))
	} else if lineIdx == lineTo {
		// Last line of multi-line selection: from start to colTo.
		selStartVis = 0
		selEndVis = visualCol(rawLine, colTo)
	} else {
		// Middle line: fully selected.
		selStartVis = 0
		selEndVis = len([]rune(displayLine))
	}

	// Clamp and ensure non-empty.
	if selStartVis > selEndVis {
		selStartVis, selEndVis = selEndVis, selStartVis
	}
	runes := []rune(displayLine)
	if selStartVis < 0 {
		selStartVis = 0
	}
	if selEndVis > len(runes) {
		selEndVis = len(runes)
	}

	if selStartVis >= selEndVis {
		out.WriteString(displayLine)
		return
	}

	// Write prefix (unselected), middle (selected with yellow bg), suffix (unselected).
	if selStartVis > 0 {
		out.WriteString(string(runes[:selStartVis]))
	}
	out.WriteString(setBg(colorYellow))
	out.WriteString(string(runes[selStartVis:selEndVis]))
	out.WriteString(ansiReset)
	if selEndVis < len(runes) {
		out.WriteString(string(runes[selEndVis:]))
	}
}

// --- Status bar rendering ---

// renderStatus draws the bottom status line on row termH (1-indexed).
func (a *App) renderStatus(out *bytes.Buffer) {
	out.WriteString(cursorMove(a.TermH, 1))
	out.WriteString(setBg(colorStatus))
	out.WriteString(setFg(colorWhite))

	// Build the status mode text.
	var status string
	if a.StatusMsg != "" {
		status = a.StatusMsg
		a.StatusMsg = "" // clear after displaying
	} else {
		if a.Git != nil {
			g := a.Git
			if g.diffLines != nil {
				status = "-- DIFF: " + g.commits[g.selStart].hash[:8] + ".." + g.commits[g.selEnd].hash[:8] + " --"
			} else if g.selStart != -1 {
				n := g.selEnd - g.selStart + 1
				status = "-- GIT: " + strconv.Itoa(n) + " commit"
				if n > 1 {
					status += "s"
				}
				status += " selected --"
			} else {
				status = "-- GIT: " + strconv.Itoa(len(g.commits)) + " commits --"
			}
		} else {
		switch a.Focus {
		case TreeFocus:
			status = "-- TREE --"
		case ViewerFocus:
			if a.Buf != nil && a.Buf.selStartLine != -1 {
				if a.Buf.selLinewise {
					status = "-- VISUAL LINE --"
				} else {
					status = "-- VISUAL --"
				}
			} else {
				status = "-- NORMAL --"
			}
		case FindInputFocus:
			status = "-- FIND --"
		case FindResultsFocus:
			status = "-- FIND: " + strconv.Itoa(len(a.findHits)) + " matches --"
		}
		} // end else (git mode)
	} // end outer else (statusMsg)

	// Build left-aligned file info.
	var leftText string
	if a.Buf != nil && a.Buf.path != "" {
		leftText = a.Buf.path + "  L" + strconv.Itoa(a.Buf.cy+1) + ":C" + strconv.Itoa(a.Buf.cursorCol()+1)
	} else {
		leftText = a.Tree.RootPath
	}

	// Left text + right-aligned status, padded/truncated to terminal width.
	full := padRight(leftText, a.TermW-len(status)-1) + " " + status
	full = truncate(full, a.TermW)
	out.WriteString(full)

	out.WriteString(ansiReset)
	out.WriteString(clearToEOL())
}
