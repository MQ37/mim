// search_test.go — tests for parseGrepOutput (grep --null output parsing).
package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseGrepOutput(t *testing.T) {
	app := &App{}

	// assertHits compares got against want, reporting path/line/text diffs.
	assertHits := func(t *testing.T, got, want []Hit) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("len(hits) = %d, want %d", len(got), len(want))
			return
		}
		for i := range got {
			if got[i].path != want[i].path {
				t.Errorf("hit[%d].path = %q, want %q", i, got[i].path, want[i].path)
			}
			if got[i].line != want[i].line {
				t.Errorf("hit[%d].line = %d, want %d", i, got[i].line, want[i].line)
			}
			if got[i].text != want[i].text {
				t.Errorf("hit[%d].text = %q, want %q", i, got[i].text, want[i].text)
			}
		}
	}

	t.Run("single hit", func(t *testing.T) {
		raw := []byte("file.go\x001:package main\n")
		hits := app.parseGrepOutput(raw)
		want := []Hit{{path: "file.go", line: 1, text: "package main"}}
		assertHits(t, hits, want)
	})

	t.Run("multiple hits same file", func(t *testing.T) {
		raw := []byte("file.go\x001:line one\nfile.go\x005:line five\n")
		hits := app.parseGrepOutput(raw)
		want := []Hit{
			{path: "file.go", line: 1, text: "line one"},
			{path: "file.go", line: 5, text: "line five"},
		}
		assertHits(t, hits, want)
	})

	t.Run("multiple files", func(t *testing.T) {
		raw := []byte("a.go\x001:foo\nb.go\x002:bar\n")
		hits := app.parseGrepOutput(raw)
		want := []Hit{
			{path: "a.go", line: 1, text: "foo"},
			{path: "b.go", line: 2, text: "bar"},
		}
		assertHits(t, hits, want)
	})

	t.Run("colon in text", func(t *testing.T) {
		raw := []byte("file.go\x0010:x := map[string]int{}\n")
		hits := app.parseGrepOutput(raw)
		want := []Hit{{path: "file.go", line: 10, text: "x := map[string]int{}"}}
		assertHits(t, hits, want)
	})

	t.Run("binary file", func(t *testing.T) {
		raw := []byte("Binary file image.png matches\n")
		hits := app.parseGrepOutput(raw)
		want := []Hit{{path: "image.png", line: 0, text: "[binary]"}}
		assertHits(t, hits, want)
	})

	t.Run("grep error line", func(t *testing.T) {
		raw := []byte("grep: some/dir: Permission denied\n")
		hits := app.parseGrepOutput(raw)
		if len(hits) != 0 {
			t.Errorf("expected 0 hits for grep error line, got %d", len(hits))
		}
	})

	t.Run("empty output", func(t *testing.T) {
		hits := app.parseGrepOutput([]byte{})
		if len(hits) != 0 {
			t.Errorf("expected 0 hits for empty output, got %d", len(hits))
		}
	})

	t.Run("mixed binary and normal", func(t *testing.T) {
		raw := []byte("Binary file a.png matches\na.go\x001:hello\n")
		hits := app.parseGrepOutput(raw)
		want := []Hit{
			{path: "a.png", line: 0, text: "[binary]"},
			{path: "a.go", line: 1, text: "hello"},
		}
		assertHits(t, hits, want)
	})

	t.Run("no trailing newline", func(t *testing.T) {
		raw := []byte("file.go\x001:last line")
		hits := app.parseGrepOutput(raw)
		want := []Hit{{path: "file.go", line: 1, text: "last line"}}
		assertHits(t, hits, want)
	})

	t.Run("deep path", func(t *testing.T) {
		raw := []byte("sub/dir/file.go\x0010:text\n")
		hits := app.parseGrepOutput(raw)
		want := []Hit{{path: "sub/dir/file.go", line: 10, text: "text"}}
		assertHits(t, hits, want)
	})
}

// TestRenderFindResultsRelativePath verifies that find results display paths
// relative to the tree root, not absolute paths.
func TestRenderFindResultsRelativePath(t *testing.T) {
	app := &App{
		TermW:       80,
		TermH:       24,
		TreeVisible: true,
		TreeW:       20,
		Tree:        Tree{RootPath: "/home/user/project"},
		Focus:       FindResultsFocus,
		findHits: []Hit{
			{path: "/home/user/project/src/main.go", line: 10, text: "hello"},
		},
		findCur: 0,
		findScr: 0,
	}

	var buf bytes.Buffer
	app.renderFindResults(&buf)
	out := stripANSI(buf.String())

	// Should contain "src/main.go" (relative), not "/home/user/project/src/main.go".
	if !strings.Contains(out, "src/main.go") {
		t.Errorf("output should contain relative path 'src/main.go', got: %s", out)
	}
	if strings.Contains(out, "/home/user/project") {
		t.Errorf("output should NOT contain absolute root path, got: %s", out)
	}
}

// TestRenderFindResultsNoOverflow verifies that find result lines never
// exceed the viewer width, even with long paths, long line numbers, and
// narrow viewers. This is the fix for text spilling into the tree pane.
func TestRenderFindResultsNoOverflow(t *testing.T) {
	// Narrow viewer: TreeW=20, TermW=30 → viewerW = 30-20-1 = 9.
	app := &App{
		TermW:       30,
		TermH:       24,
		TreeVisible: true,
		TreeW:       20,
		Tree:        Tree{RootPath: "/tmp"},
		Focus:       FindResultsFocus,
		findHits: []Hit{
			{path: "/tmp/very/long/path/to/some/file.go", line: 99999, text: "some long text here"},
		},
		findCur: 0,
		findScr: 0,
	}

	var buf bytes.Buffer
	app.renderFindResults(&buf)
	out := stripANSI(buf.String())

	viewerW := 30 - 20 - 1 // = 9
	for _, line := range splitLines(out) {
		if len([]rune(line)) > viewerW {
			t.Errorf("result line length %d exceeds viewerW %d: %q", len([]rune(line)), viewerW, line)
		}
	}
}

// TestRenderFindResultsWideViewer verifies normal behavior with a wide viewer.
func TestRenderFindResultsWideViewer(t *testing.T) {
	app := &App{
		TermW:       80,
		TermH:       24,
		TreeVisible: true,
		TreeW:       20,
		Tree:        Tree{RootPath: "/tmp"},
		Focus:       FindResultsFocus,
		findHits: []Hit{
			{path: "/tmp/src/main.go", line: 42, text: "func main()"},
		},
		findCur: 0,
		findScr: 0,
	}

	var buf bytes.Buffer
	app.renderFindResults(&buf)
	out := stripANSI(buf.String())

	viewerW := 80 - 20 - 1 // = 59
	for _, line := range splitLines(out) {
		if len([]rune(line)) > viewerW {
			t.Errorf("result line length %d exceeds viewerW %d: %q", len([]rune(line)), viewerW, line)
		}
	}
	// Should contain the relative path and text.
	if !strings.Contains(out, "src/main.go") {
		t.Errorf("output should contain 'src/main.go', got: %s", out)
	}
	if !strings.Contains(out, "func main()") {
		t.Errorf("output should contain 'func main()', got: %s", out)
	}
}

// --- helpers ---

// stripANSI removes ANSI escape sequences from s.
func stripANSI(s string) string {
	var result []byte
	i := 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' && s[j] != 'H' && s[j] != 'K' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		result = append(result, s[i])
		i++
	}
	return string(result)
}

// splitLines splits s by '\n' and returns non-empty lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// TestFindEscapeFromOpenedFileReturnsToResults verifies that when a file is
// opened from find results and ESC is pressed, focus returns to the find
// results (not the tree).
func TestFindEscapeFromOpenedFileReturnsToResults(t *testing.T) {
	app := &App{
		TermW:       80,
		TermH:       24,
		TreeVisible: true,
		Tree:        Tree{RootPath: "/tmp"},
		Focus:       FindResultsFocus,
		findHits:    []Hit{{path: "file.go", line: 1, text: "match"}},
		findCur:     0,
		findScr:     0,
	}

	// Simulate Enter on a find result — open a file.
	dir := t.TempDir()
	fpath := writeTempFile(t, dir, "file.go", "match\n")
	app.findHits[0].path = fpath
	app.Dispatch([]byte{0x0d}) // Enter

	if app.Buf == nil {
		t.Fatal("Enter on find result: Buf should be non-nil")
	}
	if app.Focus != ViewerFocus {
		t.Fatalf("Enter: Focus should be ViewerFocus, got %v", app.Focus)
	}
	if !app.findOpenedFile {
		t.Error("findOpenedFile should be true after opening from find results")
	}

	// ESC in viewer → should return to find results, NOT close to tree.
	app.Dispatch([]byte{0x1b})

	if app.Buf != nil {
		t.Error("ESC from find-opened file: Buf should be nil (file closed)")
	}
	if app.Focus != FindResultsFocus {
		t.Errorf("ESC from find-opened file: Focus should be FindResultsFocus, got %v", app.Focus)
	}
	if app.findOpenedFile {
		t.Error("ESC from find-opened file: findOpenedFile should be false")
	}

	// ESC again in find results → exit find mode, go to tree.
	app.Dispatch([]byte{0x1b})

	if app.Focus != TreeFocus {
		t.Errorf("ESC in find results: Focus should be TreeFocus, got %v", app.Focus)
	}
	if app.findHits != nil {
		t.Error("ESC in find results: findHits should be nil (find state cleared)")
	}
}

// TestFindEscapeRestoresPreviousFile verifies that when a file was open before
// find was started, exiting find restores that file in the viewer.
func TestFindEscapeRestoresPreviousFile(t *testing.T) {
	dir := t.TempDir()
	prevFile := writeTempFile(t, dir, "prev.go", "prev content\n")
	matchFile := writeTempFile(t, dir, "match.go", "match\n")

	prevBuf, _ := openFile(prevFile)

	app := &App{
		TermW:       80,
		TermH:       24,
		TreeVisible: true,
		Tree:        Tree{RootPath: dir},
		Buf:         prevBuf,
		Focus:       ViewerFocus,
	}

	// Start find — saves prevBuf as findPrevBuf.
	app.startFind()
	if app.findPrevBuf != prevBuf {
		t.Fatal("startFind: findPrevBuf should be the previous Buf")
	}

	// Simulate running find and getting results.
	app.findHits = []Hit{{path: matchFile, line: 1, text: "match"}}
	app.findCur = 0
	app.Focus = FindResultsFocus

	// Open the match file from find results.
	app.Dispatch([]byte{0x0d}) // Enter
	if app.Buf == nil {
		t.Fatal("Enter: Buf should be non-nil")
	}
	if app.Buf.Line(0) != "match" {
		t.Fatalf("Enter: should open match.go, got %q", app.Buf.Line(0))
	}

	// ESC from viewer → back to find results.
	app.Dispatch([]byte{0x1b})
	if app.Focus != FindResultsFocus {
		t.Fatalf("ESC from viewer: should be FindResultsFocus, got %v", app.Focus)
	}

	// ESC again → exit find, restore previous file.
	app.Dispatch([]byte{0x1b})
	if app.Focus != ViewerFocus {
		t.Errorf("exit find: Focus should be ViewerFocus (file restored), got %v", app.Focus)
	}
	if app.Buf == nil {
		t.Fatal("exit find: Buf should be restored (non-nil)")
	}
	if app.Buf.Line(0) != "prev content" {
		t.Errorf("exit find: restored file should be prev.go, got line %q", app.Buf.Line(0))
	}
	if app.findHits != nil {
		t.Error("exit find: findHits should be nil")
	}
}

// TestFindEscapeFromInputRestoresPrevious verifies that ESC from the find
// input popup (before running a search) restores the previous view.
func TestFindEscapeFromInputRestoresPrevious(t *testing.T) {
	dir := t.TempDir()
	prevFile := writeTempFile(t, dir, "prev.go", "prev\n")
	prevBuf, _ := openFile(prevFile)

	app := &App{
		TermW:       80,
		TermH:       24,
		TreeVisible: true,
		Tree:        Tree{RootPath: dir},
		Buf:         prevBuf,
		Focus:       ViewerFocus,
	}

	// Start find.
	app.startFind()
	if app.Focus != FindInputFocus {
		t.Fatal("startFind: should be in FindInputFocus")
	}

	// ESC from input → exit find, restore previous file.
	app.Dispatch([]byte{0x1b})

	if app.Focus != ViewerFocus {
		t.Errorf("ESC from find input: Focus should be ViewerFocus, got %v", app.Focus)
	}
	if app.Buf == nil {
		t.Fatal("ESC from find input: Buf should be restored")
	}
	if app.Buf.Line(0) != "prev" {
		t.Errorf("ESC from find input: restored file should be prev.go, got %q", app.Buf.Line(0))
	}
}

// TestRenderFindResultsNoMatchesNoOverflow verifies that the "No matches"
// message (which includes the query) is truncated to the viewer width and
// does not overflow into the tree pane.
func TestRenderFindResultsNoMatchesNoOverflow(t *testing.T) {
	// Narrow viewer + long query = potential overflow.
	app := &App{
		TermW:       40,
		TermH:       24,
		TreeVisible: true,
		TreeW:       15,
		Tree:        Tree{RootPath: "/tmp"},
		Focus:       FindResultsFocus,
		findHits:    nil, // no results
		findQuery:   []rune("this is a very long query that exceeds the narrow viewer width"),
		findCur:     -1,
		findScr:     0,
		findRunning: false,
	}

	var buf bytes.Buffer
	app.renderFindResults(&buf)
	out := stripANSI(buf.String())

	viewerW := 40 - 15 - 1 // = 24
	for _, line := range splitLines(out) {
		if len([]rune(line)) > viewerW {
			t.Errorf("no-matches line length %d exceeds viewerW %d: %q",
				len([]rune(line)), viewerW, line)
		}
	}
}

// TestRenderFindResultsSearchingNoOverflow verifies the "Searching..."
// message fits within the viewer on narrow terminals.
func TestRenderFindResultsSearchingNoOverflow(t *testing.T) {
	app := &App{
		TermW:       20,
		TermH:       24,
		TreeVisible: true,
		TreeW:       15,
		Tree:        Tree{RootPath: "/tmp"},
		Focus:       FindResultsFocus,
		findRunning: true,
	}

	var buf bytes.Buffer
	app.renderFindResults(&buf)
	out := stripANSI(buf.String())

	viewerW := 20 - 15 - 1 // = 4
	for _, line := range splitLines(out) {
		if len([]rune(line)) > viewerW {
			t.Errorf("searching line length %d exceeds viewerW %d: %q",
				len([]rune(line)), viewerW, line)
		}
	}
}
