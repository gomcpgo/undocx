// Package symbols turns characters of the symbol fonts into Unicode.
//
// Text set in Symbol or Wingdings is stored as the position of a glyph in the
// font, not as the character it shows: "a" in Symbol is alpha, and the arrow
// Word makes of "-->" is position E0 of Wingdings. Word often writes such a
// position in the private-use range, as F000 plus the position.
package symbols

import (
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

// The Symbol table is the Unicode Consortium's mapping of the Adobe Symbol
// encoding, used as distributed. See NOTICE.
//
//go:embed data/symbol.txt
var symbolData string

var (
	symbolOnce  sync.Once
	symbolTable [256]rune
)

func loadSymbol() {
	for _, line := range strings.Split(symbolData, "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 2 {
			continue
		}
		u, err1 := strconv.ParseUint(f[0], 16, 32)
		c, err2 := strconv.ParseUint(f[1], 16, 8)
		// A position can have two characters; the first listed is the
		// usual one. Entries in the private-use area are pieces of large
		// brackets, which have no character of their own.
		if err1 != nil || err2 != nil || symbolTable[c] != 0 || u >= 0xE000 && u <= 0xF8FF {
			continue
		}
		symbolTable[c] = rune(u)
	}
}

// wingdings maps the glyphs of Wingdings that documents use most: bullets,
// arrows, check marks and a few signs. It is a best effort written for this
// package, not a complete table; the rest of the font is reported as unmapped.
var wingdings = map[byte]rune{
	0x28: '☎', 0x2A: '✉', 0x45: '☜', 0x46: '☞', 0x4A: '☺', 0x4C: '☹',
	0x6C: '●', 0x6E: '■', 0x6F: '□', 0x71: '❑', 0x72: '❒', 0x75: '◆', 0x76: '❖',
	0x9F: '•', 0xA1: '○', 0xA7: '▪', 0xA8: '◻',
	0xD8: '➢', 0xDF: '←', 0xE0: '→', 0xE1: '↑', 0xE2: '↓', 0xE8: '➔', 0xEF: '⇦', 0xF0: '⇨',
	0xFB: '✗', 0xFC: '✓', 0xFD: '☒', 0xFE: '☑',
}

// Font reports whether a font is a symbol font: one whose characters are
// glyph positions.
func Font(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "symbol", "wingdings", "wingdings 2", "wingdings 3", "webdings", "zapfdingbats", "zapf dingbats", "mt extra", "marlett":
		return true
	}
	return false
}

// Position returns the glyph position that a character stands for in a
// symbol font: the character itself below 256, or its low byte in the
// private-use range F000 to F0FF.
func Position(r rune) (byte, bool) {
	switch {
	case r >= 0x20 && r <= 0xFF:
		return byte(r), true
	case r >= 0xF020 && r <= 0xF0FF:
		return byte(r - 0xF000), true
	}
	return 0, false
}

// Map returns the Unicode character for a glyph position of a symbol font.
// It reports false when the font or the position is not in the tables.
func Map(font string, pos byte) (rune, bool) {
	var r rune
	switch strings.ToLower(strings.TrimSpace(font)) {
	case "symbol":
		symbolOnce.Do(loadSymbol)
		r = symbolTable[pos]
	case "wingdings":
		r = wingdings[pos]
		if pos == 0x20 {
			r = ' '
		}
	default:
		if pos == 0x20 {
			r = ' '
		}
	}
	return r, r != 0
}
