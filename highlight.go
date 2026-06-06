// highlight.go — simple syntax highlighting for Go, Python, and TypeScript.
// Character-by-character scanning, no regex, no external dependencies.
// Returns (start, end, color) segments for each line.

package main

import "strings"

// Segment describes a highlighted span within a line.
// Start and end are byte offsets (not rune offsets).
// Color is an ANSI 256-color index (use setFg/ansiReset).
type Segment struct {
	Start int // byte offset from start of line
	End   int // byte offset, exclusive
	Color int // ANSI 256-color index
}

// Language constants for file extension detection.
const (
	langNone = iota
	langGo
	langPython
	langTypeScript
)

// Color assignments for syntax groups.
const (
	hlKeyword  = 3  // yellow
	hlString   = 2  // green
	hlComment  = 8  // dim/grey
	hlNumber   = 5  // magenta
	hlBuiltin  = 6  // cyan
	hlTypeName = 12 // blue
)

// detectLang guesses the language from a file extension.
func detectLang(path string) int {
	if strings.HasSuffix(path, ".go") {
		return langGo
	}
	if strings.HasSuffix(path, ".py") ||
		strings.HasSuffix(path, ".pyw") {
		return langPython
	}
	if strings.HasSuffix(path, ".ts") ||
		strings.HasSuffix(path, ".tsx") ||
		strings.HasSuffix(path, ".js") ||
		strings.HasSuffix(path, ".jsx") ||
		strings.HasSuffix(path, ".mjs") ||
		strings.HasSuffix(path, ".cjs") {
		return langTypeScript
	}
	return langNone
}

// highlightAll pre-computes segments for all lines in a file.
// Tracks block comment state across lines.
func highlightAll(lines []string, lang int) [][]Segment {
	result := make([][]Segment, len(lines))
	blockComment := false
	for i, line := range lines {
		var segs []Segment
		segs, blockComment = highlightLine(line, lang, blockComment)
		result[i] = segs
	}
	return result
}

// applyHighlight returns a copy of line with ANSI color codes inserted
// according to segments. If segments is nil/empty, returns line unchanged.
func applyHighlight(line string, segments []Segment) string {
	if len(segments) == 0 {
		return line
	}
	var buf []byte
	pos := 0
	for _, seg := range segments {
		if seg.Start > pos && seg.Start <= len(line) {
			buf = append(buf, line[pos:seg.Start]...)
		}
		if seg.End > seg.Start && seg.End <= len(line) {
			buf = append(buf, setFg(seg.Color)...)
			buf = append(buf, line[seg.Start:seg.End]...)
			buf = append(buf, ansiReset...)
			pos = seg.End
		}
	}
	if pos < len(line) {
		buf = append(buf, line[pos:]...)
	}
	return string(buf)
}

// highlightLine returns segments for a single line of source code.
// blockComment is true if we're inside a /* */ block from a previous line.
// Returns the segments and updated blockComment state.
func highlightLine(line string, lang int, blockComment bool) ([]Segment, bool) {
	if lang == langNone || len(line) == 0 {
		return nil, blockComment
	}

	var segs []Segment
	addSeg := func(start, end, color int) {
		if end > start {
			segs = append(segs, Segment{start, end, color})
		}
	}

	i := 0
	n := len(line)

	// If we enter this line already inside a block comment.
	if blockComment {
		// Scan for */ to end the block comment.
		end := strings.Index(line, "*/")
		if end == -1 {
			addSeg(0, n, hlComment)
			return segs, true // still inside block comment
		}
		addSeg(0, end+2, hlComment)
		i = end + 2
		blockComment = false
	}

	for i < n {
		c := line[i]

		// Block comment start (Go, TS) — /*...
		if lang != langPython && c == '/' && i+1 < n && line[i+1] == '*' {
			end := strings.Index(line[i+2:], "*/")
			if end == -1 {
				addSeg(i, n, hlComment)
				blockComment = true
				return segs, blockComment
			}
			addSeg(i, i+end+4, hlComment)
			i = i + end + 4
			continue
		}

		// Line comment (Go, TS) — //
		if lang != langPython && c == '/' && i+1 < n && line[i+1] == '/' {
			addSeg(i, n, hlComment)
			return segs, blockComment // rest of line is comment
		}

		// Line comment (Python) — #
		if lang == langPython && c == '#' {
			addSeg(i, n, hlComment)
			return segs, blockComment
		}

		// Triple-quoted string (Python) or backtick (Go, TS).
		if c == '"' && i+2 < n && line[i+1] == '"' && line[i+2] == '"' {
			// Find closing """
			end := strings.Index(line[i+3:], "\"\"\"")
			if end == -1 {
				addSeg(i, n, hlString)
				return segs, blockComment
			}
			addSeg(i, i+end+6, hlString)
			i = i + end + 6
			continue
		}

		// Backtick raw string (Go) — `...`
		if lang == langGo && c == '`' {
			end := strings.IndexByte(line[i+1:], '`')
			if end == -1 {
				addSeg(i, n, hlString)
				return segs, blockComment
			}
			addSeg(i, i+end+2, hlString)
			i = i + end + 2
			continue
		}

		// Double-quoted string — "..."
		if c == '"' {
			j := i + 1
			for j < n {
				if line[j] == '\\' {
					j += 2 // skip escaped char
					continue
				}
				if line[j] == '"' {
					break
				}
				j++
			}
			if j >= n {
				addSeg(i, n, hlString)
				return segs, blockComment
			}
			addSeg(i, j+1, hlString)
			i = j + 1
			continue
		}

		// Single-quoted string (Python, TS) or rune (Go).
		if c == '\'' && (lang == langPython || lang == langTypeScript || lang == langGo) {
			// Go single quotes are runes, not strings. Treat them as string-like.
			j := i + 1
			for j < n {
				if line[j] == '\\' {
					j += 2
					continue
				}
				if line[j] == '\'' {
					break
				}
				j++
			}
			if j >= n {
				addSeg(i, n, hlString)
				return segs, blockComment
			}
			addSeg(i, j+1, hlString)
			i = j + 1
			continue
		}

		// Number literal.
		if isDigit(c) {
			j := i + 1
			// Hex: 0x...
			if c == '0' && j < n && (line[j] == 'x' || line[j] == 'X') {
				j++
				for j < n && isHexDigit(line[j]) {
					j++
				}
			} else {
				for j < n && isDigit(line[j]) {
					j++
				}
				// Decimal point.
				if j < n && line[j] == '.' && j+1 < n && isDigit(line[j+1]) {
					j++
					for j < n && isDigit(line[j]) {
						j++
					}
				}
			}
			addSeg(i, j, hlNumber)
			i = j
			continue
		}

		// Identifier or keyword.
		if isIdentStart(c) {
			j := i + 1
			for j < n && isIdentPart(line[j]) {
				j++
			}
			word := line[i:j]

			// Check against keyword sets.
			color := 0 // default (no segment emitted — caller uses default color)
			if kw, ok := keywords[lang][word]; ok {
				color = kw
			}

			if color > 0 {
				addSeg(i, j, color)
			}
			i = j
			continue
		}

		// Other character (operators, whitespace, punctuation) — no highlighting.
		i++
	}

	return segs, blockComment
}

// --- Keyword tables ---

var keywords = map[int]map[string]int{
	langGo:         goKeywords,
	langPython:     pyKeywords,
	langTypeScript: tsKeywords,
}

var goKeywords = map[string]int{
	"break": hlKeyword, "case": hlKeyword, "chan": hlBuiltin,
	"const": hlKeyword, "continue": hlKeyword, "default": hlKeyword,
	"defer": hlKeyword, "else": hlKeyword, "fallthrough": hlKeyword,
	"for": hlKeyword, "func": hlKeyword, "go": hlKeyword,
	"goto": hlKeyword, "if": hlKeyword, "import": hlKeyword,
	"interface": hlKeyword, "map": hlBuiltin, "package": hlKeyword,
	"range": hlKeyword, "return": hlKeyword, "select": hlKeyword,
	"struct": hlKeyword, "switch": hlKeyword, "type": hlKeyword,
	"var": hlKeyword,
	// Built-in functions (cyan)
	"append": hlBuiltin, "cap": hlBuiltin, "close": hlBuiltin,
	"complex": hlBuiltin, "copy": hlBuiltin, "delete": hlBuiltin,
	"error": hlBuiltin, "false": hlBuiltin, "imag": hlBuiltin,
	"len": hlBuiltin, "make": hlBuiltin, "new": hlBuiltin,
	"nil": hlBuiltin, "panic": hlBuiltin, "print": hlBuiltin,
	"println": hlBuiltin, "real": hlBuiltin, "recover": hlBuiltin,
	"true": hlBuiltin,
	// Common types (blue)
	"string": hlTypeName, "int": hlTypeName, "int8": hlTypeName,
	"int16": hlTypeName, "int32": hlTypeName, "int64": hlTypeName,
	"uint": hlTypeName, "uint8": hlTypeName, "uint16": hlTypeName,
	"uint32": hlTypeName, "uint64": hlTypeName, "float32": hlTypeName,
	"float64": hlTypeName, "bool": hlTypeName, "byte": hlTypeName,
	"rune": hlTypeName, "any": hlTypeName, "comparable": hlTypeName,
}

var pyKeywords = map[string]int{
	"False": hlBuiltin, "None": hlBuiltin, "True": hlBuiltin,
	"and": hlKeyword, "as": hlKeyword, "assert": hlKeyword,
	"async": hlKeyword, "await": hlKeyword, "break": hlKeyword,
	"class": hlKeyword, "continue": hlKeyword, "def": hlKeyword,
	"del": hlKeyword, "elif": hlKeyword, "else": hlKeyword,
	"except": hlKeyword, "finally": hlKeyword, "for": hlKeyword,
	"from": hlKeyword, "global": hlKeyword, "if": hlKeyword,
	"import": hlKeyword, "in": hlKeyword, "is": hlBuiltin,
	"lambda": hlKeyword, "nonlocal": hlKeyword, "not": hlKeyword,
	"or": hlKeyword, "pass": hlKeyword, "raise": hlKeyword,
	"return": hlKeyword, "try": hlKeyword, "while": hlKeyword,
	"with": hlKeyword, "yield": hlKeyword,
	// Built-ins
	"print": hlBuiltin, "range": hlBuiltin, "len": hlBuiltin,
	"int": hlBuiltin, "str": hlBuiltin, "float": hlBuiltin,
	"list": hlBuiltin, "dict": hlBuiltin, "tuple": hlBuiltin,
	"set": hlBuiltin, "bool": hlBuiltin, "type": hlBuiltin,
	"super": hlBuiltin, "self": hlBuiltin, "cls": hlBuiltin,
}

var tsKeywords = map[string]int{
	"abstract": hlKeyword, "async": hlKeyword, "await": hlKeyword,
	"break": hlKeyword, "case": hlKeyword, "catch": hlKeyword,
	"class": hlKeyword, "const": hlKeyword, "continue": hlKeyword,
	"debugger": hlKeyword, "default": hlKeyword, "delete": hlKeyword,
	"do": hlKeyword, "else": hlKeyword, "enum": hlKeyword,
	"export": hlKeyword, "extends": hlKeyword, "finally": hlKeyword,
	"for": hlKeyword, "from": hlKeyword, "function": hlKeyword,
	"if": hlKeyword, "implements": hlKeyword, "import": hlKeyword,
	"in": hlKeyword, "instanceof": hlKeyword, "interface": hlKeyword,
	"let": hlKeyword, "new": hlKeyword, "of": hlKeyword,
	"private": hlKeyword, "protected": hlKeyword, "public": hlKeyword,
	"return": hlKeyword, "static": hlKeyword, "super": hlKeyword,
	"switch": hlKeyword, "this": hlBuiltin, "throw": hlKeyword,
	"try": hlKeyword, "type": hlKeyword, "typeof": hlKeyword,
	"var": hlKeyword, "void": hlKeyword, "while": hlKeyword,
	"yield": hlKeyword,
	// Built-ins / globals
	"console": hlBuiltin, "undefined": hlBuiltin, "null": hlBuiltin,
	"true": hlBuiltin, "false": hlBuiltin, "Error": hlBuiltin,
	"Promise": hlBuiltin, "Array": hlBuiltin, "Object": hlBuiltin,
	"Map": hlBuiltin, "Set": hlBuiltin, "string": hlTypeName,
	"number": hlTypeName, "boolean": hlTypeName,
}

// --- Character predicates ---

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}
func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}
