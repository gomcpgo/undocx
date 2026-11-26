package model

import (
	"strconv"
	"strings"
)

// MarkdownWriter joins blocks into one Markdown document. It nests list
// items, keeps the items of a list on consecutive lines, and keeps two lists
// that follow each other from running together.
//
// A list item whose label Markdown has syntax for is a list item. Any other
// label is written as text at the start of a paragraph, indented so that it
// stays inside the item above it: Markdown cannot show "4.1" as an item of
// "4.", but it can keep it in that item.
type MarkdownWriter struct {
	n int
	// open is the list items that later items may nest in, outermost
	// first.
	open []openItem
	// last is the last item written at each indentation, while its list
	// is still open.
	last []lastItem
	// item reports that the last block written was a list item that
	// Markdown sees as one.
	item bool
}

type openItem struct {
	level int // Block.Level
	child int // where content nested in the item starts
}

type lastItem struct {
	indent int
	bullet bool
	delim  byte // . or ) after a number
	number int
}

// Block returns the block as Markdown, preceded by what separates it from
// the block written before. It returns "" for a block with nothing to show.
func (w *MarkdownWriter) Block(b Block) string {
	md := b.Markdown()
	if md == "" {
		return ""
	}
	sep := ""
	if w.n > 0 {
		sep = "\n\n"
	}
	w.n++
	if b.Kind != ListItem {
		w.open, w.last, w.item = w.open[:0], w.last[:0], false
		return sep + md
	}

	for n := len(w.open); n > 0 && w.open[n-1].level >= b.Level; n = len(w.open) {
		w.open = w.open[:n-1]
	}
	indent := 0
	if n := len(w.open); n > 0 {
		indent = w.open[n-1].child
	}
	// Lists deeper than this one have ended.
	for n := len(w.last); n > 0 && w.last[n-1].indent > indent; n = len(w.last) {
		w.last = w.last[:n-1]
	}
	var prev *lastItem
	if n := len(w.last); n > 0 && w.last[n-1].indent == indent {
		prev = &w.last[n-1]
	}

	marker := markerWidth(b)
	pad := strings.Repeat(" ", indent)
	if marker == 0 {
		// A paragraph that starts with its label: it ends any list at
		// its own indentation, and nests nothing.
		if prev != nil {
			w.last = w.last[:len(w.last)-1]
		}
		w.open = append(w.open, openItem{b.Level, indent})
		w.item = false
		return sep + pad + indentLines(md, indent)
	}

	this := lastItem{indent: indent, bullet: b.LabelKind == LabelBullet}
	if !this.bullet {
		this.delim = b.Label[len(b.Label)-1]
		this.number, _ = strconv.Atoi(b.Label[:len(b.Label)-1])
	}
	sameList := prev != nil && prev.bullet == this.bullet && prev.delim == this.delim
	switch {
	case sameList && !this.bullet && this.number != prev.number+1:
		// Markdown numbers the items of a list from the first one, so a
		// list that starts again must be kept apart from the one before.
		sep = "\n\n" + pad + "<!-- -->\n\n"
	case w.item && (sameList || this.bullet || this.number == 1):
		// Items on consecutive lines. Only a bullet or a list that starts
		// at 1 may begin straight after a line of text; any other number
		// would be read as part of that text.
		sep = "\n"
	}
	if prev != nil {
		*prev = this
	} else {
		w.last = append(w.last, this)
	}
	w.open = append(w.open, openItem{b.Level, indent + marker})
	w.item = true
	return sep + pad + indentLines(md, indent+marker)
}

// markerWidth returns how many characters the list marker of an item and
// the space after it take, or 0 when the item is written as a paragraph.
func markerWidth(b Block) int {
	switch {
	case b.LabelKind == LabelBullet:
		return 2
	case b.numberMarker():
		return len(b.Label) + 1
	}
	return 0
}

// numberMarker reports whether the label is one Markdown reads as the
// marker of an ordered list item: one to nine digits and a full stop or a
// parenthesis. A longer number is written as text.
func (b Block) numberMarker() bool {
	n := len(b.Label)
	if b.Kind != ListItem || b.LabelKind != LabelNumber || n < 2 || n > 10 || (b.Label[n-1] != '.' && b.Label[n-1] != ')') {
		return false
	}
	for i := 0; i < n-1; i++ {
		if b.Label[i] < '0' || b.Label[i] > '9' {
			return false
		}
	}
	return true
}

// indentLines indents every line but the first.
func indentLines(s string, indent int) string {
	if indent == 0 || !strings.Contains(s, "\n") {
		return s
	}
	return strings.ReplaceAll(s, "\n", "\n"+strings.Repeat(" ", indent))
}
