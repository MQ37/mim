package app

import "testing"

func TestGgStateMachine(t *testing.T) {
	app := &App{
		Buf: &Buf{
			lines:        makeLines(100),
			cy:           50,
			cx:           0,
			scr:          40,
			selStartLine: -1,
		},
		TermW: 80,
		TermH: 24,
		Focus: ViewerFocus,
	}

	// First 'g' — sets pendingG, no movement.
	app.Dispatch([]byte{'g'})
	if !viewerPendingG {
		t.Error("after first 'g', viewerPendingG should be true")
	}
	if app.Buf.cy != 50 {
		t.Errorf("after first 'g', cy should still be 50, got %d", app.Buf.cy)
	}

	// Second 'g' — triggers gg, jumps to top.
	app.Dispatch([]byte{'g'})
	if viewerPendingG {
		t.Error("after gg, viewerPendingG should be false")
	}
	if app.Buf.cy != 0 {
		t.Errorf("after gg, cy should be 0, got %d", app.Buf.cy)
	}
	if app.Buf.scr != 0 {
		t.Errorf("after gg, scr should be 0, got %d", app.Buf.scr)
	}

	// 'g' followed by non-'g' — pendingG cleared, normal key processed.
	app.Buf.cy = 10
	app.Dispatch([]byte{'g'})
	if !viewerPendingG {
		t.Error("after 'g', viewerPendingG should be true")
	}
	app.Dispatch([]byte{'j'}) // move down
	if viewerPendingG {
		t.Error("after 'j', viewerPendingG should be false")
	}
	if app.Buf.cy != 11 {
		t.Errorf("after 'g' then 'j', cy should be 11, got %d", app.Buf.cy)
	}

	// 'g' followed by escape — pendingG cleared by escape.
	app.Dispatch([]byte{'g'})
	app.Dispatch([]byte{0x1b})
	if viewerPendingG {
		t.Error("after escape following 'g', viewerPendingG should be false")
	}

	// Multi-byte sequence after 'g' — pendingG cleared.
	app.Dispatch([]byte{'g'})
	app.Dispatch([]byte{0x1b, '[', 'A'}) // arrow up
	if viewerPendingG {
		t.Error("after multi-byte key following 'g', viewerPendingG should be false")
	}
}

func TestVisualModeTransitions(t *testing.T) {
	app := &App{
		Buf: &Buf{
			lines:        []string{"hello", "world", "foo", "bar"},
			cy:           1,
			cx:           2,
			scr:          0,
			selStartLine: -1,
		},
		TermW: 80,
		TermH: 24,
		Focus: ViewerFocus,
	}

	// Enter charwise visual mode.
	app.Dispatch([]byte{'v'})
	if app.Buf.selStartLine != 1 {
		t.Errorf("v: selStartLine should be 1, got %d", app.Buf.selStartLine)
	}
	if app.Buf.selStartCol != 2 {
		t.Errorf("v: selStartCol should be 2, got %d", app.Buf.selStartCol)
	}
	if app.Buf.selLinewise {
		t.Error("v: selLinewise should be false")
	}

	// Move down in visual mode — selection extends.
	app.Dispatch([]byte{'j'})
	if app.Buf.selEndLine != 2 {
		t.Errorf("v then j: selEndLine should be 2, got %d", app.Buf.selEndLine)
	}
	if app.Buf.cy != 2 {
		t.Errorf("v then j: cy should be 2, got %d", app.Buf.cy)
	}

	// Escape clears selection.
	app.Dispatch([]byte{0x1b})
	if app.Buf.selStartLine != -1 {
		t.Error("escape: selStartLine should be -1 (no selection)")
	}

	// Enter linewise visual mode (reset position first).
	app.Buf.cy = 1
	app.Buf.cx = 2
	app.Buf.selStartLine = -1
	app.Dispatch([]byte{'V'})
	if app.Buf.selEndLine != 1 {
		t.Errorf("V: selEndLine should start at cy=1, got %d", app.Buf.selEndLine)
	}
	if !app.Buf.selLinewise {
		t.Error("V: selLinewise should be true")
	}
	if app.Buf.selStartCol != 0 {
		t.Errorf("V: selStartCol should be 0, got %d", app.Buf.selStartCol)
	}

	// Move down in linewise — selection extends.
	app.Dispatch([]byte{'j'})
	if app.Buf.selEndLine != 2 {
		t.Errorf("V then j: selEndLine should be 2 (cy moved 1→2), got %d", app.Buf.selEndLine)
	}
}

func makeLines(n int) []string {
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		lines[i] = "line"
	}
	return lines
}
