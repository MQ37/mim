// scroll.go — shared scroll-boundary math.

package main

// clampScroll adjusts *scr so that cursor (0-indexed) is visible within
// a viewport of height rows, given maxItems total items. Returns the
// new scroll value (also updates *scr in place).
func clampScroll(cursor int, scr *int, height, maxItems int) {
	if height < 1 {
		height = 1
	}
	if cursor < *scr {
		*scr = cursor
	}
	if cursor >= *scr+height {
		*scr = cursor - height + 1
	}
	if *scr < 0 {
		*scr = 0
	}
	maxScr := maxItems - height
	if maxScr < 0 {
		maxScr = 0
	}
	if *scr > maxScr {
		*scr = maxScr
	}
}
