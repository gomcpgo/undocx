package model

import (
	"strconv"
	"strings"
)

// padColumns is the widest table whose rows are filled out with empty cells
// to the full width. A wider one, which Word cannot make, keeps its rows as
// they are, so that one very long row cannot make every other row long.
const padColumns = 64

// width returns the number of grid columns: the one the table states, or
// more if a cell reaches further.
func (t *TableData) width() int {
	n := t.Columns
	for _, row := range t.Rows {
		if k := len(row.Cells); k > 0 {
			c := row.Cells[k-1]
			n = max(n, c.Col+max(c.ColSpan, 1))
		}
	}
	return n
}

// text writes the table as plain text: a line for each row, and a tab
// between cells. An empty cell at the end of a row leaves no tab behind, and
// a row of empty cells no line.
func (t *TableData) text(sb *strings.Builder) {
	if t == nil {
		return
	}
	first := true
	line := func() {
		if !first {
			sb.WriteByte('\n')
		}
		first = false
	}
	for _, row := range t.Rows {
		tabs, any := 0, false
		for _, c := range row.Cells {
			words := false
			for _, b := range c.Blocks {
				if b.Kind == Table {
					continue
				}
				s := oneLine(b.Text())
				if s == "" {
					continue
				}
				switch {
				case words:
					sb.WriteByte(' ')
				case !any:
					line()
					fallthrough
				default:
					// One tab for each column passed, so that the cell is
					// in its own column whatever is merged above or beside
					// it.
					if any && tabs >= c.Col {
						sb.WriteByte('\t')
						tabs++
					}
					for ; tabs < c.Col; tabs++ {
						sb.WriteByte('\t')
					}
				}
				sb.WriteString(s)
				words, any = true, true
			}
		}
		for _, c := range row.Cells {
			for _, b := range c.Blocks {
				if b.Kind == Table && b.Table != nil && b.Table.hasText() {
					line()
					b.Table.text(sb)
				}
			}
		}
	}
}

// hasText reports whether the table has anything to show as text.
func (t *TableData) hasText() bool {
	for _, row := range t.Rows {
		for _, c := range row.Cells {
			for _, b := range c.Blocks {
				if b.Kind != Table && oneLine(b.Text()) != "" || b.Kind == Table && b.Table != nil && b.Table.hasText() {
					return true
				}
			}
		}
	}
	return false
}

// oneLine puts text on one line with no tab in it, since in the text of a
// table those separate rows and cells.
func oneLine(s string) string {
	s = strings.TrimSpace(s)
	if !strings.ContainsAny(s, "\n\t") {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return ' '
		}
		return r
	}, s)
}

// pipe reports whether the table can be written as a Markdown pipe table:
// no cell is merged, no cell holds a table, and no row but the first is a
// header row.
func (t *TableData) pipe() bool {
	for i, row := range t.Rows {
		if row.Header && i > 0 {
			return false
		}
		for _, c := range row.Cells {
			if c.ColSpan > 1 || c.RowSpan > 1 {
				return false
			}
			for _, b := range c.Blocks {
				if b.Kind == Table {
					return false
				}
			}
		}
	}
	return true
}

func (t *TableData) markdown(sb *strings.Builder) {
	if t == nil || len(t.Rows) == 0 {
		return
	}
	if t.pipe() {
		t.pipeTable(sb)
	} else {
		t.html(sb)
	}
}

// pipeTable writes the table in the pipe syntax. Its first row is the
// header, as the syntax needs one. A later part of a long table that has no
// header rows gets an empty header, since its first row is a row of data.
func (t *TableData) pipeTable(sb *strings.Builder) {
	cols := max(t.width(), 1)
	pad := cols
	if cols > padColumns {
		pad = 0
	}
	rows := t.Rows
	if t.Continued && !rows[0].Header {
		pipeRow(sb, Row{}, cols)
	} else {
		pipeRow(sb, rows[0], cols)
		rows = rows[1:]
	}
	sb.WriteString("\n|")
	for range cols {
		sb.WriteString(" --- |")
	}
	for _, row := range rows {
		sb.WriteByte('\n')
		pipeRow(sb, row, pad)
	}
}

// pipeRow writes one row, filled out with empty cells to at least cols
// columns.
func pipeRow(sb *strings.Builder, row Row, cols int) {
	sb.WriteByte('|')
	col := 0
	for _, c := range row.Cells {
		for ; col < c.Col; col++ {
			sb.WriteString("  |")
		}
		sb.WriteByte(' ')
		pipeCell(sb, c)
		sb.WriteString(" |")
		col++
	}
	for ; col < cols; col++ {
		sb.WriteString("  |")
	}
}

// pipeCell writes the content of a cell on one line: its paragraphs joined
// with <br>, and every | escaped, which the syntax asks for even inside a
// code span.
func pipeCell(sb *strings.Builder, c Cell) {
	n := 0
	for _, b := range c.Blocks {
		s := cellMarkdown(b)
		if s == "" {
			continue
		}
		if n++; n > 1 {
			sb.WriteString("<br>")
		}
		if strings.ContainsAny(s, "|\n") {
			s = strings.NewReplacer("|", `\|`, "\n", " ").Replace(s)
		}
		sb.WriteString(s)
	}
}

// cellMarkdown writes a block of a cell as inline Markdown. A cell cannot
// hold a heading or a list, so a heading is bold text and a list item starts
// with its label.
func cellMarkdown(b Block) string {
	heading := b.Kind == Heading || b.Kind == Title
	s := render(b.Inlines, heading, false, true)
	if heading && s != "" {
		s = "**" + s + "**"
	}
	if b.Label == "" {
		return s
	}
	return strings.TrimSpace(escape(b.Label, false) + " " + s)
}

// html writes the table as HTML, which Markdown passes through. Nothing in
// it is Markdown: between the tags of an HTML block a Markdown reader takes
// the text as it stands, so formatting and links are tags too. There is no
// empty line in it, because that would end the block.
func (t *TableData) html(sb *strings.Builder) {
	sb.WriteString("<table>")
	// covered[col] is the row down to which a cell merged from above
	// covers the column.
	var covered []int
	for r, row := range t.Rows {
		sb.WriteString("\n<tr>")
		tag := "td"
		if row.Header {
			tag = "th"
		}
		col := 0
		for _, c := range row.Cells {
			// Columns that the row leaves empty need a cell of their own,
			// or the cells after them would move left.
			gap := 0
			for ; col < c.Col; col++ {
				if col < len(covered) && covered[col] >= r {
					gap = emptyCell(sb, tag, gap)
				} else {
					gap++
				}
			}
			emptyCell(sb, tag, gap)
			sb.WriteString("<" + tag)
			span := max(c.ColSpan, 1)
			if c.RowSpan > 1 {
				sb.WriteString(` rowspan="` + strconv.Itoa(c.RowSpan) + `"`)
				for len(covered) < c.Col+span {
					covered = append(covered, -1)
				}
				for k := c.Col; k < c.Col+span; k++ {
					covered[k] = r + c.RowSpan - 1
				}
			}
			if span > 1 {
				sb.WriteString(` colspan="` + strconv.Itoa(span) + `"`)
			}
			sb.WriteByte('>')
			htmlCell(sb, c)
			sb.WriteString("</" + tag + ">")
			col = max(col, c.Col+span)
		}
		sb.WriteString("</tr>")
	}
	sb.WriteString("\n</table>")
}

// emptyCell writes an empty cell that covers gap columns, if there are any,
// and returns 0.
func emptyCell(sb *strings.Builder, tag string, gap int) int {
	switch {
	case gap == 1:
		sb.WriteString("<" + tag + "></" + tag + ">")
	case gap > 1:
		sb.WriteString("<" + tag + ` colspan="` + strconv.Itoa(gap) + `"></` + tag + ">")
	}
	return 0
}

func htmlCell(sb *strings.Builder, c Cell) {
	wasText := false
	for _, b := range c.Blocks {
		if b.Kind == Table {
			if b.Table != nil && len(b.Table.Rows) > 0 {
				sb.WriteByte('\n')
				b.Table.html(sb)
				sb.WriteByte('\n')
				wasText = false
			}
			continue
		}
		if len(b.Inlines) == 0 && b.Label == "" {
			continue
		}
		if wasText {
			sb.WriteString("<br>")
		}
		wasText = true
		heading := b.Kind == Heading || b.Kind == Title
		if heading {
			sb.WriteString("<strong>")
		}
		if b.Label != "" {
			sb.WriteString(htmlText.Replace(b.Label))
			if len(b.Inlines) > 0 {
				sb.WriteByte(' ')
			}
		}
		htmlInlines(sb, b.Inlines, heading)
		if heading {
			sb.WriteString("</strong>")
		}
	}
}

var (
	// A line end in the text becomes a space: an empty line would end the
	// HTML block.
	htmlText = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\n", " ", "\r", " ")
	htmlAttr = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "\n", "", "\r", "")
)

var htmlTags = [...]struct {
	m    marks
	name string
}{
	{bold, "strong"},
	{italic, "em"},
	{strike, "del"},
	{super, "sup"},
	{sub, "sub"},
	{code, "code"},
}

// htmlInlines writes inline content as HTML. heading says that the content
// is bold as a whole, so bold is not marked in it.
func htmlInlines(sb *strings.Builder, in []Inline, heading bool) {
	segs := segments(in, heading, true)
	link := ""
	for _, s := range segs {
		if s.link != link || s.isBrk {
			if link != "" {
				sb.WriteString("</a>")
			}
			link = ""
		}
		if s.isBrk {
			sb.WriteString("<br>")
			continue
		}
		if s.link != link {
			link = s.link
			sb.WriteString(`<a href="` + htmlAttr.Replace(link) + `">`)
		}
		for _, t := range htmlTags {
			if s.m&t.m != 0 {
				sb.WriteString("<" + t.name + ">")
			}
		}
		sb.WriteString(htmlText.Replace(s.text))
		for i := len(htmlTags) - 1; i >= 0; i-- {
			if s.m&htmlTags[i].m != 0 {
				sb.WriteString("</" + htmlTags[i].name + ">")
			}
		}
	}
	if link != "" {
		sb.WriteString("</a>")
	}
}
