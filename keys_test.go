package main

import "testing"

func TestGgStateMachine(t *testing.T) {
	app := &App{
		buf: &Buf{
			lines:        makeLines(100),
			cy:           50,
			cx:           0,
			scr:          40,
			selStartLine: -1,
		},
		termW: 80,
		termH: 24,
		focus: ViewerFocus,
	}

	// First 'g' — sets pendingG, no movement.
	app.dispatch([]byte{'g'})
	if !viewerPendingG {
		t.Error("after first 'g', viewerPendingG should be true")
	}
	if app.buf.cy != 50 {
		t.Errorf("after first 'g', cy should still be 50, got %d", app.buf.cy)
	}

	// Second 'g' — triggers gg, jumps to top.
	app.dispatch([]byte{'g'})
	if viewerPendingG {
		t.Error("after gg, viewerPendingG should be false")
	}
	if app.buf.cy != 0 {
		t.Errorf("after gg, cy should be 0, got %d", app.buf.cy)
	}
	if app.buf.scr != 0 {
		t.Errorf("after gg, scr should be 0, got %d", app.buf.scr)
	}

	// 'g' followed by non-'g' — pendingG cleared, normal key processed.
	app.buf.cy = 10
	app.dispatch([]byte{'g'})
	if !viewerPendingG {
		t.Error("after 'g', viewerPendingG should be true")
	}
	app.dispatch([]byte{'j'}) // move down
	if viewerPendingG {
		t.Error("after 'j', viewerPendingG should be false")
	}
	if app.buf.cy != 11 {
		t.Errorf("after 'g' then 'j', cy should be 11, got %d", app.buf.cy)
	}

	// 'g' followed by escape — pendingG cleared by escape.
	app.dispatch([]byte{'g'})
	app.dispatch([]byte{0x1b})
	if viewerPendingG {
		t.Error("after escape following 'g', viewerPendingG should be false")
	}

	// Multi-byte sequence after 'g' — pendingG cleared.
	app.dispatch([]byte{'g'})
	app.dispatch([]byte{0x1b, '[', 'A'}) // arrow up
	if viewerPendingG {
		t.Error("after multi-byte key following 'g', viewerPendingG should be false")
	}
}

func TestVisualModeTransitions(t *testing.T) {
	app := &App{
		buf: &Buf{
			lines:        []string{"hello", "world", "foo", "bar"},
			cy:           1,
			cx:           2,
			scr:          0,
			selStartLine: -1,
		},
		termW: 80,
		termH: 24,
		focus: ViewerFocus,
	}

	// Enter charwise visual mode.
	app.dispatch([]byte{'v'})
	if app.buf.selStartLine != 1 {
		t.Errorf("v: selStartLine should be 1, got %d", app.buf.selStartLine)
	}
	if app.buf.selStartCol != 2 {
		t.Errorf("v: selStartCol should be 2, got %d", app.buf.selStartCol)
	}
	if app.buf.selLinewise {
		t.Error("v: selLinewise should be false")
	}

	// Move down in visual mode — selection extends.
	app.dispatch([]byte{'j'})
	if app.buf.selEndLine != 2 {
		t.Errorf("v then j: selEndLine should be 2, got %d", app.buf.selEndLine)
	}
	if app.buf.cy != 2 {
		t.Errorf("v then j: cy should be 2, got %d", app.buf.cy)
	}

	// Escape clears selection.
	app.dispatch([]byte{0x1b})
	if app.buf.selStartLine != -1 {
		t.Error("escape: selStartLine should be -1 (no selection)")
	}

	// Enter linewise visual mode (reset position first).
	app.buf.cy = 1
	app.buf.cx = 2
	app.buf.selStartLine = -1
	app.dispatch([]byte{'V'})
	if app.buf.selEndLine != 1 {
		t.Errorf("V: selEndLine should start at cy=1, got %d", app.buf.selEndLine)
	}
	if !app.buf.selLinewise {
		t.Error("V: selLinewise should be true")
	}
	if app.buf.selStartCol != 0 {
		t.Errorf("V: selStartCol should be 0, got %d", app.buf.selStartCol)
	}

	// Move down in linewise — selection extends.
	app.dispatch([]byte{'j'})
	if app.buf.selEndLine != 2 {
		t.Errorf("V then j: selEndLine should be 2 (cy moved 1→2), got %d", app.buf.selEndLine)
	}
}

func makeLines(n int) []string {
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		lines[i] = "line"
	}
	return lines
}
