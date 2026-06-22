package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTreeBuildFromPaths(t *testing.T) {
	// Test tree construction from a flat list of paths.
	paths := []string{
		"main.go",
		"internal/helper.go",
		"internal/util/format.go",
		"README.md",
	}

	tree := buildTreeFromPaths(paths)

	if tree.root == nil {
		t.Fatal("root is nil")
	}
	if !tree.root.isDir {
		t.Error("root should be a directory")
	}
	if !tree.root.open {
		t.Error("root should be expanded")
	}
	if len(tree.root.kids) != 3 {
		// main.go, internal/, README.md
		t.Errorf("root should have 3 children, got %d", len(tree.root.kids))
	}

	// Rebuild flat.
	tree.buildFlat()

	if len(tree.flat) == 0 {
		t.Fatal("flat is empty")
	}

	// Verify dirs-first sort.
	flatNames := make([]string, len(tree.flat))
	for i, n := range tree.flat {
		flatNames[i] = n.name
	}
	t.Logf("flat: %v", flatNames)

	// internal/ should be before main.go and README.md (dirs first).
	internalIdx := indexOf(flatNames, "internal")
	mainIdx := indexOf(flatNames, "main.go")
	readmeIdx := indexOf(flatNames, "README.md")

	if internalIdx < 0 || mainIdx < 0 || readmeIdx < 0 {
		t.Fatal("missing expected children in flat")
	}
	if internalIdx > mainIdx || internalIdx > readmeIdx {
		t.Error("dirs should come before files in flat")
	}
	// README.md (R=0x52) < main.go (m=0x6d) in ASCII.
	if readmeIdx > mainIdx {
		t.Errorf("files should be alphabetically sorted, got README.md at %d, main.go at %d", readmeIdx, mainIdx)
	}

	// Expand internal/ — should show helper.go and util/.
	internal := tree.root.kids[indexOf(childNames(tree.root.kids), "internal")]
	if internal == nil || !internal.isDir {
		t.Fatal("expected internal/ to be a directory in tree")
	}

	tree.cursor = internalIdx
	tree.expandCurrent()
	tree.buildFlat()

	// Should now have 5 entries in flat (1 extra level from internal).
	if len(tree.flat) < 5 {
		t.Errorf("after expand, flat should have >=5 entries, got %d", len(tree.flat))
	}
}

func TestEnsureTreeVisible(t *testing.T) {
	// Build a tree with 50 UNIQUE entries.
	paths := make([]string, 50)
	for i := 0; i < 50; i++ {
		paths[i] = "file" + string(rune('A'+i/26)) + string(rune('a'+i%26)) + ".go"
	}

	tree := buildTreeFromPaths(paths)
	tree.buildFlat()
	if len(tree.flat) < 51 {
		t.Fatalf("flat too short: %d entries (need >=51 for root + 50 files)", len(tree.flat))
	}

	app := &App{Tree: *tree, TermH: 24}

	// Invariant: after ensureTreeVisible, cursor must be within [scr, scr+visibleH).
	visibleH := app.contentHeight()

	// Cursor at 0, scr at 0 — no change.
	app.Tree.cursor = 0
	app.Tree.scr = 0
	app.ensureTreeVisible()
	if app.Tree.scr != 0 {
		t.Errorf("cursor=0, scr should stay 0, got %d", app.Tree.scr)
	}

	// Cursor at 46, scr at 0 — ensureVisible must scroll to show it.
	app.Tree.cursor = 46
	app.Tree.scr = 0
	app.ensureTreeVisible()
	if app.Tree.cursor < app.Tree.scr || app.Tree.cursor >= app.Tree.scr+visibleH {
		t.Errorf("cursor %d not visible with scr=%d (visibleH=%d, flat=%d)", app.Tree.cursor, app.Tree.scr, visibleH, len(app.Tree.flat))
	}

	// Cursor below current scr — must scroll up.
	app.Tree.cursor = 5
	app.Tree.scr = 30
	app.ensureTreeVisible()
	if app.Tree.cursor < app.Tree.scr || app.Tree.cursor >= app.Tree.scr+visibleH {
		t.Errorf("cursor %d not visible with scr=%d (visibleH=%d)", app.Tree.cursor, app.Tree.scr, visibleH)
	}

	// Scr beyond upper bound — must clamp.
	app.Tree.cursor = 10
	app.Tree.scr = 100
	app.ensureTreeVisible()
	maxScr := len(app.Tree.flat) - visibleH
	if maxScr < 0 {
		maxScr = 0
	}
	if app.Tree.scr > maxScr {
		t.Errorf("scr=%d exceeds maxScr=%d", app.Tree.scr, maxScr)
	}
}

func TestNewTree(t *testing.T) {
	// Create a temp directory with files.
	dir := t.TempDir()

	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test"), 0644)
	os.WriteFile(filepath.Join(dir, "src", "helper.go"), []byte("package main"), 0644)

	tree, err := NewTree(dir)
	if err != nil {
		t.Fatalf("NewTree(%q) failed: %v", dir, err)
	}

	if tree.root == nil {
		t.Fatal("root is nil")
	}
	if !tree.root.isDir {
		t.Error("root should be a directory")
	}
	if tree.RootPath != dir {
		t.Errorf("rootPath = %q, want %q", tree.RootPath, dir)
	}
}

// --- helpers ---

// buildTreeFromPaths creates a Tree from a list of relative paths.
// This simulates what newTree does with git ls-files output.
func buildTreeFromPaths(paths []string) *Tree {
	t := &Tree{
		root: &Node{
			name:  ".",
			path:  ".",
			isDir: true,
			open:  true,
		},
		RootPath: ".",
		showAll:  false,
	}

	for _, p := range paths {
		insertPath(t.root, p)
	}

	// Sort root children (dirs first, then alphabetical).
	sortKids(t.root)

	return t
}

// insertPath inserts a path into the tree, creating intermediate dir nodes.
func insertPath(root *Node, path string) {
	parts := splitPath(path)
	cur := root
	for i, part := range parts {
		isLast := i == len(parts)-1
		// Find or create child.
		var child *Node
		for _, c := range cur.kids {
			if c.name == part {
				child = c
				break
			}
		}
		if child == nil {
			child = &Node{
				name:  part,
				path:  filepath.Join(cur.path, part),
				isDir: !isLast,
			}
			if child.isDir {
				child.kids = []*Node{} // empty = loaded, no children yet
				child.open = false
			}
			cur.kids = append(cur.kids, child)
		}
		cur = child
	}
}

func splitPath(p string) []string {
	if p == "" {
		return nil
	}
	var parts []string
	for {
		dir, file := filepath.Split(p)
		if file != "" {
			parts = append([]string{file}, parts...)
		}
		if dir == "" || dir == string(filepath.Separator) {
			break
		}
		p = dir[:len(dir)-1] // strip trailing separator
	}
	return parts
}

// sortKids (already defined in tree.go)

func nodeLess(a, b *Node) bool {
	if a.isDir != b.isDir {
		return a.isDir // dirs first
	}
	return a.name < b.name
}

func childNames(kids []*Node) []string {
	names := make([]string, len(kids))
	for i, k := range kids {
		names[i] = k.name
	}
	return names
}

func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}
