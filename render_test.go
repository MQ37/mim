package main

import "testing"

func TestPadRight(t *testing.T) {
	cases := []struct {
		input string
		width int
		want  string
	}{
		{"hello", 10, "hello     "},
		{"hello", 3, "hello"},
		{"", 5, "     "},
		{"abc", 3, "abc"},
	}
	for _, c := range cases {
		got := padRight(c.input, c.width)
		if got != c.want {
			t.Errorf("padRight(%q, %d) = %q, want %q", c.input, c.width, got, c.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		input string
		width int
		want  string
	}{
		{"hello world", 5, "hello"},
		{"abc", 10, "abc"},
		{"", 5, ""},
		{"hello", 0, ""},
		{"hello", -1, ""},
		{"café", 3, "caf"}, // 4 runes, truncate to 3
	}
	for _, c := range cases {
		got := truncate(c.input, c.width)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.input, c.width, got, c.want)
		}
	}
}

func TestTabExpand(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"\t", "    "},
		{"a\tb", "a   b"},
		{"\t\t", "        "},
		{"ab\tcd", "ab  cd"},     // col 2, tab to 4 → 2 spaces
		{"abc\t", "abc "},        // col 3, tab to 4 → 1 space
		{"abcd\t", "abcd    "},   // col 4, tab to 8 → 4 spaces
		{"", ""},
	}
	for _, c := range cases {
		got := tabExpand(c.input)
		if got != c.want {
			t.Errorf("tabExpand(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestTabExpandAnsi(t *testing.T) {
	// ANSI codes should be preserved, tabs expanded ignoring ANSI codes.
	cases := []struct {
		input string
		want  string
	}{
		{"a\tb", "a   b"}, // no ANSI, same as tabExpand
		{"a\x1b[31m\t\x1b[0mb", "a\x1b[31m   \x1b[0mb"}, // ANSI around tab
		{"\x1b[32m\t\x1b[0m", "\x1b[32m    \x1b[0m"},     // ANSI before tab
	}
	for _, c := range cases {
		got := tabExpandAnsi(c.input)
		if got != c.want {
			t.Errorf("tabExpandAnsi(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestTruncateVisible(t *testing.T) {
	cases := []struct {
		input string
		width int
		want  string
	}{
		{"hello world", 5, "hello"},
		{"abc", 10, "abc"},
		// ANSI codes at start should be preserved if within visible range.
		// truncateVisible appends ansiReset when truncating mid-ANSI to prevent bleed.
		{"\x1b[31mhello\x1b[0m world", 5, "\x1b[31mhello" + ansiReset},
		// ANSI mid-text, truncate at visible boundary, auto-close.
		{"he\x1b[31mllo\x1b[0m world", 5, "he\x1b[31mllo" + ansiReset},
	}
	for _, c := range cases {
		got := truncateVisible(c.input, c.width)
		if got != c.want {
			t.Errorf("truncateVisible(%q, %d) = %q, want %q", c.input, c.width, got, c.want)
		}
	}
}
