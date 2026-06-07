package app

import (
	"fmt"
	"testing"
)

func newTestBuf(lines []string) *Buf {
	return &Buf{lines: lines, cx: 0, cy: 0, scr: 0, selStartLine: -1}
}

func TestClampCursor(t *testing.T) {
	// Case 1: empty line list with only [""], cursor out of bounds.
	b := newTestBuf([]string{""})
	b.cy = 5
	b.cx = 100
	b.clampCursor()
	if b.cy != 0 || b.cx != 0 {
		t.Errorf("case 1: expected (cy=0, cx=0), got (cy=%d, cx=%d)", b.cy, b.cx)
	}

	// Case 2: two lines, cursor way past end — clamps to last line and its length.
	b = newTestBuf([]string{"hello", "world"})
	b.cy = 5
	b.cx = 100
	b.clampCursor()
	if b.cy != 1 || b.cx != 5 {
		t.Errorf("case 2: expected (cy=1, cx=5), got (cy=%d, cx=%d)", b.cy, b.cx)
	}

	// Case 3: negative cursor — clamps to (0,0).
	b = newTestBuf([]string{"a", "b", "c"})
	b.cy = -5
	b.cx = -5
	b.clampCursor()
	if b.cy != 0 || b.cx != 0 {
		t.Errorf("case 3: expected (cy=0, cx=0), got (cy=%d, cx=%d)", b.cy, b.cx)
	}

	// Case 4: already valid — no change.
	b = newTestBuf([]string{""})
	b.cy = 0
	b.cx = 0
	b.clampCursor()
	if b.cy != 0 || b.cx != 0 {
		t.Errorf("case 4: expected (cy=0, cx=0), got (cy=%d, cx=%d)", b.cy, b.cx)
	}

	// Case 5: normal line, cx past end — clamps to line length.
	b = newTestBuf([]string{"abc"})
	b.cy = 0
	b.cx = 10
	b.clampCursor()
	if b.cy != 0 || b.cx != 3 {
		t.Errorf("case 5: expected (cy=0, cx=3), got (cy=%d, cx=%d)", b.cy, b.cx)
	}
}

func TestEnsureVisible(t *testing.T) {
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i)
	}
	const vpHeight = 24

	// Case 1: cy above viewport — scroll up so cy becomes top line.
	b := newTestBuf(lines)
	b.cy = 0
	b.scr = 50
	b.ensureVisible(vpHeight)
	if b.scr != 0 {
		t.Errorf("case 1: expected scr=0, got scr=%d", b.scr)
	}

	// Case 2: cy below viewport — scroll down so cy becomes bottom line.
	// Implementation: scr = cy - vpHeight + 1 = 50 - 24 + 1 = 27.
	b = newTestBuf(lines)
	b.cy = 50
	b.scr = 0
	b.ensureVisible(vpHeight)
	if b.scr != 27 {
		t.Errorf("case 2: expected scr=27, got scr=%d", b.scr)
	}

	// Case 3: cy at end — scroll to show last line at bottom.
	// scr = 99 - 24 + 1 = 76.
	b = newTestBuf(lines)
	b.cy = 99
	b.scr = 0
	b.ensureVisible(vpHeight)
	if b.scr != 76 {
		t.Errorf("case 3: expected scr=76, got scr=%d", b.scr)
	}

	// Case 4: cy already visible within viewport — no change.
	b = newTestBuf(lines)
	b.cy = 50
	b.scr = 40
	b.ensureVisible(vpHeight)
	if b.scr != 40 {
		t.Errorf("case 4: expected scr=40, got scr=%d", b.scr)
	}

	// Edge: empty buffer — scr stays 0.
	b = newTestBuf([]string{})
	b.cy = 0
	b.scr = 0
	b.ensureVisible(vpHeight)
	if b.scr != 0 {
		t.Errorf("empty buffer: expected scr=0, got scr=%d", b.scr)
	}
}

func TestVisualText(t *testing.T) {
	lines := []string{"line1", "line2", "line3", "line4"}

	// Case 1: no selection — returns "".
	b := newTestBuf(lines)
	if s := b.visualText(); s != "" {
		t.Errorf("case 1: expected \"\", got %q", s)
	}

	// Case 2: linewise (V) selection from line 1 to line 2.
	b = newTestBuf(lines)
	b.selStartLine = 1
	b.selEndLine = 2
	b.selLinewise = true
	if s := b.visualText(); s != "line2\nline3" {
		t.Errorf("case 2: expected %q, got %q", "line2\nline3", s)
	}

	// Case 3: charwise (v) single-line selection, cols 1–5 exclusive.
	// "line1"[1:5] = "ine1".
	b = newTestBuf(lines)
	b.selStartLine = 0
	b.selStartCol = 1
	b.selEndLine = 0
	b.selEndCol = 5
	b.selLinewise = false
	if s := b.visualText(); s != "ine1" {
		t.Errorf("case 3: expected %q, got %q", "ine1", s)
	}

	// Case 4: backward linewise selection — normalized before joining.
	// selStartLine=2, selEndLine=0 → after swap: start=0, end=2.
	b = newTestBuf(lines)
	b.selStartLine = 2
	b.selStartCol = 0
	b.selEndLine = 0
	b.selEndCol = 0
	b.selLinewise = true
	if s := b.visualText(); s != "line1\nline2\nline3" {
		t.Errorf("case 4: expected %q, got %q", "line1\nline2\nline3", s)
	}

	// Case 5: multi-line charwise forward selection.
	// startLine=0, startCol=1 → "ine1"
	// middle: "line2"
	// endLine=2, endCol=3 → "lin"
	// result: "ine1\nline2\nlin"
	b = newTestBuf(lines)
	b.selStartLine = 0
	b.selStartCol = 1
	b.selEndLine = 2
	b.selEndCol = 3
	b.selLinewise = false
	expected := "ine1\nline2\nlin"
	if s := b.visualText(); s != expected {
		t.Errorf("case 5: expected %q, got %q", expected, s)
	}

	// Case 6: backward charwise selection — normalized, same result as case 5.
	b = newTestBuf(lines)
	b.selStartLine = 2
	b.selStartCol = 3
	b.selEndLine = 0
	b.selEndCol = 1
	b.selLinewise = false
	if s := b.visualText(); s != expected {
		t.Errorf("case 6: expected %q, got %q", expected, s)
	}
}
