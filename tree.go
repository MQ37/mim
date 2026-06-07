// tree.go — file tree: build via git ls-files, expand/collapse, navigation,
// and rendering with ANSI escapes. All stdlib only.

package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// treeShowAll is a package-level flag read by newTree so the fixed-signature
// newTree(rootPath string) can still honour the showAll toggle without
// changing its parameter list. handleTreeKey sets this before calling newTree.
var treeShowAll bool

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

// newTree builds a file tree rooted at rootPath.  It first tries to detect a
// git repository and use "git ls-files" for a fast, correctly-filtered
// listing.  When git is not available it falls back to filepath.WalkDir with
// a best-effort .gitignore parser.
func newTree(rootPath string) (*Tree, error) {
	rootPath, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(rootPath); err != nil || !info.IsDir() {
		if err != nil {
			return nil, err
		}
		return nil, os.ErrNotExist
	}

	t := &Tree{
		rootPath: rootPath,
		showAll:  treeShowAll,
		root: &Node{
			name:  filepath.Base(rootPath),
			path:  rootPath,
			isDir: true,
			open:  true,
		},
	}

	gitRoot, inGit := findGitRoot(rootPath)
	if inGit {
		if err := t.buildFromGit(gitRoot); err != nil {
			// Fall through to walk fallback on any git error.
			inGit = false
		}
	}
	if !inGit {
		if err := t.buildFromWalk(); err != nil {
			return nil, err
		}
	}

	t.buildFlat()
	return t, nil
}

// findGitRoot walks up from path looking for a .git entry (file or directory).
// Returns the repository root and true, or ("", false) if none found.
func findGitRoot(path string) (string, bool) {
	for {
		gitEnt := filepath.Join(path, ".git")
		if _, err := os.Stat(gitEnt); err == nil {
			return path, true
		}
		parent := filepath.Dir(path)
		if parent == path {
			break
		}
		path = parent
	}
	return "", false
}

// buildFromGit runs "git ls-files" from gitRoot and populates t.root.kids.
// When rootPath is a subdirectory of gitRoot, output is filtered to only
// include paths under rootPath and the git-root-relative prefix is stripped.
func (t *Tree) buildFromGit(gitRoot string) error {
	args := []string{"-C", gitRoot, "ls-files", "--cached", "--others"}
	if !t.showAll {
		args = append(args, "--exclude-standard")
	}
	cmd := exec.Command("git", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	// Compute how rootPath sits relative to gitRoot.
	prefix, _ := filepath.Rel(gitRoot, t.rootPath)
	if prefix == "." {
		prefix = ""
	} else {
		prefix += string(os.PathSeparator)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Only include paths under rootPath.
		if prefix != "" {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			line = line[len(prefix):]
		}
		t.insertPath(line, false)
	}
	// Wait for git to finish; ignore exit status for robustness.
	_ = cmd.Wait()
	sortKids(t.root)
	return nil
}

// buildFromWalk uses filepath.WalkDir as a fallback when git is unavailable.
func (t *Tree) buildFromWalk() error {
	var rootPatterns []string
	if !t.showAll {
		rootPatterns = loadGitignore(t.rootPath)
	}

	return filepath.WalkDir(t.rootPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == t.rootPath {
			return nil
		}
		name := d.Name()
		if name == ".git" {
			return filepath.SkipDir
		}

		if !t.showAll && isIgnored(name, rootPatterns) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(t.rootPath, path)
		if err != nil {
			return nil
		}
		t.insertPath(rel, d.IsDir())
		return nil
	})
}

// insertPath parses a relative path (slash-separated) and creates/finds
// intermediate Node entries under t.root.  isDir is only used for the final
// component; intermediate components are always directories.
func (t *Tree) insertPath(rel string, isDir bool) {
	parts := strings.Split(rel, string(os.PathSeparator))
	cur := t.root
	for i, part := range parts {
		if part == "" {
			continue
		}
		last := i == len(parts)-1
		kid := findKid(cur, part)
		if kid == nil {
			kid = &Node{
				name:  part,
				path:  filepath.Join(cur.path, part),
				isDir: !last || isDir,
			}
			cur.kids = append(cur.kids, kid)
		}
		if !last {
			kid.isDir = true // promote to dir if it was created as file earlier
		}
		cur = kid
	}
}

// findKid returns the child of parent with the given name, or nil.
func findKid(parent *Node, name string) *Node {
	for _, k := range parent.kids {
		if k.name == name {
			return k
		}
	}
	return nil
}

// sortKids recursively sorts each level: directories first (alphabetically),
// then files (alphabetically).
func sortKids(n *Node) {
	if n.kids == nil {
		return
	}
	sort.Slice(n.kids, func(i, j int) bool {
		if n.kids[i].isDir != n.kids[j].isDir {
			return n.kids[i].isDir
		}
		return n.kids[i].name < n.kids[j].name
	})
	for _, kid := range n.kids {
		sortKids(kid)
	}
}

// ---------------------------------------------------------------------------
// Flat cache
// ---------------------------------------------------------------------------

// buildFlat rebuilds t.flat from t.root by walking expanded nodes.
func (t *Tree) buildFlat() {
	t.flat = t.flat[:0]
	t.walkFlat(t.root)
	t.dirty = false
}

func (t *Tree) walkFlat(n *Node) {
	t.flat = append(t.flat, n)
	if n.isDir && n.open && n.kids != nil {
		for _, kid := range n.kids {
			t.walkFlat(kid)
		}
	}
}

// ---------------------------------------------------------------------------
// Queries
// ---------------------------------------------------------------------------

// selectedPath returns the full path of the currently selected node,
// or t.rootPath when flat is empty.
func (t *Tree) selectedPath() string {
	if len(t.flat) == 0 || t.cursor < 0 || t.cursor >= len(t.flat) {
		return t.rootPath
	}
	return t.flat[t.cursor].path
}

// ---------------------------------------------------------------------------
// Expand / collapse
// ---------------------------------------------------------------------------

// expandCurrent toggles the open state of the selected directory.  Children
// are loaded (via readDir) on first expansion.  No-op for files.
func (t *Tree) expandCurrent() {
	if len(t.flat) == 0 || t.cursor < 0 || t.cursor >= len(t.flat) {
		return
	}
	n := t.flat[t.cursor]
	if !n.isDir {
		return
	}
	if n.kids == nil {
		n.kids = readDir(n.path, t.showAll)
	}
	n.open = !n.open
	t.dirty = true
}

// collapseCurrent closes the selected directory.  No-op for files.
func (t *Tree) collapseCurrent() {
	if len(t.flat) == 0 || t.cursor < 0 || t.cursor >= len(t.flat) {
		return
	}
	n := t.flat[t.cursor]
	if !n.isDir {
		return
	}
	n.open = false
	t.dirty = true
}

// readDir reads directory entries from dirPath, applies .gitignore filtering
// when showAll is false, and returns a sorted slice of Nodes.
func readDir(dirPath string, showAll bool) []*Node {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	var patterns []string
	if !showAll {
		patterns = loadGitignore(dirPath)
	}

	var nodes []*Node
	for _, e := range entries {
		name := e.Name()
		if name == "." || name == ".." || name == ".git" {
			continue
		}
		if !showAll && isIgnored(name, patterns) {
			continue
		}
		nodes = append(nodes, &Node{
			name:  name,
			path:  filepath.Join(dirPath, name),
			isDir: e.IsDir(),
		})
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].isDir != nodes[j].isDir {
			return nodes[i].isDir
		}
		return nodes[i].name < nodes[j].name
	})
	return nodes
}

// ---------------------------------------------------------------------------
// .gitignore helpers (80 % parser — handles *, #, !, blank lines)
// ---------------------------------------------------------------------------

// loadGitignore reads the .gitignore file in dir and returns the list of
// non-comment, non-blank patterns.
func loadGitignore(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

// isIgnored returns true when name matches one of the patterns (respecting
// leading-! negation).  Uses filepath.Match for glob semantics.
func isIgnored(name string, patterns []string) bool {
	ignored := false
	for _, p := range patterns {
		negate := false
		if strings.HasPrefix(p, "!") {
			negate = true
			p = p[1:]
		}
		if matched, _ := filepath.Match(p, name); matched {
			ignored = !negate
		}
	}
	return ignored
}

// ---------------------------------------------------------------------------
// Keyboard handling (TreeFocus)
// ---------------------------------------------------------------------------

// handleTreeKey processes a keypress when focus is TreeFocus.
func (a *App) handleTreeKey(seq []byte) {
	t := &a.tree

	switch {
	// j — move cursor down
	case bytes.Equal(seq, []byte{'j'}):
		if t.cursor < len(t.flat)-1 {
			t.cursor++
		}
		a.ensureTreeVisible()

	// k — move cursor up
	case bytes.Equal(seq, []byte{'k'}):
		if t.cursor > 0 {
			t.cursor--
		}
		a.ensureTreeVisible()

	// gg — jump to top
	case bytes.Equal(seq, []byte{'g', 'g'}):
		t.cursor = 0
		t.scr = 0

	// G — jump to bottom
	case bytes.Equal(seq, []byte{'G'}):
		if len(t.flat) > 0 {
			t.cursor = len(t.flat) - 1
		}
		a.ensureTreeVisible()

	// Enter — expand/collapse dir or open file
	case bytes.Equal(seq, []byte{'\r'}), bytes.Equal(seq, []byte{'\n'}):
		if len(t.flat) == 0 || t.cursor < 0 || t.cursor >= len(t.flat) {
			return
		}
		n := t.flat[t.cursor]
		if n.isDir {
			t.expandCurrent()
			t.buildFlat()
			a.ensureTreeVisible()
		} else {
			buf, err := openFile(n.path)
			if err != nil {
				a.statusMsg = err.Error()
				return
			}
			a.buf = buf
			a.focus = ViewerFocus
		}

	// Escape — switch focus to viewer
	case bytes.Equal(seq, []byte{0x1b}):
		a.focus = ViewerFocus

	// Ctrl-A (0x01) — toggle showAll and rebuild tree
	case bytes.Equal(seq, []byte{0x01}):
		rootPath := a.tree.rootPath
		treeShowAll = !a.tree.showAll
		newT, err := newTree(rootPath)
		if err == nil {
			a.tree = *newT
			// Clamp cursor into range of the new flat.
			if a.tree.cursor >= len(a.tree.flat) {
				a.tree.cursor = 0
			}
			if a.tree.scr >= len(a.tree.flat) {
				a.tree.scr = 0
			}
		} else {
			// Restore flag on failure so UI stays consistent.
			treeShowAll = a.tree.showAll
		}

	// All other keys — silently ignored.
	}
}

// ensureTreeVisible adjusts t.scr so that t.cursor is visible in the tree
// pane (visible height = termH - 2).
func (a *App) ensureTreeVisible() {
	t := &a.tree
	visibleH := a.termH - 2
	if visibleH < 1 {
		visibleH = 1
	}
	if t.cursor < t.scr {
		t.scr = t.cursor
	}
	if t.cursor >= t.scr+visibleH {
		t.scr = t.cursor - visibleH + 1
	}
	if t.scr < 0 {
		t.scr = 0
	}
	maxScr := len(t.flat) - visibleH
	if maxScr < 0 {
		maxScr = 0
	}
	if t.scr > maxScr {
		t.scr = maxScr
	}
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

// renderTree draws the file tree into out as ANSI-escaped text.  Uses a.treeW
// as the fixed pane width and highlights a.tree.cursor with reverse video.
func (a *App) renderTree(out *bytes.Buffer) {
	t := &a.tree
	visibleH := a.termH - 2
	if visibleH < 1 {
		visibleH = 1
	}

	end := t.scr + visibleH
	if end > len(t.flat) {
		end = len(t.flat)
	}

	treeW := a.treeW
	if treeW < 1 {
		treeW = 1
	}

	for i := t.scr; i < end; i++ {
		n := t.flat[i]
		depth := nodeDepth(t, n)

		// Indent: 2 spaces per depth level.
		indent := strings.Repeat("  ", depth)

		// Prefix: dir markers.
		var prefix string
		if n.isDir {
			if n.open {
				prefix = "▼ "
			} else {
				prefix = "▶ "
			}
		} else {
			prefix = "  "
		}

		// Truncate name to fit inside treeW minus indent and prefix.
		name := n.name
		avail := treeW - depth*2 - 2
		if avail < 1 {
			avail = 1
		}
		if len(name) > avail {
			name = name[:avail]
		}

		line := indent + prefix + name
		// Pad to exactly treeW columns (assumes ASCII / single-byte runes).
		if len(line) < treeW {
			line += strings.Repeat(" ", treeW-len(line))
		}

		if i == t.cursor {
			out.WriteString(ansiReverse)
			out.WriteString(line)
			out.WriteString(ansiReset)
		} else {
			out.WriteString(line)
		}
		out.WriteString(clearToEOL())
		out.WriteByte('\n')
	}

	// Clear any remaining rows below the tree content.
	for i := end; i < t.scr+visibleH; i++ {
		out.WriteString(strings.Repeat(" ", treeW))
		out.WriteString(clearToEOL())
		out.WriteByte('\n')
	}
}

// nodeDepth returns the depth of n relative to the tree root.
// Root is depth 0; direct children are depth 1, etc.
func nodeDepth(t *Tree, n *Node) int {
	if n == t.root {
		return 0
	}
	rel := n.path
	if strings.HasPrefix(rel, t.rootPath) {
		rel = rel[len(t.rootPath):]
	}
	rel = strings.TrimPrefix(rel, string(os.PathSeparator))
	if rel == "" {
		return 0
	}
	return strings.Count(rel, string(os.PathSeparator)) + 1
}
