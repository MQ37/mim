package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMouseSGR(t *testing.T) {
	cases := []struct {
		name    string
		seq     []byte
		want    MouseEvent
		wantOK  bool
	}{
		{
			name:   "left click at 5,10",
			seq:    []byte{0x1b, '[', '<', '0', ';', '5', ';', '1', '0', 'M'},
			want:   MouseEvent{Button: 0, X: 5, Y: 10, Release: false},
			wantOK: true,
		},
		{
			name:   "left release at 5,10",
			seq:    []byte{0x1b, '[', '<', '0', ';', '5', ';', '1', '0', 'm'},
			want:   MouseEvent{Button: 0, X: 5, Y: 10, Release: true},
			wantOK: true,
		},
		{
			name:   "wheel up at 20,3",
			seq:    []byte{0x1b, '[', '<', '6', '4', ';', '2', '0', ';', '3', 'M'},
			want:   MouseEvent{Button: 64, X: 20, Y: 3, Release: false},
			wantOK: true,
		},
		{
			name:   "wheel down at 20,3",
			seq:    []byte{0x1b, '[', '<', '6', '5', ';', '2', '0', ';', '3', 'M'},
			want:   MouseEvent{Button: 65, X: 20, Y: 3, Release: false},
			wantOK: true,
		},
		{
			name:   "large coords",
			seq:    []byte{0x1b, '[', '<', '0', ';', '2', '0', '0', ';', '8', '0', 'M'},
			want:   MouseEvent{Button: 0, X: 200, Y: 80, Release: false},
			wantOK: true,
		},
		{
			name:   "arrow key is not mouse",
			seq:    []byte{0x1b, '[', 'A'},
			wantOK: false,
		},
		{
			name:   "bare escape is not mouse",
			seq:    []byte{0x1b},
			wantOK: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseMouseSGR(c.seq)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if !c.wantOK {
				return
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestMouseWheelScrollsViewer verifies wheel events scroll the viewport
// (not the cursor line-by-line). The cursor stays put until it would scroll
// out of view, then sticks to the edge.
func TestMouseWheelScrollsViewer(t *testing.T) {
	app := &App{
		Buf: &Buf{
			lines:        makeLines(100),
			cy:           10,
			cx:           0,
			scr:          0,
			selStartLine: -1,
		},
		TermW:       80,
		TermH:       24,
		TreeVisible: true,
		Tree: Tree{
			flat: []*Node{{name: "x"}},
			RootPath: "/tmp",
		},
		Focus: ViewerFocus,
	}
	// Wheel down at col 50 (viewer pane). Viewport should advance; cursor
	// (cy=10) is still well inside the window so it must NOT move.
	app.Dispatch([]byte{0x1b, '[', '<', '6', '5', ';', '5', '0', ';', '5', 'M'})
	if app.Buf.scr != wheelScrollLines {
		t.Errorf("wheel down: scr should be %d, got %d", wheelScrollLines, app.Buf.scr)
	}
	if app.Buf.cy != 10 {
		t.Errorf("wheel down: cy should stay 10 (still visible), got %d", app.Buf.cy)
	}

	// Wheel back up — viewport returns to top, cursor unchanged.
	app.Dispatch([]byte{0x1b, '[', '<', '6', '4', ';', '5', '0', ';', '5', 'M'})
	if app.Buf.scr != 0 {
		t.Errorf("wheel up: scr should be 0, got %d", app.Buf.scr)
	}
	if app.Buf.cy != 10 {
		t.Errorf("wheel up: cy should stay 10, got %d", app.Buf.cy)
	}
}

// TestMouseWheelViewerSticksCursorToEdge verifies that once the viewport
// scrolls past the cursor, the cursor sticks to the bottom edge (scrolling
// down) or top edge (scrolling up) rather than disappearing off-screen.
func TestMouseWheelViewerSticksCursorToEdge(t *testing.T) {
	app := &App{
		Buf: &Buf{
			lines:        makeLines(100),
			cy:           5, // near the top of a 23-row viewport
			cx:           0,
			scr:          0,
			selStartLine: -1,
		},
		TermW:       80,
		TermH:       24, // viewport height = 23
		TreeVisible: true,
		Tree: Tree{
			flat: []*Node{{name: "x"}},
			RootPath: "/tmp",
		},
		Focus: ViewerFocus,
	}
	// Scroll down several notches until the cursor (cy=5) is above the
	// viewport. Each notch is wheelScrollLines (3); after 4 notches scr=12,
	// so cy=5 < 12 and must stick to scr.
	for i := 0; i < 4; i++ {
		app.Dispatch([]byte{0x1b, '[', '<', '6', '5', ';', '5', '0', ';', '5', 'M'})
	}
	if app.Buf.cy != app.Buf.scr {
		t.Errorf("after scrolling past cursor: cy should stick to scr=%d, got cy=%d",
			app.Buf.scr, app.Buf.cy)
	}
}

// TestMouseWheelScrollsTree verifies wheel events over the tree pane move the
// tree cursor.
func TestMouseWheelScrollsTree(t *testing.T) {
	nodes := makeFlatNodes(10)
	app := &App{
		TermW:       80,
		TermH:       24,
		TreeVisible: true,
		TreeW:       20,
		Tree: Tree{
			flat:     nodes,
			cursor:   3,
			scr:      0,
			RootPath: "/tmp",
		},
		Focus: TreeFocus,
	}
	// Wheel down at col 5 (inside tree pane).
	app.Dispatch([]byte{0x1b, '[', '<', '6', '5', ';', '5', ';', '5', 'M'})
	if app.Tree.cursor != 4 {
		t.Errorf("wheel down over tree: cursor should be 4, got %d", app.Tree.cursor)
	}
	// Wheel up.
	app.Dispatch([]byte{0x1b, '[', '<', '6', '4', ';', '5', ';', '5', 'M'})
	if app.Tree.cursor != 3 {
		t.Errorf("wheel up over tree: cursor should be 3, got %d", app.Tree.cursor)
	}
}

// TestMouseClickViewerMovesCursor verifies clicking the viewer moves the
// buffer cursor to the clicked line/column.
func TestMouseClickViewerMovesCursor(t *testing.T) {
	app := &App{
		Buf: &Buf{
			lines:        []string{"hello world", "foo bar baz", "qux"},
			cy:           0,
			cx:           0,
			scr:          0,
			selStartLine: -1,
		},
		TermW:       80,
		TermH:       24,
		TreeVisible: true,
		TreeW:       20,
		Focus:       TreeFocus,
	}
	// Click at row 1 (0-indexed) = line 1 ("foo bar baz"), col 25 in terminal.
	// viewerStartCol = TreeW+2 = 22. visCol = 25-22 = 3 → byte offset 3 (" bar baz").
	app.Dispatch([]byte{0x1b, '[', '<', '0', ';', '2', '5', ';', '2', 'M'})
	if app.Buf.cy != 1 {
		t.Errorf("click viewer: cy should be 1, got %d", app.Buf.cy)
	}
	if app.Buf.cx != 3 {
		t.Errorf("click viewer: cx should be 3, got %d", app.Buf.cx)
	}
	if app.Focus != ViewerFocus {
		t.Errorf("click viewer: focus should be ViewerFocus, got %v", app.Focus)
	}
}

func TestMouseClickTreeOpensFile(t *testing.T) {
	// Build a tiny tree with one file node pointing at a temp file.
	dir := t.TempDir()
	fpath := writeTempFile(t, dir, "hello.txt", "line1\nline2\n")

	root := &Node{name: dir, path: dir, isDir: true, open: true,
		kids: []*Node{{name: "hello.txt", path: fpath, isDir: false}}}
	flat := []*Node{root, root.kids[0]}

	app := &App{
		TermW:       80,
		TermH:       24,
		TreeVisible: true,
		TreeW:       20,
		Tree: Tree{
			flat:     flat,
			cursor:   0,
			scr:      0,
			RootPath: dir,
		},
		Focus: TreeFocus,
	}
	// Click row 1 (0-indexed) = flat[1] = the file. Col 5 (inside tree pane).
	app.Dispatch([]byte{0x1b, '[', '<', '0', ';', '5', ';', '2', 'M'})
	if app.Tree.cursor != 1 {
		t.Fatalf("click tree: cursor should be 1, got %d", app.Tree.cursor)
	}
	if app.Buf == nil {
		t.Fatal("click tree file: Buf should be non-nil (file opened)")
	}
	if app.Focus != ViewerFocus {
		t.Errorf("click tree file: focus should be ViewerFocus, got %v", app.Focus)
	}
	// "line1\nline2\n" splits into ["line1","line2",""] — 3 elements.
	if app.Buf.Line(0) != "line1" {
		t.Errorf("opened file first line should be line1, got %q", app.Buf.Line(0))
	}
}

// TestMouseClickTreeTogglesDir verifies clicking a directory toggles it.
func TestMouseClickTreeTogglesDir(t *testing.T) {
	dir := t.TempDir()
	subdir := dir + "/sub"
	writeTempFile(t, subdir, "a.txt", "a\n") // creates subdir + file

	root := &Node{name: dir, path: dir, isDir: true, open: true,
		kids: []*Node{{name: "sub", path: subdir, isDir: true, open: false, kids: nil}}}
	flat := []*Node{root, root.kids[0]}

	app := &App{
		TermW:       80,
		TermH:       24,
		TreeVisible: true,
		TreeW:       20,
		Tree: Tree{
			root:     root,
			flat:     flat,
			cursor:   0,
			scr:      0,
			RootPath: dir,
			showAll:  true, // don't filter by .gitignore in readDir
		},
		Focus: TreeFocus,
	}

	// Click row 1 (0-indexed) = flat[1] = the subdirectory.
	app.Dispatch([]byte{0x1b, '[', '<', '0', ';', '5', ';', '2', 'M'})
	if app.Tree.cursor != 1 {
		t.Fatalf("click tree dir: cursor should be 1, got %d", app.Tree.cursor)
	}
	sub := app.Tree.flat[1]
	if !sub.open {
		t.Error("click tree dir: directory should be expanded (open) after click")
	}
	if app.Focus != TreeFocus {
		t.Errorf("click tree dir: focus should stay TreeFocus, got %v", app.Focus)
	}
}

func TestVisualToByte(t *testing.T) {
	cases := []struct {
		line   string
		visCol int
		want   int
	}{
		{"hello", 0, 0},
		{"hello", 1, 1},
		{"hello", 5, 5},
		{"hello", 99, 5}, // clamp to end
		{"a\tbc", 0, 0},  // 'a' at col 0
		{"a\tbc", 1, 1},  // 'a' still (col 1 < tab at col1→4)
		{"a\tbc", 2, 1},  // inside the tab expansion (cols 1-3 are the tab)
		{"a\tbc", 4, 2},  // 'b' at col 4
		{"a\tbc", 5, 3},  // 'c' at col 5
	}
	for _, c := range cases {
		got := visualToByte(c.line, c.visCol)
		if got != c.want {
			t.Errorf("visualToByte(%q, %d) = %d, want %d", c.line, c.visCol, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func makeFlatNodes(n int) []*Node {
	nodes := make([]*Node, n)
	for i := 0; i < n; i++ {
		nodes[i] = &Node{name: "n" + itoa(i), isDir: false}
	}
	return nodes
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// writeTempFile creates file `name` inside dir (creating any intermediate
// directories) and returns its full path. Used by the tree click tests.
func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := dir + "/" + name
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}