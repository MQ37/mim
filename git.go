// Git diff view — commit list + range diff.
// Ctrl+G enters/exits. v anchors selection, movement extends, Enter computes diff.

package main

import (
	"bytes"
	"os/exec"
	"strings"
)

// minCommitLineLen = 40-char SHA + space + ≥1 char subject.
const minCommitLineLen = 42

// enterGitMode loads the commit list and switches to git view.
func (a *App) enterGitMode() {
	cmd := exec.Command("git", "-C", a.tree.rootPath,
		"log", "--format=%H %s", "-n", "200")
	out, err := cmd.Output()
	if err != nil {
		a.statusMsg = "git log failed: " + err.Error()
		return
	}

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	commits := make([]Commit, 0, len(lines))
	for _, line := range lines {
		if len(line) < minCommitLineLen {
			continue
		}
		commits = append(commits, Commit{
			hash:    line[:40],
			subject: line[41:],
		})
	}

	if len(commits) == 0 {
		a.statusMsg = "no commits"
		return
	}

	a.git = &GitState{
		commits:   commits,
		commitCur: 0,
		selAnchor: -1,
		selStart:  -1,
		selEnd:    -1,
	}
	a.focus = TreeFocus // commit list occupies tree pane
}

// exitGitMode clears git state and returns to normal view.
func (a *App) exitGitMode() {
	a.git = nil
	a.focus = TreeFocus
}

// updateSelection recomputes selStart/selEnd from selAnchor and commitCur.
// The anchor stays fixed; the cursor extends. Normalizes so selStart <= selEnd.
func (g *GitState) updateSelection() {
	if g.selAnchor < 0 {
		return
	}
	if g.commitCur < g.selAnchor {
		g.selStart = g.commitCur
		g.selEnd = g.selAnchor
	} else {
		g.selStart = g.selAnchor
		g.selEnd = g.commitCur
	}
}

// clearSelection clears the visual range.
func (g *GitState) clearSelection() {
	g.selAnchor = -1
	g.selStart = -1
	g.selEnd = -1
	g.diffLines = nil
}

// computeDiff runs git diff for the selected commit range.
func (a *App) computeDiff() {
	g := a.git
	if g.selStart < 0 || g.selStart >= len(g.commits) || g.selEnd >= len(g.commits) {
		return
	}

	// git log outputs newest-first, so smaller index = newer commit.
	oldest := g.commits[g.selEnd].hash   // larger index = older
	newest := g.commits[g.selStart].hash // smaller index = newer

	g.loadingDiff = true
	g.diffLines = nil
	g.diffCursor = 0
	g.diffScr = 0

	var out []byte
	var err error

	if g.selStart == g.selEnd {
		// Single commit: use git show (handles root commit correctly).
		cmd := exec.Command("git", "-C", a.tree.rootPath,
			"show", "--color=always", oldest)
		out, err = cmd.Output()
	} else {
		// Range: oldest~1..newest
		rangeSpec := oldest + "~1.." + newest
		cmd := exec.Command("git", "-C", a.tree.rootPath,
			"diff", "--color=always", rangeSpec)
		out, err = cmd.Output()
		if err != nil {
			// Fallback for root commit in range.
			cmd2 := exec.Command("git", "-C", a.tree.rootPath,
				"diff", "--color=always", oldest+".."+newest)
			out, err = cmd2.Output()
		}
	}

	g.loadingDiff = false

	raw := strings.TrimRight(string(out), "\n")
	if raw == "" {
		g.diffLines = nil
	} else {
		g.diffLines = strings.Split(raw, "\n")
	}

	if err != nil && g.diffLines == nil {
		a.statusMsg = "git diff failed: " + err.Error()
	}
}

// handleGitKey dispatches keys in git mode.
func (a *App) handleGitKey(seq []byte) {
	g := a.git
	if g == nil {
		return
	}

	// ESC behavior:
	//   In diff view → back to commit list (clear selection)
	//   In commit list with selection → clear selection
	//   In commit list without selection → exit git mode
	if bytes.Equal(seq, []byte{0x1b}) {
		if g.selAnchor != -1 && g.diffLines != nil {
			// Viewing diff: back to commit list, clear selection.
			g.clearSelection()
			return
		}
		if g.selAnchor != -1 {
			g.clearSelection()
			return
		}
		a.exitGitMode()
		return
	}

	// In commit list mode.
	if g.diffLines == nil {
		a.handleCommitListKey(seq)
		return
	}

	// In diff view mode.
	a.handleDiffViewKey(seq)
}


// ensureCommitVisible adjusts commitScr so commitCur is on screen.
func (a *App) ensureCommitVisible() {
	g := a.git
	visibleH := a.termH - 1
	if visibleH < 1 {
		visibleH = 1
	}
	if g.commitCur < g.commitScr {
		g.commitScr = g.commitCur
	}
	if g.commitCur >= g.commitScr+visibleH {
		g.commitScr = g.commitCur - visibleH + 1
	}
	if g.commitScr < 0 {
		g.commitScr = 0
	}
}

// handleCommitListKey handles keys when browsing the commit list.
func (a *App) handleCommitListKey(seq []byte) {
	g := a.git
	if len(seq) != 1 || len(g.commits) == 0 {
		return
	}

	maxIdx := len(g.commits) - 1

	switch seq[0] {
	case 'j':
		if g.commitCur < maxIdx {
			g.commitCur++
		}
		if g.selAnchor != -1 {
			g.updateSelection()
		}
		a.ensureCommitVisible()

	case 'k':
		if g.commitCur > 0 {
			g.commitCur--
		}
		if g.selAnchor != -1 {
			g.updateSelection()
		}
		a.ensureCommitVisible()

	case 'g':
		g.commitCur = 0
		if g.selAnchor != -1 {
			g.updateSelection()
		}
		g.commitScr = 0

	case 'G':
		g.commitCur = maxIdx
		if g.selAnchor != -1 {
			g.updateSelection()
		}
		g.commitScr = g.commitCur - (a.termH - 1) + 1
		if g.commitScr < 0 {
			g.commitScr = 0
		}

	case 'v', 'V':
		if g.selAnchor == -1 {
			g.selAnchor = g.commitCur
			g.updateSelection()
		} else {
			g.clearSelection()
		}

	case '\r', '\n': // Enter
		if g.selAnchor == -1 {
			// No selection: select current commit and show its diff.
			g.selAnchor = g.commitCur
			g.updateSelection()
		}
		a.computeDiff()

	case 0x04: // Ctrl-D — half page down
		g.commitCur += (a.termH - 1) / 2
		if g.commitCur > maxIdx {
			g.commitCur = maxIdx
		}
		if g.selAnchor != -1 {
			g.updateSelection()
		}
		a.ensureCommitVisible()

	case 0x15: // Ctrl-U — half page up
		g.commitCur -= (a.termH - 1) / 2
		if g.commitCur < 0 {
			g.commitCur = 0
		}
		if g.selAnchor != -1 {
			g.updateSelection()
		}
		a.ensureCommitVisible()
	}
}


// ensureDiffVisible adjusts diffScr so diffCursor is on screen.
func (a *App) ensureDiffVisible() {
	g := a.git
	visibleH := a.termH - 1
	if visibleH < 1 {
		visibleH = 1
	}
	if g.diffCursor < g.diffScr {
		g.diffScr = g.diffCursor
	}
	if g.diffCursor >= g.diffScr+visibleH {
		g.diffScr = g.diffCursor - visibleH + 1
	}
	if g.diffScr < 0 {
		g.diffScr = 0
	}
}

// handleDiffViewKey handles keys when viewing diff output.
func (a *App) handleDiffViewKey(seq []byte) {
	g := a.git
	if len(seq) != 1 || len(g.diffLines) == 0 {
		return
	}

	maxIdx := len(g.diffLines) - 1

	switch seq[0] {
	case 'j':
		if g.diffCursor < maxIdx {
			g.diffCursor++
		}
		a.ensureDiffVisible()

	case 'k':
		if g.diffCursor > 0 {
			g.diffCursor--
		}
		a.ensureDiffVisible()

	case 'g':
		g.diffCursor = 0
		g.diffScr = 0

	case 'G':
		g.diffCursor = maxIdx
		g.diffScr = g.diffCursor - (a.termH - 1) + 1
		if g.diffScr < 0 {
			g.diffScr = 0
		}

	case 0x04: // Ctrl-D — half page down
		g.diffCursor += (a.termH - 1) / 2
		if g.diffCursor > maxIdx {
			g.diffCursor = maxIdx
		}
		a.ensureDiffVisible()

	case 0x15: // Ctrl-U — half page up
		g.diffCursor -= (a.termH - 1) / 2
		if g.diffCursor < 0 {
			g.diffCursor = 0
		}
		a.ensureDiffVisible()
	}
}


// renderGitView draws the full git mode layout: commit list + diff.
func (a *App) renderGitView(out *bytes.Buffer) {
	g := a.git
	if g == nil {
		return
	}

	for row := 0; row < a.termH-1; row++ {
		// Left: commit list (same column as tree pane).
		out.WriteString(cursorMove(row+1, 1))
		a.renderCommitRow(out, row)

		// Separator.
		out.WriteString(cursorMove(row+1, a.treeW+1))
		out.WriteString(ansiDim)
		out.WriteString("│")
		out.WriteString(ansiReset)

		// Right: diff viewer or placeholder.
		out.WriteString(cursorMove(row+1, a.treeW+2))
		if g.loadingDiff {
			if row == (a.termH-2)/2 {
				out.WriteString("Computing diff...")
			}
			out.WriteString(clearToEOL())
		} else if g.diffLines == nil {
			if row == (a.termH-2)/2 {
				out.WriteString("v — select commits, Enter — view diff")
			}
			out.WriteString(clearToEOL())
		} else if row < len(g.diffLines)-g.diffScr {
			a.renderDiffRow(out, row)
		} else {
			// Past the end of diff — clear the row.
			out.WriteString(clearToEOL())
		}
	}
}

// renderCommitRow draws one row of the commit list.
func (a *App) renderCommitRow(out *bytes.Buffer, row int) {
	g := a.git
	idx := g.commitScr + row
	treeContentW := a.treeW

	if idx >= len(g.commits) {
		out.WriteString(strings.Repeat(" ", treeContentW))
		out.WriteString(clearToEOL())
		return
	}

	c := g.commits[idx]
	shortHash := c.hash[:8]
	line := shortHash + " " + c.subject

	// Check if this commit is in the visual selection range.
	inSelection := g.selAnchor != -1 &&
		idx >= g.selStart && idx <= g.selEnd

	if inSelection || idx == g.commitCur {
		out.WriteString(ansiReverse)
	}

	line = truncate(padRight(line, treeContentW), treeContentW)
	out.WriteString(line)

	if inSelection || idx == g.commitCur {
		out.WriteString(ansiReset)
	}
	out.WriteString(clearToEOL())
}

// renderDiffRow draws one row of the diff output.
func (a *App) renderDiffRow(out *bytes.Buffer, row int) {
	g := a.git
	idx := g.diffScr + row
	availW := a.termW - a.treeW - 1

	if idx >= len(g.diffLines) {
		out.WriteString(clearToEOL())
		return
	}

	line := g.diffLines[idx]

	if idx == g.diffCursor {
		out.WriteString(ansiReverse)
	}

	line = truncateVisible(line, availW)
	out.WriteString(line)

	if idx == g.diffCursor {
		out.WriteString(ansiReset)
	}

	out.WriteString(clearToEOL())
}
