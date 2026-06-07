// Key dispatch and viewer-mode vim keybindings.

package main

import "bytes"

// viewerPendingG tracks whether the previous key in viewer mode was 'g',
// enabling detection of the two-key 'gg' sequence.
var viewerPendingG bool

// --- Central dispatch ---

// dispatch is the central input router. Called on every keypress.
// First checks global keys (q, Ctrl-T, Ctrl-F).
// Then routes to mode-specific handler based on a.focus.
func (a *App) dispatch(seq []byte) {
	// Global keys first (take priority everywhere).
	if a.handleGlobalKey(seq) {
		return
	}

	// Git mode — route to git handler.
	if a.git != nil {
		a.handleGitKey(seq)
		return
	}

	// When tree is hidden, ViewerFocus is the only focus.
	if !a.treeVisible && a.focus == TreeFocus {
		a.focus = ViewerFocus
	}

	// Reset pending-g when focus is not viewer (e.g., after Ctrl-T away).
	if a.focus != ViewerFocus {
		viewerPendingG = false
	}

	// Mode-specific dispatch.
	switch a.focus {
	case TreeFocus:
		a.handleTreeKey(seq)
	case ViewerFocus:
		a.handleViewerKey(seq)
	case FindInputFocus:
		a.handleFindInputKey(seq)
	case FindResultsFocus:
		a.handleFindResultsKey(seq)
	}
}

// --- Global keys ---

// handleGlobalKey handles keys that work in ANY focus mode.
// Returns true if the key was consumed (preventing further dispatch).
func (a *App) handleGlobalKey(seq []byte) bool {
	if len(seq) != 1 {
		return false
	}
	switch seq[0] {
	case 'q': // 0x71 — quit
		a.quit = true
		return true
	case 0x14: // Ctrl-T — toggle focus between TreeFocus and ViewerFocus
		if a.focus == TreeFocus {
			if a.buf != nil {
				a.focus = ViewerFocus
			}
		} else if a.treeVisible {
			a.focus = TreeFocus
		}
		return true
	case 0x05: // Ctrl-E — toggle tree visibility
		a.treeVisible = !a.treeVisible
		if a.treeVisible {
			a.focus = TreeFocus
		} else if a.buf != nil {
			a.focus = ViewerFocus
		} else {
			// No file open — keep tree visible, focus stays on tree.
			a.treeVisible = true
		}
		return true
	case 0x07: // Ctrl-G — toggle git diff view
		if a.git != nil {
			a.git = nil
			a.focus = TreeFocus
		} else {
			a.enterGitMode()
		}
		return true
	case 0x06: // Ctrl-F — start find
		a.startFind()
		return true
	}
	return false
}

// --- Viewer key handler ---

// handleViewerKey handles keys when focus is ViewerFocus.
// Implements vim-like navigation, visual selection, and yank.
func (a *App) handleViewerKey(seq []byte) {
	if a.buf == nil {
		return
	}

	// --- Escape clears selection or is ignored ---
	if bytes.Equal(seq, []byte{0x1b}) {
		if a.buf.selStartLine != -1 {
			a.buf.selStartLine = -1 // clear selection
		}
		viewerPendingG = false
		return
	}

	// --- gg detection (two-key sequence) ---
	if len(seq) == 1 && seq[0] == 'g' {
		if viewerPendingG {
			// gg — jump to first line
			a.buf.cy = 0
			a.buf.clampCursor()
			a.buf.ensureVisible(a.termH - 2)
			viewerPendingG = false
			a.updateVisualEnd()
			return
		}
		viewerPendingG = true
		return
	}
	// Any other single-byte or multi-byte key clears pending-g.
	viewerPendingG = false

	// --- Single-byte navigation and commands ---
	if len(seq) != 1 {
		return // ignore multi-byte sequences for now (arrows etc. in v1)
	}

	// Track whether this key is a movement key (for visual extension).
	isMovement := false

	switch seq[0] {
	case 'h': // 0x68 — left
		a.buf.cx--
		a.buf.clampCursor()
		isMovement = true
	case 'j': // 0x6a — down
		a.buf.cy++
		a.buf.clampCursor()
		a.buf.ensureVisible(a.termH - 2)
		isMovement = true
	case 'k': // 0x6b — up
		a.buf.cy--
		a.buf.clampCursor()
		a.buf.ensureVisible(a.termH - 2)
		isMovement = true
	case 'l': // 0x6c — right
		a.buf.cx++
		a.buf.clampCursor()
		isMovement = true
	case 'G': // 0x47 — jump to last line
		a.buf.cy = a.buf.LineCount() - 1
		a.buf.clampCursor()
		a.buf.ensureVisible(a.termH - 2)
		isMovement = true
	case '0': // 0x30 — jump to column 0
		a.buf.cx = 0
		a.buf.clampCursor()
		isMovement = true
	case '$': // 0x24 — jump to end of line
		a.buf.cx = len(a.buf.Line(a.buf.cy))
		a.buf.clampCursor()
		isMovement = true
	case 0x04: // Ctrl-D — half page down
		a.buf.cy += (a.termH - 2) / 2
		a.buf.clampCursor()
		a.buf.ensureVisible(a.termH - 2)
		isMovement = true
	case 0x15: // Ctrl-U — half page up
		a.buf.cy -= (a.termH - 2) / 2
		a.buf.clampCursor()
		a.buf.ensureVisible(a.termH - 2)
		isMovement = true
	case 0x02: // Ctrl-B — page up
		a.buf.cy -= (a.termH - 2)
		a.buf.clampCursor()
		a.buf.ensureVisible(a.termH - 2)
		isMovement = true

	// --- Visual mode ---
	case 'v': // 0x76 — charwise visual mode
		a.buf.selStartLine = a.buf.cy
		a.buf.selStartCol = a.buf.cx
		a.buf.selEndLine = a.buf.cy
		a.buf.selEndCol = a.buf.cx
		a.buf.selLinewise = false
	case 'V': // 0x56 — linewise visual mode
		a.buf.selStartLine = a.buf.cy
		a.buf.selStartCol = 0
		a.buf.selEndLine = a.buf.cy
		a.buf.selEndCol = 0
		a.buf.selLinewise = true

	// --- Yank ---
	case 'y': // 0x79 — yank selection
		if a.buf.selStartLine != -1 {
			if err := a.buf.yankToClipboard(); err != nil {
				a.statusMsg = "yank failed"
			} else {
				a.statusMsg = "yanked N lines"
			}
			a.buf.selStartLine = -1 // clear selection
		}

	// --- Tab (ignored in v1) ---
	case 0x09: // Tab
		// no-op
	}

	// Update visual selection end after any movement key.
	if isMovement {
		a.updateVisualEnd()
	}
}

// --- Helpers ---

// updateVisualEnd updates the visual selection endpoint after cursor movement.
// If no selection is active (selStartLine == -1), this is a no-op.
func (a *App) updateVisualEnd() {
	if a.buf.selStartLine == -1 {
		return
	}
	a.buf.selEndLine = a.buf.cy
	if !a.buf.selLinewise {
		a.buf.selEndCol = a.buf.cx
	}
}
