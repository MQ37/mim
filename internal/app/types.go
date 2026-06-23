// Types and constants shared across all files.

package app

// Focus — which UI pane or overlay owns keyboard input.
type Focus int

const (
	TreeFocus        Focus = iota // navigating file tree
	ViewerFocus                   // navigating code viewer
	FindInputFocus                // typing search query in popup
	FindResultsFocus              // browsing search results list
)

// Buf holds a file's content in memory and cursor state.
type Buf struct {
	path  string   // absolute file path (empty if no file)
	lines []string // file content split by '\n', no trailing newline in elements
	cx    int      // cursor column, byte offset into lines[cy]
	cy    int      // cursor line, 0-indexed
	scr   int      // first visible line (scroll offset)

	// Visual selection (v / V mode). selStartLine == -1 means no selection.
	selStartLine int
	selStartCol  int
	selEndLine   int
	selEndCol    int
	selLinewise  bool // true = V (linewise), false = v (charwise)

	// Syntax highlighting (populated on file open).
	hlLang     int         // language constant from highlight.go
	hlSegments [][]Segment // one []Segment per line, nil if no highlighting
}

// Node is one entry in the file tree.
type Node struct {
	name  string  // base name, e.g. "main.go"
	path  string  // full path from tree root
	isDir bool
	kids  []*Node // nil = not loaded yet; []*Node{} = loaded, empty
	open  bool    // expanded state for directories (meaningless for files)
}

// Tree holds the file tree state, including a flat cache for O(1) rendering.
type Tree struct {
	root     *Node   // never changes after init
	flat     []*Node // cached flattened view of expanded nodes
	cursor   int     // index into flat (selected node)
	scr      int     // first visible index in flat
	RootPath string  // absolute path passed to newTree
	showAll  bool    // toggle: include gitignored files when true
}

// Hit is one search result from grep.
type Hit struct {
	path string // file path relative to search root
	line int    // 1-indexed line number
	text string // matching line content
}

// Commit represents one entry in the git log.
type Commit struct {
	hash      string // full 40-char SHA
	subject   string // first line of commit message
}

// GitState holds all state for the git diff view mode.
// When non-nil on App, the UI switches to git mode.
type GitState struct {
	// Commit list (left pane, replaces Tree)
	commits   []Commit
	commitCur int      // cursor index into commits
	commitScr int      // first visible commit row
	selAnchor int      // anchor where v was pressed; -1 = no selection

	// Computed range (always selStart <= selEnd after normalization).
	// When selAnchor == -1, these are both -1.
	selStart int
	selEnd   int

	// Diff output (right pane, replaces Buf)
	diffLines    []string // raw lines from git diff --color=always
	diffCursor   int      // cursor line in diff (0-indexed)
	diffScr      int      // first visible diff line
	diffSelStart int      // visual selection start line, -1 = no selection
	diffSelEnd   int      // visual selection end line (inclusive)

	loadingDiff bool
}

// App is the global application state.
type App struct {
	// File tree (left pane)
	Tree Tree

	// Code viewer (center/right pane, nil when no file open)
	Buf *Buf

	// Tree visibility (Ctrl+E toggles)
	TreeVisible bool

	// Which pane/overlay owns input
	Focus Focus

	// Find / search state (overlay)
	findQuery      []rune // text typed by user
	findCursor     int    // cursor position within findQuery
	findHits       []Hit  // grep results
	findCur        int    // selected hit index, -1 if none
	findScr        int    // scroll offset in results list
	findRunning    bool   // grep subprocess still running
	findPrevBuf    *Buf   // Buf active before find was started (restored on exit)
	findOpenedFile bool   // true when the viewer file was opened from find results

	// Terminal geometry (updated on SIGWINCH)
	TermW int // total columns
	TermH int // total rows
	TreeW int // left pane width (computed as termW * 30 / 100, clamped)

	// Status line
	StatusMsg string // temporary message shown instead of default status

	// Git diff view state. When non-nil, the UI switches to git mode.
	Git *GitState

	// Lifecycle
	Quit bool
}

