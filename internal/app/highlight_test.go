package app

import (
	"strings"
	"testing"
)

func TestHighlightLineGo(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantSegs  []Segment
		wantBlock bool
	}{
		{
			name:     "package keyword",
			line:     "package main",
			wantSegs: []Segment{{0, 7, hlKeyword}},
		},
		{
			name:     "var keyword and number",
			line:     "var x = 42",
			wantSegs: []Segment{{0, 3, hlKeyword}, {8, 10, hlNumber}},
		},
		{
			name:     "line comment",
			line:     "// comment",
			wantSegs: []Segment{{0, 10, hlComment}},
		},
		{
			name:     "double-quoted string",
			line:     `x := "string"`,
			wantSegs: []Segment{{5, 13, hlString}},
		},
		{
			name:     "backtick raw string",
			line:     "x := `raw string`",
			wantSegs: []Segment{{5, 17, hlString}},
		},
		{
			name:     "block comment",
			line:     "/* block comment */",
			wantSegs: []Segment{{0, 19, hlComment}},
		},
		{
			name:     "func keyword",
			line:     "func main() {",
			wantSegs: []Segment{{0, 4, hlKeyword}},
		},
		{
			name:     "return keyword and nil builtin",
			line:     "return nil",
			wantSegs: []Segment{{0, 6, hlKeyword}, {7, 10, hlBuiltin}},
		},
		{
			name:     "var keyword and string type",
			line:     "var x string",
			wantSegs: []Segment{{0, 3, hlKeyword}, {6, 12, hlTypeName}},
		},
		{
			name:     "mixed numbers and line comment",
			line:     "x := 1 + 2 // sum",
			wantSegs: []Segment{{5, 6, hlNumber}, {9, 10, hlNumber}, {11, 17, hlComment}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSegs, gotBlock := highlightLine(tt.line, langGo, false)
			if gotBlock != tt.wantBlock {
				t.Errorf("blockComment = %v, want %v", gotBlock, tt.wantBlock)
			}
			if !segmentsEqual(gotSegs, tt.wantSegs) {
				t.Errorf("segments:\n  got  %v\n  want %v", gotSegs, tt.wantSegs)
			}
		})
	}
}

func TestHighlightLinePython(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantSegs []Segment
	}{
		{
			name:     "def keyword",
			line:     "def foo():",
			wantSegs: []Segment{{0, 3, hlKeyword}},
		},
		{
			name:     "single-quoted string",
			line:     "x = 'hello'",
			wantSegs: []Segment{{4, 11, hlString}},
		},
		{
			name:     "hash comment",
			line:     "# comment",
			wantSegs: []Segment{{0, 9, hlComment}},
		},
		{
			name:     "float number",
			line:     "x = 3.14",
			wantSegs: []Segment{{4, 8, hlNumber}},
		},
		{
			name:     "return keyword",
			line:     "return x",
			wantSegs: []Segment{{0, 6, hlKeyword}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSegs, gotBlock := highlightLine(tt.line, langPython, false)
			if gotBlock {
				t.Errorf("blockComment = true, want false")
			}
			if !segmentsEqual(gotSegs, tt.wantSegs) {
				t.Errorf("segments:\n  got  %v\n  want %v", gotSegs, tt.wantSegs)
			}
		})
	}
}

func TestHighlightLineTypeScript(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantSegs []Segment
	}{
		{
			name:     "const keyword and number",
			line:     "const x = 5;",
			wantSegs: []Segment{{0, 5, hlKeyword}, {10, 11, hlNumber}},
		},
		{
			name:     "line comment",
			line:     "// comment",
			wantSegs: []Segment{{0, 10, hlComment}},
		},
		{
			name:     "let keyword and string",
			line:     `let s = "hi";`,
			wantSegs: []Segment{{0, 3, hlKeyword}, {8, 12, hlString}},
		},
		{
			name:     "function keyword",
			line:     "function foo() {",
			wantSegs: []Segment{{0, 8, hlKeyword}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSegs, gotBlock := highlightLine(tt.line, langTypeScript, false)
			if gotBlock {
				t.Errorf("blockComment = true, want false")
			}
			if !segmentsEqual(gotSegs, tt.wantSegs) {
				t.Errorf("segments:\n  got  %v\n  want %v", gotSegs, tt.wantSegs)
			}
		})
	}
}

func TestApplyHighlight(t *testing.T) {
	// Empty / nil segments: returns line unchanged.
	t.Run("nil segments", func(t *testing.T) {
		got := applyHighlight("hello world", nil)
		if got != "hello world" {
			t.Errorf("got %q, want %q", got, "hello world")
		}
	})

	t.Run("empty segments", func(t *testing.T) {
		got := applyHighlight("hello world", []Segment{})
		if got != "hello world" {
			t.Errorf("got %q, want %q", got, "hello world")
		}
	})

	// Single keyword segment.
	t.Run("single keyword segment", func(t *testing.T) {
		got := applyHighlight("hello world", []Segment{{0, 5, hlKeyword}})
		wantPrefix := setFg(hlKeyword) + "hello" + ansiReset
		if !strings.Contains(got, wantPrefix) {
			t.Errorf("output missing keyword highlight: got %q", got)
		}
		if !strings.HasSuffix(got, " world") {
			t.Errorf("output missing trailing text: got %q", got)
		}
	})

	// Two non-overlapping segments.
	t.Run("two segments", func(t *testing.T) {
		line := "if (\"test\")"
		segs := []Segment{
			{0, 2, hlKeyword},
			{4, 10, hlString},
		}
		got := applyHighlight(line, segs)

		// Check that keyword "if" is properly wrapped.
		if !strings.Contains(got, setFg(hlKeyword)+"if"+ansiReset) {
			t.Errorf("missing keyword highlight: got %q", got)
		}
		// Check that string "\"test\"" is properly wrapped.
		if !strings.Contains(got, setFg(hlString)+`"test"`+ansiReset) {
			t.Errorf("missing string highlight: got %q", got)
		}
		// Check that the parenthesis between segments is preserved uncolored.
		if !strings.Contains(got, ansiReset+" ("+setFg(hlString)) {
			t.Errorf("missing uncolored ' (' between segments: got %q", got)
		}
	})
}

// segmentsEqual compares two segment slices for equality.
func segmentsEqual(a, b []Segment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
