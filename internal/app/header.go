// header.go — top header bar with clickable mode tabs (Files / Git).
//
// The header occupies terminal row 1. The content area starts at row 2,
// pushing all pane rendering down by one row (see contentHeight / the
// row+2 offset in render.go). Clicking a tab switches modes — the same
// actions as Ctrl-G (Git) / Ctrl-G again (back to Files).

package app

import "bytes"

// headerTab describes one clickable tab in the top header bar.
type headerTab struct {
	label    string
	startCol int // 1-indexed inclusive
	endCol   int // 1-indexed inclusive
	active   bool
}

// headerTabs returns the current set of tabs with their screen positions.
// "Files" is active in normal (tree+viewer) mode; "Git" is active in git
// diff view mode.
func (a *App) headerTabs() []headerTab {
	tabs := []headerTab{
		{label: "Files", active: a.Git == nil},
		{label: "Git", active: a.Git != nil},
	}
	col := 2 // 1-indexed; leave column 1 as left padding
	for i := range tabs {
		w := len(tabs[i].label) + 2 // " label "
		tabs[i].startCol = col
		tabs[i].endCol = col + w - 1
		col += w + 2 // 2-space gap between tabs
	}
	return tabs
}

// contentHeight returns the number of usable content rows, excluding the
// top header (1 row) and the bottom status bar (1 row).
func (a *App) contentHeight() int {
	h := a.TermH - 2
	if h < 1 {
		h = 1
	}
	return h
}

// renderHeader draws the header bar on terminal row 1.
// Inactive tabs are shown on the header background; the active tab is
// rendered with reverse video so it stands out.
func (a *App) renderHeader(out *bytes.Buffer) {
	out.WriteString(cursorMove(1, 1))
	out.WriteString(setBg(colorStatus))
	out.WriteString(setFg(colorWhite))

	tabs := a.headerTabs()
	col := 1

	for _, tab := range tabs {
		// Gap before this tab (left padding / inter-tab spacing).
		if tab.startCol > col {
			out.WriteString(spaces(tab.startCol - col))
			col = tab.startCol
		}
		if tab.active {
			out.WriteString(ansiReverse)
			out.WriteString(" ")
			out.WriteString(tab.label)
			out.WriteString(" ")
			out.WriteString(ansiReset)
			// Re-enable header colours after the reset.
			out.WriteString(setBg(colorStatus))
			out.WriteString(setFg(colorWhite))
		} else {
			out.WriteString(" ")
			out.WriteString(tab.label)
			out.WriteString(" ")
		}
		col += len(tab.label) + 2
	}

	// Pad the rest of the header row with the background colour.
	if col <= a.TermW {
		out.WriteString(spaces(a.TermW - col + 1))
	}
	out.WriteString(ansiReset)
	out.WriteString(clearToEOL())
}

// handleHeaderClick processes a mouse click on the header row (y == 1).
// It matches x against each tab's column range and switches modes.
func (a *App) handleHeaderClick(x int) {
	for _, tab := range a.headerTabs() {
		if x < tab.startCol || x > tab.endCol {
			continue
		}
		switch tab.label {
		case "Files":
			// Leave git mode if active.
			if a.Git != nil {
				a.Git = nil
			}
			if a.TreeVisible {
				a.Focus = TreeFocus
			} else if a.Buf != nil {
				a.Focus = ViewerFocus
			}

		case "Git":
			// Enter git mode if not already active.
			if a.Git == nil {
				a.enterGitMode()
			}
		}
		return
	}
}

// spaces returns a string of n space characters.
func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return repeatSpace(n)
}

// repeatSpace allocates a string of n spaces (kept separate so spaces()
// can inline the zero/negative fast-path).
func repeatSpace(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}