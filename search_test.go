// search_test.go — tests for parseGrepOutput (grep --null output parsing).
package main

import (
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
