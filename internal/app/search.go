// search.go — find popup: grep execution, result navigation, and popup rendering.
// Implements Ctrl-F find overlay with query input and results browsing.

package app

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"
	"unicode/utf8"
)

// utf8Accum accumulates bytes of a multi-byte UTF-8 sequence across
// successive calls to handleFindInputKey (raw mode delivers one byte at a time).
var utf8Accum []byte

// startFind enters find input mode. Called when Ctrl-F is pressed.
func (a *App) startFind() {
	a.findQuery = nil
	a.findCursor = 0
	a.findHits = nil
	a.findCur = -1
	a.findScr = 0
	a.findRunning = false
	a.findOpenedFile = false
	a.findPrevBuf = a.Buf // remember what was open so ESC can restore it
	utf8Accum = nil
	a.Focus = FindInputFocus
}

// handleFindInputKey processes keys in the find query input popup.
// Backspace: delete last rune of findQuery.
// Enter: executeFind().
// Escape: return to previous Focus (ViewerFocus if buf != nil, else TreeFocus).
// Printable runes: append to findQuery at findCursor position.
func (a *App) handleFindInputKey(seq []byte) {
	if len(seq) == 0 {
		return
	}

	// Escape (0x1b) — cancel find, restore previous view.
	if bytes.Equal(seq, []byte{0x1b}) {
		utf8Accum = nil
		a.exitFind()
		return
	}

	// Enter (0x0d) — execute search.
	if len(seq) == 1 && seq[0] == 0x0d {
		utf8Accum = nil
		a.executeFind()
		return
	}

	// Backspace (0x7f or 0x08) — delete rune before cursor.
	if len(seq) == 1 && (seq[0] == 0x7f || seq[0] == 0x08) {
		utf8Accum = nil
		if a.findCursor > 0 {
			a.findQuery = append(a.findQuery[:a.findCursor-1], a.findQuery[a.findCursor:]...)
			a.findCursor--
		}
		return
	}

	// Printable ASCII (0x20–0x7e) — insert single rune.
	if len(seq) == 1 && seq[0] >= 0x20 && seq[0] <= 0x7e {
		utf8Accum = nil
		a.insertFindRune(rune(seq[0]))
		return
	}

	// UTF-8 multi-byte (bytes >= 0x80) — accumulate and decode.
	if seq[0] >= 0x80 {
		utf8Accum = append(utf8Accum, seq...)
		r, size := utf8.DecodeRune(utf8Accum)
		if r != utf8.RuneError && size > 0 {
			a.insertFindRune(r)
			utf8Accum = nil
		} else if r == utf8.RuneError {
			// Invalid or incomplete — discard.
			utf8Accum = nil
		}
		// If size > len(utf8Accum), the sequence is incomplete; keep accumulating.
		return
	}

	// Other control characters: ignore, reset UTF-8 accumulator.
	utf8Accum = nil
}

// insertFindRune inserts rune r into findQuery at findCursor, then advances the cursor.
func (a *App) insertFindRune(r rune) {
	if a.findCursor == len(a.findQuery) {
		a.findQuery = append(a.findQuery, r)
	} else {
		a.findQuery = append(a.findQuery, 0) // grow by one
		copy(a.findQuery[a.findCursor+1:], a.findQuery[a.findCursor:])
		a.findQuery[a.findCursor] = r
	}
	a.findCursor++
}

// executeFind runs grep -rn with --null, parses output into findHits.
// Excludes .git and other common VCS/dependency directories. Paths are
// stored as absolute (for file opening) but displayed relative to the
// tree root (see renderFindResults).
// Sets findRunning=true during execution, false when done.
// On completion: findCur = 0 if results found, -1 if empty.
// Switches Focus to FindResultsFocus.
func (a *App) executeFind() {
	query := string(a.findQuery)
	if query == "" {
		a.StatusMsg = "empty query"
		return
	}

	a.findRunning = true
	a.Render() // show "Searching..." before blocking on grep

	cmd := exec.Command("grep", "-rn", "--color=never", "--null",
		"--exclude-dir=.git",
		"--exclude-dir=node_modules",
		"--exclude-dir=vendor",
		"--exclude-dir=.hg",
		"--exclude-dir=.svn",
		"--", query, a.Tree.RootPath)
	out, _ := cmd.CombinedOutput()

	a.findHits = a.parseGrepOutput(out)
	a.findRunning = false

	if len(a.findHits) > 0 {
		a.findCur = 0
	} else {
		a.findCur = -1
	}
	a.findScr = 0
	a.Focus = FindResultsFocus
}

// parseGrepOutput parses grep --null output (file\x00line:text\n) into []Hit.
// Handles "Binary file X matches" and silently ignores "grep:" error lines.
func (a *App) parseGrepOutput(out []byte) []Hit {
	var hits []Hit
	raw := string(out)

	// Pass 1: scan for binary-file lines (written to stdout, not in --null format).
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(line, "Binary file ") && strings.HasSuffix(line, " matches") {
			path := line[len("Binary file "):len(line)-len(" matches")]
			hits = append(hits, Hit{path: path, line: 0, text: "[binary]"})
		}
		// grep error lines (e.g. "grep: some/dir: Permission denied") are
		// silently ignored.
	}

	// Pass 2: parse --null entries.
	// grep --null format: file\x00line:text\n
	// Split lines first, then find the null within each line.
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		// Skip grep error lines and binary-file lines.
		if strings.HasPrefix(line, "grep:") || strings.HasPrefix(line, "Binary file ") {
			continue
		}

		nullPos := strings.IndexByte(line, 0)
		if nullPos < 0 {
			continue
		}
		file := line[:nullPos]
		rest := line[nullPos+1:]

		colon := strings.IndexByte(rest, ':')
		if colon < 0 {
			continue
		}

		lineStr := rest[:colon]
		lineNum, err := strconv.Atoi(lineStr)
		if err != nil {
			continue
		}

		text := rest[colon+1:]

		hits = append(hits, Hit{path: file, line: lineNum, text: text})
	}

	return hits
}

// handleFindResultsKey processes keys in the results list.
// j/k: navigate findCur up/down.
// Enter: open file at hit line, switch to ViewerFocus.
// Escape: back to FindInputFocus to refine query.
func (a *App) handleFindResultsKey(seq []byte) {
	if len(seq) != 1 {
		return
	}

	switch seq[0] {
	case 'j': // 0x6a — move selection down
		if a.findCur < len(a.findHits)-1 {
			a.findCur++
		}
		a.scrollFindToCursor()

	case 'k': // 0x6b — move selection up
		if a.findCur > 0 {
			a.findCur--
		}
		a.scrollFindToCursor()

	case 0x0d: // Enter — open file at selected hit
		if a.findCur >= 0 && a.findCur < len(a.findHits) {
			hit := a.findHits[a.findCur]
			buf, err := openFile(hit.path)
			if err != nil {
				a.StatusMsg = err.Error()
			} else {
				a.Buf = buf
				a.Buf.cy = hit.line - 1
				a.Buf.clampCursor()
				a.Buf.ensureVisible(a.contentHeight())
				a.Focus = ViewerFocus
				a.findOpenedFile = true // ESC from viewer returns to find results
			}
		}

	case 0x1b: // Escape — exit find mode, restore previous view
		a.exitFind()
	}
}

// exitFind leaves find mode entirely, restoring the Buf (if any) that was
// active before find was started. Focus goes to the viewer if a file is
// restored, otherwise to the tree.
func (a *App) exitFind() {
	a.findHits = nil
	a.findCur = -1
	a.findScr = 0
	a.findOpenedFile = false
	utf8Accum = nil

	// Restore the Buf that was open before find was started.
	a.Buf = a.findPrevBuf
	a.findPrevBuf = nil

	if a.Buf != nil {
		a.Focus = ViewerFocus
	} else if a.TreeVisible {
		a.Focus = TreeFocus
	}
}

// scrollFindToCursor ensures findCur is visible by adjusting findScr.
func (a *App) scrollFindToCursor() {
	clampScroll(a.findCur, &a.findScr, a.contentHeight(), len(a.findHits))
}

// renderFindInput draws the centered find popup overlay.
// A 60-column wide, 3-row box centered on screen.
// Row 1: "Find: " + query text with cursor indicator.
// Row 2: border/separator.
// Row 3: "[Enter] search  [Escape] cancel".
func (a *App) renderFindInput(out *bytes.Buffer) {
	popupW := 60
	if a.TermW < popupW {
		popupW = a.TermW
	}
	startCol := (a.TermW - popupW) / 2
	if startCol < 0 {
		startCol = 0
	}
	// Center the 3-row popup within the content area (rows 2..TermH-1).
	startRow := 2 + (a.contentHeight()-3)/2
	if startRow < 2 {
		startRow = 2
	}

	// Row 1: header with query and cursor.
	out.WriteString(cursorMove(startRow+1, startCol+1))
	out.WriteString(setFg(colorBlue))
	out.WriteString(" Find: ")
	out.WriteString(ansiReset)

	// Write query text with cursor indicator.
	for i, r := range a.findQuery {
		if i == a.findCursor {
			out.WriteString(ansiReverse)
			out.WriteString(string(r))
			out.WriteString(ansiReset)
		} else {
			out.WriteString(string(r))
		}
	}
	// Cursor at end: show reverse-video space.
	if a.findCursor == len(a.findQuery) {
		out.WriteString(ansiReverse)
		out.WriteByte(' ')
		out.WriteString(ansiReset)
	}
	out.WriteString(clearToEOL())

	// Row 2: separator line.
	out.WriteString(cursorMove(startRow+2, startCol+1))
	out.WriteString(ansiDim)
	out.WriteString(strings.Repeat("─", popupW))
	out.WriteString(ansiReset)
	out.WriteString(clearToEOL())

	// Row 3: help text.
	out.WriteString(cursorMove(startRow+3, startCol+1))
	out.WriteString(ansiDim)
	out.WriteString("[Enter] search  [Escape] cancel")
	out.WriteString(ansiReset)
	out.WriteString(clearToEOL())
}

// renderFindResults draws the search results list in the viewer pane area
// (columns treeW+1 through termW-1, rows 0 through termH-2).
// Each line: dim(filepath) + ":" + lineno + ":" + truncated text.
// Selected line highlighted with reverse video.
// "Searching..." while findRunning; "No matches" when empty after completion.
func (a *App) renderFindResults(out *bytes.Buffer) {
	viewerStartCol := 1 // 1-indexed ANSI column (no tree = start at col 1)
	viewerW := a.TermW
	if a.TreeVisible {
		viewerStartCol = a.TreeW + 2
		viewerW = a.TermW - a.TreeW - 1
	}
	visibleRows := a.contentHeight() // rows 0..TermH-2

	for row := 0; row < visibleRows; row++ {
		// Content begins at terminal row 2 (row 0 → row+2) due to the header.
		out.WriteString(cursorMove(row+2, viewerStartCol))
		out.WriteString(clearToEOL())

		// "Searching..." state — show centered message.
		if a.findRunning {
			if row == visibleRows/2 {
				msg := "Searching..."
				pad := (viewerW - len(msg)) / 2
				if pad < 0 {
					pad = 0
				}
				out.WriteString(strings.Repeat(" ", pad))
				out.WriteString(ansiDim)
				out.WriteString(msg)
				out.WriteString(ansiReset)
			}
			continue
		}

		// No matches state — show centered message.
		if len(a.findHits) == 0 {
			if row == visibleRows/2 {
				msg := "No matches for '" + string(a.findQuery) + "'"
				pad := (viewerW - len(msg)) / 2
				if pad < 0 {
					pad = 0
				}
				out.WriteString(strings.Repeat(" ", pad))
				out.WriteString(ansiDim)
				out.WriteString(msg)
				out.WriteString(ansiReset)
			}
			continue
		}

		// Results.
		hitIdx := a.findScr + row
		if hitIdx >= len(a.findHits) {
			continue
		}

		hit := a.findHits[hitIdx]
		lineNoStr := strconv.Itoa(hit.line)
		isSelected := hitIdx == a.findCur

		// Display path relative to the tree root (the stored path is
		// absolute so openFile still works when the user presses Enter).
		relPath := hit.path
		if strings.HasPrefix(relPath, a.Tree.RootPath) {
			relPath = relPath[len(a.Tree.RootPath):]
			relPath = strings.TrimPrefix(relPath, "/")
		}
		if relPath == "" {
			relPath = hit.path
		}

		// Layout: path + ":" + lineno + ":" + text, truncated to viewerW.
		// The delimiter is all ASCII so byte length == rune count.
		delim := ":" + lineNoStr + ":"
		delimW := len(delim)

		if delimW >= viewerW {
			// Even the delimiter doesn't fit — show it truncated.
			if isSelected {
				out.WriteString(ansiReverse)
			}
			out.WriteString(truncate(delim, viewerW))
			if isSelected {
				out.WriteString(ansiReset)
			}
			continue
		}

		remaining := viewerW - delimW
		// Give 60 % to path, 40 % to text.
		maxPath := remaining * 3 / 5
		if maxPath > remaining {
			maxPath = remaining
		}
		maxText := remaining - maxPath

		pathDisplay := truncate(relPath, maxPath)
		// If the path was shorter than allocated, give the slack to text.
		actualPathW := len([]rune(pathDisplay))
		maxText += maxPath - actualPathW
		textDisplay := truncate(hit.text, maxText)

		// Draw the line. Selected lines get reverse video.
		if isSelected {
			out.WriteString(ansiReverse)
		}
		out.WriteString(ansiDim)
		out.WriteString(pathDisplay)
		out.WriteString(ansiReset)
		if isSelected {
			out.WriteString(ansiReverse)
		}
		out.WriteString(delim)
		out.WriteString(textDisplay)
		if isSelected {
			out.WriteString(ansiReset)
		}
	}
}
