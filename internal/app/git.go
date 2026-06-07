// Git diff view — commit list + range diff.
// Ctrl+G enters/exits. v anchors selection, movement extends, Enter computes diff.

package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	// Virtual commit hash for uncommitted changes (staged + unstaged).
	hashUnstaged = "__unstaged__"
	hashStaged   = "__staged__"
)

// minCommitLineLen = 40-char SHA + space + ≥1 char subject.
const minCommitLineLen = 42

// enterGitMode loads the commit list and switches to git view.
func (a *App) enterGitMode() {
	cmd := exec.Command("git", "-C", a.Tree.RootPath,
		"log", "--format=%H %s", "-n", "200")
	out, err := cmd.Output()
	if err != nil {
		a.StatusMsg = "git log failed: " + err.Error()
		return
	}

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	commits := make([]Commit, 2, len(lines)+2) // +2 for unstaged+staged

	// Prepend virtual entries.
	commits[0] = Commit{hash: hashUnstaged, subject: "Unstaged changes"}
	commits[1] = Commit{hash: hashStaged, subject: "Staged changes"}

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
		a.StatusMsg = "no commits"
		return
	}

	a.Git = &GitState{
		commits:   commits,
		commitCur: 0,
		selAnchor: -1,
		selStart:  -1,
		selEnd:    -1,
	}
	a.Focus = TreeFocus // commit list occupies tree pane
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

// computeDiff runs git diff for the selected range (including virtual entries).
func (a *App) computeDiff() {
	g := a.Git
	if g.selStart < 0 || g.selStart >= len(g.commits) || g.selEnd >= len(g.commits) {
		return
	}

	g.loadingDiff = true
	defer func() { g.loadingDiff = false }()
	g.diffLines = nil
	g.diffCursor = 0
	g.diffScr = 0
	g.diffSelStart = -1

	// Check if selection includes the virtual uncommitted entry.
	if g.selStart == g.selEnd {
		switch g.commits[g.selStart].hash {
		case hashUnstaged:
			a.runUnstagedDiff(&g.diffLines)
			return
		case hashStaged:
			a.runGitDiff(&g.diffLines, "diff", "--cached", "--color=always")
			return
		}
	}

	// Regular commit range.
	oldest := g.commits[g.selEnd].hash
	newest := g.commits[g.selStart].hash

	if g.selStart == g.selEnd {
		if oldest == hashUnstaged || oldest == hashStaged {
			return
		}
		a.runGitShow(oldest, &g.diffLines)
	} else {
		a.runGitRangeDiff(oldest, newest, &g.diffLines)
	}
	g.loadingDiff = false
}

// handleGitKey dispatches keys in git mode.
func (a *App) handleGitKey(seq []byte) {
	g := a.Git
	if g == nil {
		return
	}

	// ESC: in diff/selection → clear selection. Otherwise → exit git mode.
	if bytes.Equal(seq, []byte{0x1b}) {
		if g.selAnchor != -1 {
			g.clearSelection() // also clears diffLines
			return
		}
		a.Git = nil
		a.Focus = TreeFocus
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


func (a *App) ensureCommitVisible() {
	clampScroll(a.Git.commitCur, &a.Git.commitScr, a.TermH-1, len(a.Git.commits))
}

// handleCommitListKey handles keys when browsing the commit list.
func (a *App) handleCommitListKey(seq []byte) {
	g := a.Git
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
		g.commitScr = g.commitCur - (a.TermH - 1) + 1
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
		g.commitCur += (a.TermH - 1) / 2
		if g.commitCur > maxIdx {
			g.commitCur = maxIdx
		}
		if g.selAnchor != -1 {
			g.updateSelection()
		}
		a.ensureCommitVisible()

	case 0x15: // Ctrl-U — half page up
		g.commitCur -= (a.TermH - 1) / 2
		if g.commitCur < 0 {
			g.commitCur = 0
		}
		if g.selAnchor != -1 {
			g.updateSelection()
		}
		a.ensureCommitVisible()
	}
}


func (a *App) ensureDiffVisible() {
	clampScroll(a.Git.diffCursor, &a.Git.diffScr, a.TermH-1, len(a.Git.diffLines))
}

// handleDiffViewKey handles keys when viewing diff output.
func (a *App) handleDiffViewKey(seq []byte) {
	g := a.Git
	if len(seq) != 1 || len(g.diffLines) == 0 {
		return
	}

	maxIdx := len(g.diffLines) - 1

	switch seq[0] {
	case 'j':
		if g.diffCursor < maxIdx {
			g.diffCursor++
		}
		if g.diffSelStart != -1 {
			g.diffSelEnd = g.diffCursor
		}
		a.ensureDiffVisible()

	case 'k':
		if g.diffCursor > 0 {
			g.diffCursor--
		}
		if g.diffSelStart != -1 {
			g.diffSelEnd = g.diffCursor
		}
		a.ensureDiffVisible()

	case 'g':
		g.diffCursor = 0
		g.diffScr = 0
		if g.diffSelStart != -1 {
			g.diffSelEnd = 0
		}

	case 'G':
		g.diffCursor = maxIdx
		g.diffScr = g.diffCursor - (a.TermH - 1) + 1
		if g.diffScr < 0 {
			g.diffScr = 0
		}
		if g.diffSelStart != -1 {
			g.diffSelEnd = maxIdx
		}

	case 0x04: // Ctrl-D
		g.diffCursor += (a.TermH - 1) / 2
		if g.diffCursor > maxIdx {
			g.diffCursor = maxIdx
		}
		if g.diffSelStart != -1 {
			g.diffSelEnd = g.diffCursor
		}
		a.ensureDiffVisible()

	case 0x15: // Ctrl-U
		g.diffCursor -= (a.TermH - 1) / 2
		if g.diffCursor < 0 {
			g.diffCursor = 0
		}
		if g.diffSelStart != -1 {
			g.diffSelEnd = g.diffCursor
		}
		a.ensureDiffVisible()

	case 'v', 'V':
		// Toggle visual selection in diff (linewise only).
		if g.diffSelStart == -1 {
			g.diffSelStart = g.diffCursor
			g.diffSelEnd = g.diffCursor
		} else {
			g.diffSelStart = -1
		}

	case 'y':
		// Yank selected diff lines to clipboard.
		if g.diffSelStart != -1 {
			from, to := g.diffSelStart, g.diffSelEnd
			if from > to {
				from, to = to, from
			}
			if to >= len(g.diffLines) {
				to = len(g.diffLines) - 1
			}
			text := strings.Join(g.diffLines[from:to+1], "\n")
			yankText(text)
			n := to - from + 1
			a.StatusMsg = "yanked " + strconv.Itoa(n) + " lines"
			g.diffSelStart = -1
		}
	}
}


// renderGitView draws the full git mode layout: commit list + diff.
func (a *App) renderGitView(out *bytes.Buffer) {
	g := a.Git
	if g == nil {
		return
	}

	for row := 0; row < a.TermH-1; row++ {
		// Left: commit list (same column as tree pane).
		out.WriteString(cursorMove(row+1, 1))
		a.renderCommitRow(out, row)

		// Separator.
		out.WriteString(cursorMove(row+1, a.TreeW+1))
		out.WriteString(ansiDim)
		out.WriteString("│")
		out.WriteString(ansiReset)

		// Right: diff viewer or placeholder.
		out.WriteString(cursorMove(row+1, a.TreeW+2))
		if g.loadingDiff {
			if row == (a.TermH-2)/2 {
				out.WriteString("Computing diff...")
			}
			out.WriteString(clearToEOL())
		} else if g.diffLines == nil {
			if row == (a.TermH-2)/2 {
				out.WriteString("v — select commits, Enter — view diff")
			}
			out.WriteString(clearToEOL())
		} else if len(g.diffLines) == 0 {
			if row == (a.TermH-2)/2 {
				out.WriteString("No changes")
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
	g := a.Git
	idx := g.commitScr + row
	treeContentW := a.TreeW

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
	g := a.Git
	idx := g.diffScr + row
	availW := a.TermW - a.TreeW - 1

	if idx >= len(g.diffLines) {
		out.WriteString(clearToEOL())
		return
	}

	line := g.diffLines[idx]

	// Selection highlighting (yellow bg).
	inDiffSel := g.diffSelStart != -1 &&
		idx >= min(g.diffSelStart, g.diffSelEnd) &&
		idx <= max(g.diffSelStart, g.diffSelEnd)

	if idx == g.diffCursor {
		out.WriteString(ansiReverse)
	} else if inDiffSel {
		out.WriteString(setBg(colorYellow))
	}

	line = truncateVisible(line, availW)
	out.WriteString(line)

	if idx == g.diffCursor || inDiffSel {
		out.WriteString(ansiReset)
	}

	out.WriteString(clearToEOL())
}

func (a *App) runGitDiff(lines *[]string, args ...string) {
	cmd := exec.Command("git", append([]string{"-C", a.Tree.RootPath}, args...)...)
	out, _ := cmd.Output()
	raw := strings.TrimRight(string(out), "\n")
	if raw == "" {
		*lines = []string{} // empty diff, not nil
	} else {
		*lines = strings.Split(raw, "\n")
	}
}

func (a *App) runGitShow(hash string, lines *[]string) {
	cmd := exec.Command("git", "-C", a.Tree.RootPath, "show", "--color=always", hash)
	out, _ := cmd.Output()
	raw := strings.TrimRight(string(out), "\n")
	if raw == "" {
		*lines = []string{}
	} else {
		*lines = strings.Split(raw, "\n")
	}
}

func (a *App) runGitRangeDiff(oldest, newest string, lines *[]string) {
	rangeSpec := oldest + "~1.." + newest
	cmd := exec.Command("git", "-C", a.Tree.RootPath, "diff", "--color=always", rangeSpec)
	out, err := cmd.Output()
	if err != nil {
		cmd2 := exec.Command("git", "-C", a.Tree.RootPath, "diff", "--color=always", oldest+".."+newest)
		out, _ = cmd2.Output()
	}
	raw := strings.TrimRight(string(out), "\n")
	if raw == "" {
		*lines = []string{}
	} else {
		*lines = strings.Split(raw, "\n")
	}
}

func (a *App) runUnstagedDiff(lines *[]string) {
	*lines = nil

	// Tracked changes: git diff HEAD.
	cmd := exec.Command("git", "-C", a.Tree.RootPath, "diff", "HEAD", "--color=always")
	out, _ := cmd.Output()
	raw := strings.TrimRight(string(out), "\n")
	if raw != "" {
		*lines = append(*lines, strings.Split(raw, "\n")...)
	}

	// Untracked files: git ls-files --others --exclude-standard.
	untrackedCmd := exec.Command("git", "-C", a.Tree.RootPath,
		"ls-files", "--others", "--exclude-standard")
	untrackedOut, _ := untrackedCmd.Output()
	untracked := strings.Split(strings.TrimSpace(string(untrackedOut)), "\n")

	for _, f := range untracked {
		if f == "" {
			continue
		}
		// Read the file content and show as additions.
		data, err := os.ReadFile(filepath.Join(a.Tree.RootPath, f))
		if err != nil {
			continue
		}
		// Diff header.
		green := "\033[32m"
		reset := "\033[0m"
		if len(*lines) > 0 {
			*lines = append(*lines, "")
		}
		*lines = append(*lines, "diff --git a/"+f+" b/"+f)
		*lines = append(*lines, "new file mode 100644")
		*lines = append(*lines, "--- /dev/null")
		*lines = append(*lines, "+++ b/"+f)
		for _, l := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
			*lines = append(*lines, green+"+"+reset+l)
		}
	}

	if len(*lines) == 0 {
		*lines = []string{}
	}
}
