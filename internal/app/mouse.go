// mouse.go — SGR (1006) mouse event parsing and dispatch.
//
// Mouse tracking is enabled in main.go with the escape sequences:
//
//	\033[?1000h  — X10 / X11 button-event tracking (clicks + wheel)
//	\033[?1006h  — SGR coordinate encoding (decimal, no overflow)
//
// SGR press events look like:  \033[<button;x;yM
// SGR release events look like: \033[<button;x;ym
//
// button is a decimal integer. The low 2 bits encode the button:
//
//	0 = left, 1 = middle, 2 = right, 3 = release (X10 only)
//
// Bit 6 (add 64) means wheel: 64 = wheel up, 65 = wheel down.
// Modifier bits (shift/meta/ctrl) are in bits 2/3/4 and are ignored here.
//
// All coordinates are 1-indexed terminal cells, matching the ANSI cursor
// addressing used by render.go.

package app

import (
	"bytes"
	"strconv"
)

// MouseEvent is a decoded mouse report.
type MouseEvent struct {
	Button  int  // raw button field (low bits = button, +64 = wheel)
	X       int  // 1-indexed terminal column
	Y       int  // 1-indexed terminal row
	Release bool // true = button release, false = press
}

// Mouse button constants (low 2 bits).
const (
	mouseLeft   = 0
	mouseMiddle = 1
	mouseRight  = 2
	mouseWheelUp   = 64
	mouseWheelDown = 65
)

// parseMouseSGR decodes an SGR-mode mouse report.
// Returns ok=false if seq is not a mouse event.
func parseMouseSGR(seq []byte) (ev MouseEvent, ok bool) {
	// Prefix is exactly \033[<
	if len(seq) < 6 || seq[0] != 0x1b || seq[1] != '[' || seq[2] != '<' {
		return ev, false
	}
	body := seq[3:]
	if len(body) < 3 {
		return ev, false
	}
	last := body[len(body)-1]
	if last != 'M' && last != 'm' {
		return ev, false
	}
	ev.Release = last == 'm'
	body = body[:len(body)-1]

	// body is "button;x;y" — split on ';'.
	parts := bytes.Split(body, []byte{';'})
	if len(parts) != 3 {
		return ev, false
	}
	b, err1 := strconv.Atoi(string(parts[0]))
	x, err2 := strconv.Atoi(string(parts[1]))
	y, err3 := strconv.Atoi(string(parts[2]))
	if err1 != nil || err2 != nil || err3 != nil {
		return ev, false
	}
	ev.Button = b
	ev.X = x
	ev.Y = y
	return ev, true
}

// handleMouse routes a decoded mouse event to the right pane.
//
// The top header bar (row 1) contains clickable mode tabs. The content area
// spans rows 2..TermH-1; the status bar is row TermH. Wheel events scroll
// whichever pane the cursor is over (tree vs viewer), mirroring how a user
// expects the wheel to work. Clicks select/open in the tree and move the
// cursor in the viewer. Focus follows the click.
func (a *App) handleMouse(ev MouseEvent) {
	// Header bar (row 1) — check for tab clicks first.
	if ev.Y == 1 {
		if !ev.Release {
			a.handleHeaderClick(ev.X)
		}
		return
	}

	// Status bar (bottom row) — ignore.
	if ev.Y >= a.TermH {
		return
	}
	// Content area is rows 2..TermH-1.
	if ev.Y < 2 {
		return
	}
	row := ev.Y - 2 // 0-indexed content row (header occupies terminal row 1)

	// --- Wheel scrolling -------------------------------------------------
	if ev.Button == mouseWheelUp {
		a.mouseScroll(row, ev.X, -1)
		return
	}
	if ev.Button == mouseWheelDown {
		a.mouseScroll(row, ev.X, +1)
		return
	}

	// --- Clicks (press only; ignore release) ----------------------------
	if ev.Release {
		return
	}

	switch ev.Button {
	case mouseLeft:
		a.mouseClick(row, ev.X)
	case mouseMiddle:
		// Middle-click could paste in some terminals; ignore here.
	case mouseRight:
		// Right-click: select without opening (tree) / move cursor (viewer).
		a.mouseClick(row, ev.X)
	}
}

// inTreePane reports whether terminal column x (1-indexed) falls inside the
// tree pane (only when the tree is visible).
func (a *App) inTreePane(x int) bool {
	return a.TreeVisible && x >= 1 && x <= a.TreeW
}

// viewerStartCol returns the 1-indexed ANSI column where the viewer content
// begins.
func (a *App) viewerStartCol() int {
	if a.TreeVisible {
		return a.TreeW + 2
	}
	return 1
}

// mouseScroll performs a wheel scroll in the pane under the pointer.
// dir is -1 for up, +1 for down.
func (a *App) mouseScroll(row, x int, dir int) {
	// Git mode has its own panes.
	if a.Git != nil {
		a.mouseScrollGit(row, x, dir)
		return
	}

	// Find results overlay occupies the viewer area when active.
	if a.Focus == FindResultsFocus && !a.inTreePane(x) {
		a.scrollFindResults(dir)
		return
	}

	if a.inTreePane(x) {
		a.scrollTree(dir)
		return
	}
	a.scrollViewer(dir)
}

// scrollTree scrolls the tree *viewport* by one wheel notch in dir
// (-1 = up, +1 = down). The cursor stays put and only sticks to the edge
// when it would scroll out of view — same feel as the file viewer.
func (a *App) scrollTree(dir int) {
	t := &a.Tree
	if len(t.flat) == 0 {
		return
	}
	height := a.contentHeight()
	if height < 1 {
		height = 1
	}

	t.scr += dir * wheelScrollLines

	maxScr := len(t.flat) - height
	if maxScr < 0 {
		maxScr = 0
	}
	if t.scr < 0 {
		t.scr = 0
	}
	if t.scr > maxScr {
		t.scr = maxScr
	}

	// Keep the cursor inside the visible window, stuck to the scrolled edge.
	if t.cursor < t.scr {
		t.cursor = t.scr
	} else if t.cursor >= t.scr+height {
		t.cursor = t.scr + height - 1
	}
}

// wheelScrollLines is how many lines one mouse-wheel notch scrolls in the
// viewer. 3 matches the default step used by most GUI editors (GTK/Qt).
const wheelScrollLines = 3

// scrollViewer scrolls the viewer *viewport* by one wheel notch in dir
// (-1 = up, +1 = down). Unlike cursor-movement keys, the viewport moves and
// the cursor stays put — it only shifts when it would scroll out of the
// visible window, in which case it sticks to the edge that scrolled past.
// This mirrors how GUI editors scroll with the mouse wheel.
func (a *App) scrollViewer(dir int) {
	if a.Buf == nil {
		return
	}
	height := a.contentHeight() // visible content rows (header + status bar excluded)
	if height < 1 {
		height = 1
	}
	b := a.Buf

	b.scr += dir * wheelScrollLines

	// Clamp the scroll offset to the valid range [0, lineCount-height].
	maxScr := b.LineCount() - height
	if maxScr < 0 {
		maxScr = 0
	}
	if b.scr < 0 {
		b.scr = 0
	}
	if b.scr > maxScr {
		b.scr = maxScr
	}

	// Keep the cursor inside the visible window; stick to whichever edge
	// scrolled past it. This is the "cursor stays, viewport moves" feel.
	if b.cy < b.scr {
		b.cy = b.scr
	} else if b.cy >= b.scr+height {
		b.cy = b.scr + height - 1
	}
	b.clampCursor()
	a.updateVisualEnd()
}

// scrollFindResults scrolls the find-results *viewport* by one wheel notch
// in dir (-1 = up, +1 = down). The selection (findCur) stays put and only
// sticks to the edge when it would scroll out of view — same as the file
// viewer.
func (a *App) scrollFindResults(dir int) {
	if len(a.findHits) == 0 {
		return
	}
	height := a.contentHeight()
	if height < 1 {
		height = 1
	}

	a.findScr += dir * wheelScrollLines

	// Clamp the scroll offset to [0, len(findHits)-height].
	maxScr := len(a.findHits) - height
	if maxScr < 0 {
		maxScr = 0
	}
	if a.findScr < 0 {
		a.findScr = 0
	}
	if a.findScr > maxScr {
		a.findScr = maxScr
	}

	// Keep the selection inside the visible window.
	if a.findCur < a.findScr {
		a.findCur = a.findScr
	} else if a.findCur >= a.findScr+height {
		a.findCur = a.findScr + height - 1
	}
	if a.findCur >= len(a.findHits) {
		a.findCur = len(a.findHits) - 1
	}
	if a.findCur < 0 {
		a.findCur = 0
	}
}

// mouseClick handles a left/right click at (row, x).
func (a *App) mouseClick(row, x int) {
	// Git mode panes.
	if a.Git != nil {
		a.mouseClickGit(row, x)
		return
	}

	// Find results overlay: clicking a result selects + opens it.
	if a.Focus == FindResultsFocus && !a.inTreePane(x) {
		idx := a.findScr + row
		if idx >= 0 && idx < len(a.findHits) {
			a.findCur = idx
			a.scrollFindToCursor()
			// Open the hit (same as Enter in find results).
			a.openFindHit(a.findCur)
		}
		return
	}

	if a.inTreePane(x) {
		a.clickTree(row)
		return
	}
	a.clickViewer(row, x)
}

// clickTree selects the tree row under the click and opens/toggles it.
func (a *App) clickTree(row int) {
	t := &a.Tree
	idx := t.scr + row
	if idx < 0 || idx >= len(t.flat) {
		return
	}
	t.cursor = idx
	a.ensureTreeVisible()
	a.Focus = TreeFocus

	n := t.flat[idx]
	if n.isDir {
		t.expandCurrent()
		t.buildFlat()
		a.ensureTreeVisible()
		return
	}
	// File: open it and switch focus to the viewer (same as Enter in tree).
	buf, err := openFile(n.path)
	if err != nil {
		a.StatusMsg = err.Error()
		return
	}
	a.Buf = buf
	a.Focus = ViewerFocus
}

// clickViewer moves the buffer cursor to the clicked cell.
func (a *App) clickViewer(row, x int) {
	if a.Buf == nil {
		return
	}
	lineIdx := a.Buf.scr + row
	if lineIdx < 0 {
		lineIdx = 0
	}
	if lineIdx >= a.Buf.LineCount() {
		lineIdx = a.Buf.LineCount() - 1
	}
	a.Buf.cy = lineIdx

	// Compute the visual column from the click position.
	visCol := x - a.viewerStartCol()
	if visCol < 0 {
		visCol = 0
	}
	rawLine := a.Buf.Line(a.Buf.cy)
	a.Buf.cx = visualToByte(rawLine, visCol)
	a.Buf.clampCursor()
	a.Buf.ensureVisible(a.contentHeight())
	a.updateVisualEnd()
	a.Focus = ViewerFocus
}

// openFindHit opens the file referenced by findHits[idx] at the match line.
func (a *App) openFindHit(idx int) {
	if idx < 0 || idx >= len(a.findHits) {
		return
	}
	hit := a.findHits[idx]
	buf, err := openFile(hit.path)
	if err != nil {
		a.StatusMsg = err.Error()
		return
	}
	a.Buf = buf
	a.Buf.cy = hit.line - 1
	a.Buf.clampCursor()
	a.Buf.ensureVisible(a.contentHeight())
	a.Focus = ViewerFocus
	a.findOpenedFile = true // ESC from viewer returns to find results
}

// ---------------------------------------------------------------------------
// Git-mode mouse helpers
// ---------------------------------------------------------------------------

// mouseScrollGit routes wheel events in git mode to the commit list or diff.
func (a *App) mouseScrollGit(row, x int, dir int) {
	g := a.Git
	if g == nil {
		return
	}
	if a.inTreePane(x) {
		// Commit list pane — scroll the viewport, cursor sticks to the edge.
		if len(g.commits) == 0 {
			return
		}
		height := a.contentHeight()
		if height < 1 {
			height = 1
		}
		g.commitScr += dir * wheelScrollLines
		maxScr := len(g.commits) - height
		if maxScr < 0 {
			maxScr = 0
		}
		if g.commitScr < 0 {
			g.commitScr = 0
		}
		if g.commitScr > maxScr {
			g.commitScr = maxScr
		}
		if g.commitCur < g.commitScr {
			g.commitCur = g.commitScr
		} else if g.commitCur >= g.commitScr+height {
			g.commitCur = g.commitScr + height - 1
		}
		if g.selAnchor != -1 {
			g.updateSelection()
		}
		return
	}
	// Diff pane — scroll the viewport (not the cursor line-by-line),
	// mirroring the file viewer wheel behavior. The cursor sticks to the
	// edge only when it would scroll out of view.
	height := a.contentHeight()
	if height < 1 || len(g.diffLines) == 0 {
		return
	}
	g.diffScr += dir * wheelScrollLines

	// Clamp the scroll offset to [0, len(diffLines)-height].
	maxScr := len(g.diffLines) - height
	if maxScr < 0 {
		maxScr = 0
	}
	if g.diffScr < 0 {
		g.diffScr = 0
	}
	if g.diffScr > maxScr {
		g.diffScr = maxScr
	}

	// Keep the cursor inside the visible window; stick to whichever edge
	// scrolled past it.
	if g.diffCursor < g.diffScr {
		g.diffCursor = g.diffScr
	} else if g.diffCursor >= g.diffScr+height {
		g.diffCursor = g.diffScr + height - 1
	}
	if g.diffSelStart != -1 {
		g.diffSelEnd = g.diffCursor
	}
	// Final safety clamp.
	if g.diffCursor < 0 {
		g.diffCursor = 0
	}
	if g.diffCursor >= len(g.diffLines) {
		g.diffCursor = len(g.diffLines) - 1
	}
}

// mouseClickGit handles clicks in git mode (commit list / diff).
func (a *App) mouseClickGit(row, x int) {
	g := a.Git
	if g == nil {
		return
	}
	if a.inTreePane(x) {
		// Commit list — clicking a commit selects it and shows its diff,
		// mirroring the Enter key behavior in handleCommitListKey.
		idx := g.commitScr + row
		if idx < 0 || idx >= len(g.commits) {
			return
		}
		g.commitCur = idx
		a.ensureCommitVisible()
		// Anchor the selection on the clicked commit so updateSelection
		// produces a single-commit range, then compute the diff.
		g.selAnchor = g.commitCur
		g.updateSelection()
		a.computeDiff()
		return
	}
	// Diff pane.
	if len(g.diffLines) == 0 {
		return
	}
	idx := g.diffScr + row
	if idx < 0 {
		idx = 0
	}
	if idx >= len(g.diffLines) {
		idx = len(g.diffLines) - 1
	}
	g.diffCursor = idx
	a.ensureDiffVisible()
}

// ---------------------------------------------------------------------------
// visualToByte — inverse of visualCol (see render.go)
// ---------------------------------------------------------------------------

// visualToByte returns the byte offset into rawLine whose display cell
// (after 4-space tab expansion) corresponds to visCol. Clicking within a
// tab's expanded whitespace lands on the tab byte itself; clicking the cell
// where the next character starts lands on that character.
// Used to convert a mouse click column into a cursor byte offset.
func visualToByte(rawLine string, visCol int) int {
	if visCol <= 0 {
		return 0
	}
	col := 0
	for i := 0; i < len(rawLine); i++ {
		startCol := col
		if rawLine[i] == '\t' {
			col = (col/4 + 1) * 4
		} else {
			col++
		}
		// visCol falls within this byte's display span [startCol, col).
		if visCol >= startCol && visCol < col {
			return i
		}
	}
	// Past end of line — clamp to length.
	return len(rawLine)
}