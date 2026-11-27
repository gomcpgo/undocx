// Package model is the document as the reader hands it on: a sequence of
// blocks, each a sequence of inline runs. The reader fills it from the file
// and the writers turn it into Markdown or plain text; neither knows about
// the other. The public package exports these types under the same names.
package model

import "strings"

// BlockKind says what part a block plays in the document.
type BlockKind int

const (
	// Paragraph is body text, and anything not recognised as something
	// else.
	Paragraph BlockKind = iota
	// Heading is a section heading. Block.Level is its rank, from 1.
	Heading
	// Title is the title of the document: a paragraph in the Title style.
	Title
	// ListItem is a paragraph of a numbered or bulleted list. Block.Level
	// is its level in the list, from 1, and Block.Label its number or
	// bullet.
	ListItem
	// Table is a table. Block.Table holds its rows and cells, and
	// Block.Inlines is empty.
	Table
)

func (k BlockKind) String() string {
	switch k {
	case Heading:
		return "heading"
	case Title:
		return "title"
	case ListItem:
		return "list item"
	case Table:
		return "table"
	}
	return "paragraph"
}

// Block is one paragraph-level piece of the document: a paragraph of some
// kind, or a table.
type Block struct {
	Kind BlockKind
	// Level is the rank of a heading, from 1 for the highest to 9. It is
	// the outline level Word gives the paragraph, so it does not depend on
	// what the heading styles are called. For a list item it is the level
	// in the list, from 1 for the outermost to 9.
	Level int
	// Label is the number or bullet that Word shows in front of the
	// paragraph, worked out the way Word counts: "4.2", "(b)", "iii.",
	// "Article VII", "•". List items have one, and so do headings numbered
	// through their style. It is empty for a paragraph that is not
	// numbered, and for a list level that shows no label.
	//
	// Text and Markdown write the label in front of the content, and a
	// heading path includes it. Inlines does not hold it.
	Label string
	// LabelKind says how Markdown writes the label of a list item.
	LabelKind LabelKind
	// Style is the name of the paragraph style, as Word shows it: "Normal",
	// "heading 1", "Quote". It is empty when the paragraph names no style
	// or the style is not defined.
	Style string
	// Path is the headings the block stands under, outermost first: for a
	// paragraph in section 3.2, the text of heading 3 and of heading 3.2.
	// For a heading it is the headings above it. The slice is shared
	// between blocks and must not be changed. A block inside a table cell
	// has no path of its own; it stands where its table stands.
	Path []string
	// Inlines is the content of the block, in reading order. A table has
	// none.
	Inlines []Inline
	// Table is the rows of a Table block, and nil for any other kind.
	Table *TableData
}

// TableData is the content of a Table block: its rows, each cell placed on
// the grid of columns that the table's cells line up on.
type TableData struct {
	// Columns is the number of columns of the grid.
	Columns int
	// Rows is the rows from top to bottom. A row whose cells are all
	// covered by cells merged down from above has no cells.
	Rows []Row
	// Continued reports that this block carries on the table of the block
	// before it. A table of several thousand rows is handed on in parts,
	// so that it need not be held in memory whole. Each part starts with
	// the table's header rows again, as each page of the table does in
	// Word.
	Continued bool
}

// Row is one row of a table.
type Row struct {
	// Header reports that the row is a header row: one of the rows at the
	// top of the table that Word repeats on each page.
	Header bool
	// Cells is the cells that begin in this row, from left to right. A
	// cell merged down from a row above is not repeated.
	Cells []Cell
}

// Cell is one cell of a table.
type Cell struct {
	// Col is the grid column the cell starts in, from 0. Columns that no
	// cell of a row covers are empty space: the row is indented, or
	// shorter than the table is wide.
	Col int
	// ColSpan and RowSpan are the columns and rows the cell covers, 1 for
	// a cell that is not merged.
	ColSpan, RowSpan int
	// Blocks is the content of the cell: paragraphs, list items, and
	// tables nested in it. A heading in a cell keeps its kind, but does
	// not open a section of the document. A numbered paragraph with no
	// text is kept, since its number is often all a cell holds.
	Blocks []Block
}

// LabelKind says how Markdown writes the label of a list item.
type LabelKind int

const (
	// LabelText is a label Markdown has no list syntax for, such as "4.2",
	// "(b)" or "Article VII". It is written as text at the start of the
	// paragraph.
	LabelText LabelKind = iota
	// LabelBullet is a bullet. Markdown writes the item with "-", whatever
	// the glyph.
	LabelBullet
	// LabelNumber is a plain number followed by a full stop or a
	// parenthesis, "7." or "7)", which Markdown writes as an item of an
	// ordered list.
	LabelNumber
)

// InlineKind says what an Inline is.
type InlineKind int

const (
	// Text is a run of text with one set of formatting.
	Text InlineKind = iota
	// Break is a line break inside a paragraph.
	Break
)

// Inline is a piece of a block's content.
type Inline struct {
	Kind InlineKind
	// Text is the text of a Text inline. Adjacent runs with the same
	// formatting are joined, so the way Word happened to split a sentence
	// into runs does not show.
	Text string

	Bold   bool
	Italic bool
	Strike bool
	// Code reports that the run is set in a monospace font or a style
	// meant for code.
	Code        bool
	Superscript bool
	Subscript   bool

	// Link is the address the run links to, or empty. Only web, mail and
	// telephone addresses are kept; a link of any other kind is reported
	// as a warning and its text left plain.
	Link string
}

// sameFormat reports whether two inlines can be joined into one.
func (a Inline) sameFormat(b Inline) bool {
	a.Text, b.Text = "", ""
	return a == b
}

// Append adds an inline to a list, joining it to the last one when the two
// are text with the same formatting.
func Append(list []Inline, in Inline) []Inline {
	if in.Kind == Text {
		if in.Text == "" {
			return list
		}
		if n := len(list); n > 0 && list[n-1].Kind == Text && list[n-1].sameFormat(in) {
			list[n-1].Text += in.Text
			return list
		}
	}
	return append(list, in)
}

// PlainText returns the text of the inlines without formatting, with a
// newline for each line break.
func PlainText(inlines []Inline) string {
	var sb strings.Builder
	for _, in := range inlines {
		switch in.Kind {
		case Text:
			sb.WriteString(in.Text)
		case Break:
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// Empty reports whether the inlines hold nothing but white space.
func Empty(inlines []Inline) bool {
	for _, in := range inlines {
		if in.Kind == Text && strings.TrimSpace(in.Text) != "" {
			return false
		}
	}
	return true
}
