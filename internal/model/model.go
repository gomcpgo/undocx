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
)

func (k BlockKind) String() string {
	switch k {
	case Heading:
		return "heading"
	case Title:
		return "title"
	}
	return "paragraph"
}

// Block is one paragraph-level piece of the document.
type Block struct {
	Kind BlockKind
	// Level is the rank of a heading, from 1 for the highest to 9. It is
	// the outline level Word gives the paragraph, so it does not depend on
	// what the heading styles are called.
	Level int
	// Style is the name of the paragraph style, as Word shows it: "Normal",
	// "heading 1", "Quote". It is empty when the paragraph names no style
	// or the style is not defined.
	Style string
	// Path is the headings the block stands under, outermost first: for a
	// paragraph in section 3.2, the text of heading 3 and of heading 3.2.
	// For a heading it is the headings above it. The slice is shared
	// between blocks and must not be changed.
	Path []string
	// Inlines is the content of the block, in reading order.
	Inlines []Inline
}

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
