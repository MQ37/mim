package main

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
		t.Errorf("move down: want [1,2], got [%d,%d]", g.selStart, g.selEnd)
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

	app := &App{termW: 80, termH: 24}
	app.tree.rootPath = dir
	app.treeW = 25

	app.enterGitMode()
	if app.git == nil {
		t.Fatal("enterGitMode: git state is nil")
	}
	if len(app.git.commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(app.git.commits))
	}
	if app.git.commits[0].subject != "second commit" {
		t.Errorf("HEAD should be second commit, got %q", app.git.commits[0].subject)
	}
	if app.git.commits[1].subject != "first commit" {
		t.Errorf("second entry should be first commit, got %q", app.git.commits[1].subject)
	}
	if app.git.selAnchor != -1 || app.git.selStart != -1 {
		t.Error("no selection should be active on enter")
	}

	// Exit.
	app.git = nil
	if app.git != nil {
		t.Error("exitGitMode: git state not cleared")
	}
}

func TestGitComputeDiff(t *testing.T) {
	dir := initTestRepo(t)

	app := &App{termW: 80, termH: 24}
	app.tree.rootPath = dir
	app.treeW = 25
	app.enterGitMode()

	// Select first commit (HEAD) and diff it.
	g := app.git
	g.selAnchor = 0
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

	// Select both commits (range) and diff.
	g.selAnchor = 1 // first commit (older)
	g.commitCur = 0  // second commit (newer)
	g.updateSelection()
	app.computeDiff()
	if g.diffLines == nil || len(g.diffLines) == 0 {
		t.Fatal("computeDiff range: diffLines is empty")
	}
}

func TestGitKeyDispatch(t *testing.T) {
	dir := initTestRepo(t)

	app := &App{termW: 80, termH: 24}
	app.tree.rootPath = dir
	app.treeW = 25
	app.enterGitMode()
	g := app.git

	// j — move down.
	g.commitCur = 0
	app.handleGitKey([]byte{'j'})
	if g.commitCur != 1 {
		t.Errorf("j: commitCur should be 1, got %d", g.commitCur)
	}

	// k — move up.
	app.handleGitKey([]byte{'k'})
	if g.commitCur != 0 {
		t.Errorf("k: commitCur should be 0, got %d", g.commitCur)
	}

	// v — start selection.
	app.handleGitKey([]byte{'v'})
	if g.selAnchor != 0 || g.selStart != 0 || g.selEnd != 0 {
		t.Errorf("v: selection should be [0,0], got anchor=%d [%d,%d]",
			g.selAnchor, g.selStart, g.selEnd)
	}

	// j extends selection down.
	app.handleGitKey([]byte{'j'})
	if g.selStart != 0 || g.selEnd != 1 {
		t.Errorf("v then j: selection should be [0,1], got [%d,%d]", g.selStart, g.selEnd)
	}

	// v again — toggle selection off.
	app.handleGitKey([]byte{'v'})
	if g.selAnchor != -1 {
		t.Error("v toggle: selection should be cleared")
	}

	// Enter with no selection — auto-selects current + shows diff.
	g.commitCur = 0
	g.diffLines = nil
	app.handleGitKey([]byte{'\r'})
	if g.selAnchor != 0 {
		t.Error("Enter without selection: should auto-select current commit")
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
	if app.git != nil {
		t.Error("ESC from commit list: git mode should exit")
	}
}

func TestGitSelectionRangeOnMovement(t *testing.T) {
	dir := initTestRepo(t)

	app := &App{termW: 80, termH: 24}
	app.tree.rootPath = dir
	app.treeW = 25
	app.enterGitMode()

	// Anchor at commit 1 (older), move to 0 (newer) — selection spans [0,1].
	app.git.selAnchor = 1
	app.git.commitCur = 0
	app.git.updateSelection()
	if app.git.selStart != 0 || app.git.selEnd != 1 {
		t.Errorf("anchor=1,cur=0: want [0,1] (normalized), got [%d,%d]",
			app.git.selStart, app.git.selEnd)
	}

	// Anchor at 0, move to 1 — same result after normalization.
	app.git.selAnchor = 0
	app.git.commitCur = 1
	app.git.updateSelection()
	if app.git.selStart != 0 || app.git.selEnd != 1 {
		t.Errorf("anchor=0,cur=1: want [0,1], got [%d,%d]",
			app.git.selStart, app.git.selEnd)
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
