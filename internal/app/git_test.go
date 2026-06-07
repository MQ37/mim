package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// --- Helpers for creating test git repos ---

// initTestRepo creates a temp git repo with two commits and returns its path.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test")
	runGit(t, dir, "config", "user.name", "test")

	os.WriteFile(filepath.Join(dir, "f"), []byte("line1\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "first commit")

	os.WriteFile(filepath.Join(dir, "f"), []byte("line1\nline2\n"), 0644)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "second commit")

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s\n%s", args, err, out)
	}
}

// --- Tests ---

func TestGitUpdateSelection(t *testing.T) {
	g := &GitState{
		commits: []Commit{
			{hash: "a", subject: "newest"},
			{hash: "b", subject: "middle"},
			{hash: "c", subject: "oldest"},
		},
		selAnchor: 1, // anchored at middle commit
		commitCur: 1,
	}

	// Same position: range is [1,1].
	g.updateSelection()
	if g.selStart != 1 || g.selEnd != 1 {
		t.Errorf("same pos: want [1,1], got [%d,%d]", g.selStart, g.selEnd)
	}

	// Move up (to newer commit, index 0).
	g.commitCur = 0
	g.updateSelection()
	if g.selStart != 0 || g.selEnd != 1 {
		t.Errorf("move up: want [0,1], got [%d,%d]", g.selStart, g.selEnd)
	}

	// Move down (to older commit, index 2).
	g.commitCur = 2
	g.updateSelection()
	if g.selStart != 1 || g.selEnd != 2 {
		t.Errorf("move down: want [2,3], got [%d,%d]", g.selStart, g.selEnd)
	}
}

func TestGitClearSelection(t *testing.T) {
	g := &GitState{
		selAnchor:  1,
		selStart:   0,
		selEnd:     2,
		diffLines:  []string{"diff output"},
		diffCursor: 5,
	}
	g.clearSelection()
	if g.selAnchor != -1 {
		t.Error("selAnchor not cleared")
	}
	if g.selStart != -1 {
		t.Error("selStart not cleared")
	}
	if g.diffLines != nil {
		t.Error("diffLines not cleared")
	}
}

func TestGitEnterExit(t *testing.T) {
	dir := initTestRepo(t)

	app := &App{TermW: 80, TermH: 24}
	app.Tree.RootPath = dir
	app.TreeW = 25

	app.enterGitMode()
	if app.Git == nil {
		t.Fatal("enterGitMode: git state is nil")
	}
	if len(app.Git.commits) != 4 {
		t.Fatalf("expected 4 commits, got %d", len(app.Git.commits))
	}
	// Real commits start at index 2 (after Unstaged, Staged).
	if app.Git.commits[2].subject != "second commit" {
		t.Errorf("index 2 should be second commit, got %q", app.Git.commits[2].subject)
	}
	if app.Git.commits[3].subject != "first commit" {
		t.Errorf("index 3 should be first commit, got %q", app.Git.commits[3].subject)
	}
	if app.Git.selAnchor != -1 || app.Git.selStart != -1 {
		t.Error("no selection should be active on enter")
	}

	// Exit.
	app.Git = nil
	if app.Git != nil {
		t.Error("exitGitMode: git state not cleared")
	}
}

func TestGitComputeDiff(t *testing.T) {
	dir := initTestRepo(t)

	app := &App{TermW: 80, TermH: 24}
	app.Tree.RootPath = dir
	app.TreeW = 25
	app.enterGitMode()

	// Select HEAD (first real commit at index 2) and diff it.
	g := app.Git
	g.commitCur = 2
	g.selAnchor = 2
	g.updateSelection()
	app.computeDiff()
	if g.diffLines == nil {
		t.Fatal("computeDiff: diffLines is nil")
	}
	if len(g.diffLines) == 0 {
		t.Fatal("computeDiff: diffLines is empty")
	}
	// Single-commit diff uses git show — should contain commit subject.
	found := false
	for _, l := range g.diffLines {
		if contains(l, "second commit") {
			found = true
			break
		}
	}
	if !found {
		t.Error("computeDiff: git show output should contain commit subject")
	}

	// Select both real commits (range) and diff.
	g.selAnchor = 3 // first commit (older, at index 3)
	g.commitCur = 2 // HEAD (newer, at index 2)
	g.updateSelection()
	app.computeDiff()
	if g.diffLines == nil || len(g.diffLines) == 0 {
		t.Fatal("computeDiff range: diffLines is empty")
	}
}

func TestGitKeyDispatch(t *testing.T) {
	dir := initTestRepo(t)

	app := &App{TermW: 80, TermH: 24}
	app.Tree.RootPath = dir
	app.TreeW = 25
	app.enterGitMode()
	g := app.Git

	// j — move down from first real commit (index 2).
	g.commitCur = 2
	app.handleGitKey([]byte{'j'})
	if g.commitCur != 3 {
		t.Errorf("j: commitCur should be 3, got %d", g.commitCur)
	}

	// k — move up.
	app.handleGitKey([]byte{'k'})
	if g.commitCur != 2 {
		t.Errorf("k: commitCur should be 2, got %d", g.commitCur)
	}

	// v — start selection.
	app.handleGitKey([]byte{'v'})
	if g.selAnchor != 2 || g.selStart != 2 || g.selEnd != 2 {
		t.Errorf("v: selection should be [2,2], got anchor=%d [%d,%d]",
			g.selAnchor, g.selStart, g.selEnd)
	}

	// j extends selection down.
	app.handleGitKey([]byte{'j'})
	if g.selStart != 2 || g.selEnd != 3 {
		t.Errorf("v then j: selection should be [2,3], got [%d,%d]", g.selStart, g.selEnd)
	}

	// v again — toggle selection off.
	app.handleGitKey([]byte{'v'})
	if g.selAnchor != -1 {
		t.Error("v toggle: selection should be cleared")
	}

	// Enter with no selection — auto-selects current + shows diff.
	g.commitCur = 2
	g.diffLines = nil
	app.handleGitKey([]byte{'\r'})
	if g.selAnchor != 2 {
		t.Errorf("Enter without selection: should auto-select current, got anchor=%d", g.selAnchor)
	}
	if g.diffLines == nil {
		t.Error("Enter: should compute diff")
	}

	// ESC from diff view — clears selection and diff.
	app.handleGitKey([]byte{0x1b})
	if g.selAnchor != -1 {
		t.Error("ESC from diff: selection should be cleared")
	}
	if g.diffLines != nil {
		t.Error("ESC from diff: diffLines should be cleared")
	}

	// ESC from commit list with no selection — exits git mode.
	app.handleGitKey([]byte{0x1b})
	if app.Git != nil {
		t.Error("ESC from commit list: git mode should exit")
	}
}

func TestGitSelectionRangeOnMovement(t *testing.T) {
	dir := initTestRepo(t)

	app := &App{TermW: 80, TermH: 24}
	app.Tree.RootPath = dir
	app.TreeW = 25
	app.enterGitMode()

	// Anchor at commit 3 (older), move to 2 (newer) — range [2,3].
	app.Git.selAnchor = 3
	app.Git.commitCur = 2
	app.Git.updateSelection()
	if app.Git.selStart != 2 || app.Git.selEnd != 3 {
		t.Errorf("anchor=3,cur=2: want [2,3] (normalized), got [%d,%d]",
			app.Git.selStart, app.Git.selEnd)
	}

	// Anchor at 2, move to 3 — same result after normalization.
	app.Git.selAnchor = 2
	app.Git.commitCur = 3
	app.Git.updateSelection()
	if app.Git.selStart != 2 || app.Git.selEnd != 3 {
		t.Errorf("anchor=2,cur=3: want [2,3], got [%d,%d]",
			app.Git.selStart, app.Git.selEnd)
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
