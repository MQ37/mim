// Key dispatch and viewer-mode vim keybindings.

package app

import (
	"bytes"
	"strconv"
	"strings"
)

// viewerPendingG tracks whether the previous key in viewer mode was 'g',
// enabling detection of the two-key 'gg' sequence.
var viewerPendingG bool

// --- Central dispatch ---

// dispatch is the central input router. Called on every keypress.
// First checks global keys (q, Ctrl-T, Ctrl-F).
// Then routes to mode-specific handler based on a.Focus.
func (a *App) Dispatch(seq []byte) {
	// Global keys first (take priority everywhere).
	if a.handleGlobalKey(seq) {
		return
	}

	// Git mode — route to git handler.
	if a.Git != nil {
		a.handleGitKey(seq)
		return
	}

	// When tree is hidden, ViewerFocus is the only focus.
	if !a.TreeVisible && a.Focus == TreeFocus {
		a.Focus = ViewerFocus
	}

	// Reset pending-g when Focus is not viewer (e.g., after Ctrl-T away).
	if a.Focus != ViewerFocus {
		viewerPendingG = false
	}

	// Mode-specific dispatch.
	switch a.Focus {
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

// handleGlobalKey handles keys that work in ANY Focus mode.
// Returns true if the key was consumed (preventing further dispatch).
func (a *App) handleGlobalKey(seq []byte) bool {
	if len(seq) != 1 {
		return false
	}
	switch seq[0] {
	case 'q': // 0x71 — quit
		a.Quit = true
		return true
	case 0x14: // Ctrl-T — toggle Focus between TreeFocus and ViewerFocus
		if a.Focus == TreeFocus {
			if a.Buf != nil {
				a.Focus = ViewerFocus
			}
		} else if a.TreeVisible {
			a.Focus = TreeFocus
		}
		return true
	case 0x05: // Ctrl-E — toggle tree visibility
		a.TreeVisible = !a.TreeVisible
		if a.TreeVisible {
			a.Focus = TreeFocus
		} else if a.Buf != nil {
			a.Focus = ViewerFocus
		} else {
			// No file open — keep tree visible, Focus stays on tree.
			a.TreeVisible = true
		}
		return true
	case 0x07: // Ctrl-G — toggle git diff view
		if a.Git != nil {
			a.Git = nil
			a.Focus = TreeFocus
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

// handleViewerKey handles keys when Focus is ViewerFocus.
// Implements vim-like navigation, visual selection, and yank.
func (a *App) handleViewerKey(seq []byte) {
	if a.Buf == nil {
		return
	}

	// --- Escape clears selection or is ignored ---
	if bytes.Equal(seq, []byte{0x1b}) {
		if a.Buf.selStartLine != -1 {
			a.Buf.selStartLine = -1 // clear selection
		}
		viewerPendingG = false
		return
	}

	// --- gg detection (two-key sequence) ---
	if len(seq) == 1 && seq[0] == 'g' {
		if viewerPendingG {
			// gg — jump to first line
			a.Buf.cy = 0
			a.Buf.clampCursor()
			a.Buf.ensureVisible(a.TermH - 1)
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
		a.Buf.cx--
		a.Buf.clampCursor()
		isMovement = true
	case 'j': // 0x6a — down
		a.Buf.cy++
		a.Buf.clampCursor()
		a.Buf.ensureVisible(a.TermH - 1)
		isMovement = true
	case 'k': // 0x6b — up
		a.Buf.cy--
		a.Buf.clampCursor()
		a.Buf.ensureVisible(a.TermH - 1)
		isMovement = true
	case 'l': // 0x6c — right
		a.Buf.cx++
		a.Buf.clampCursor()
		isMovement = true
	case 'G': // 0x47 — jump to last line
		a.Buf.cy = a.Buf.LineCount() - 1
		a.Buf.clampCursor()
		a.Buf.ensureVisible(a.TermH - 1)
		isMovement = true
	case '0': // 0x30 — jump to column 0
		a.Buf.cx = 0
		a.Buf.clampCursor()
		isMovement = true
	case '$': // 0x24 — jump to end of line
		a.Buf.cx = len(a.Buf.Line(a.Buf.cy))
		a.Buf.clampCursor()
		isMovement = true
	case 0x04: // Ctrl-D — half page down
		a.Buf.cy += (a.TermH - 1) / 2
		a.Buf.clampCursor()
		a.Buf.ensureVisible(a.TermH - 1)
		isMovement = true
	case 0x15: // Ctrl-U — half page up
		a.Buf.cy -= (a.TermH - 1) / 2
		a.Buf.clampCursor()
		a.Buf.ensureVisible(a.TermH - 1)
		isMovement = true
	case 0x02: // Ctrl-B — page up
		a.Buf.cy -= (a.TermH - 1)
		a.Buf.clampCursor()
		a.Buf.ensureVisible(a.TermH - 1)
		isMovement = true

	// --- Visual mode ---
	case 'v': // 0x76 — charwise visual mode
		a.Buf.selStartLine = a.Buf.cy
		a.Buf.selStartCol = a.Buf.cx
		a.Buf.selEndLine = a.Buf.cy
		a.Buf.selEndCol = a.Buf.cx
		a.Buf.selLinewise = false
	case 'V': // 0x56 — linewise visual mode
		a.Buf.selStartLine = a.Buf.cy
		a.Buf.selStartCol = 0
		a.Buf.selEndLine = a.Buf.cy
		a.Buf.selEndCol = 0
		a.Buf.selLinewise = true

	// --- Yank ---
	case 'y': // 0x79 — yank selection
		if a.Buf.selStartLine != -1 {
			if err := a.Buf.yankToClipboard(); err != nil {
				a.StatusMsg = "yank failed"
			} else {
				n := strings.Count(a.Buf.visualText(), "\n") + 1
				a.StatusMsg = "yanked " + strconv.Itoa(n) + " lines"
			}
			a.Buf.selStartLine = -1 // clear selection
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
	if a.Buf.selStartLine == -1 {
		return
	}
	a.Buf.selEndLine = a.Buf.cy
	if !a.Buf.selLinewise {
		a.Buf.selEndCol = a.Buf.cx
	}
}
