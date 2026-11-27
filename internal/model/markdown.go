package model

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text returns the text of the block without formatting, with its label in
// front. A line break inside the block is a newline.
//
// A table is one line for each row, with a tab between cells, so that every
// cell is in its column: a cell merged across columns is followed by as many
// tabs as it covers columns. The paragraphs of a cell are joined with a
// space. A table inside a cell follows the row it stands in.
func (b Block) Text() string {
	if b.Kind == Table {
		var sb strings.Builder
		b.Table.text(&sb)
		return sb.String()
	}
	if b.Label != "" {
		return b.Label + " " + PlainText(b.Inlines)
	}
	return PlainText(b.Inlines)
}

// Markdown returns the block as Markdown, without a newline at its end.
//
// A heading is written with as many # as its level, up to six, and the
// title of the document with one. Bold, italic and struck-out text use
// Markdown marks; superscript and subscript use the HTML tags sup and sub;
// text in a monospace font is a code span.
//
// A list item is written as a bullet or numbered item when Markdown has
// the syntax for its label (see LabelKind), and otherwise as a paragraph
// that starts with its label. A block on its own does not know its place
// in a list, so it is not indented; MarkdownWriter nests items.
//
// A table is a pipe table, with its header row first. Markdown has no way to
// write a merged cell, a table within a cell or a second header row, so a
// table with any of these is written as an HTML table instead.
func (b Block) Markdown() string {
	switch b.Kind {
	case Table:
		var sb strings.Builder
		b.Table.markdown(&sb)
		return sb.String()
	case Heading:
		return strings.Repeat("#", min(max(b.Level, 1), 6)) + " " + b.headingText()
	case Title:
		return "# " + b.headingText()
	case ListItem:
		switch {
		case b.LabelKind == LabelBullet:
			return "- " + inlines(b.Inlines, false, true)
		case b.numberMarker():
			return b.Label + " " + inlines(b.Inlines, false, true)
		}
	}
	if b.Label != "" {
		return escape(b.Label, true) + " " + inlines(b.Inlines, false, false)
	}
	return inlines(b.Inlines, false, true)
}

// headingText renders the content of a heading, which is one line. A
// heading is bold already, so bold is not marked in it.
func (b Block) headingText() string {
	s := inlines(b.Inlines, true, b.Label == "")
	if b.Label != "" {
		s = escape(b.Label, false) + " " + s
	}
	if strings.HasSuffix(s, "#") && !strings.HasSuffix(s, `\#`) {
		// A row of # at the end of a heading line is a closing sequence.
		s = s[:len(s)-1] + `\#`
	}
	return s
}

// The marks of inline formatting, in the order they nest: bold outermost.
type marks uint8

const (
	bold marks = 1 << iota
	italic
	strike
	super
	sub
	code
)

var nesting = [...]struct {
	m           marks
	open, close string
}{
	{bold, "**", "**"},
	{italic, "*", "*"},
	{strike, "~~", "~~"},
	{super, "<sup>", "</sup>"},
	{sub, "<sub>", "</sub>"},
}

type segment struct {
	text  string
	m     marks
	link  string
	isBrk bool
}

func segments(in []Inline, heading, cell bool) []segment {
	segs := make([]segment, 0, len(in))
	for _, x := range in {
		if x.Kind == Break {
			if heading && !cell {
				segs = append(segs, segment{text: " "})
			} else {
				segs = append(segs, segment{isBrk: true})
			}
			continue
		}
		var m marks
		set := func(on bool, bit marks) {
			if on {
				m |= bit
			}
		}
		set(x.Bold && !heading, bold)
		set(x.Italic, italic)
		set(x.Strike, strike)
		set(x.Superscript, super)
		set(x.Subscript, sub)
		set(x.Code, code)
		segs = append(segs, segment{text: x.Text, m: m, link: x.Link})
	}
	// White space between two parts of the same link belongs to the link.
	for i := range segs {
		if segs[i].isBrk || strings.TrimSpace(segs[i].text) != "" {
			continue
		}
		segs[i].link = ""
		if i > 0 && i+1 < len(segs) && segs[i-1].link == segs[i+1].link {
			segs[i].link = segs[i-1].link
		}
	}
	return segs
}

// inlines renders the content of a block. start says that the content
// begins a line, where a few more characters are markup.
func inlines(in []Inline, heading, start bool) string {
	return render(in, heading, start, false)
}

// render is inlines with one more choice: cell says that the content goes
// into a cell of a pipe table, which is a single line.
func render(in []Inline, heading, start, cell bool) string {
	segs := segments(in, heading, cell)
	var sb strings.Builder
	lineStart := 0 // where the current line began in sb
	for i := 0; i < len(segs); {
		if segs[i].isBrk {
			trimTrailingSpace(&sb, lineStart)
			if cell {
				sb.WriteString("<br>")
			} else {
				// A backslash at the end of a line is a line break.
				sb.WriteString("\\\n")
			}
			lineStart = sb.Len()
			i++
			continue
		}
		j := i
		for j < len(segs) && !segs[j].isBrk && segs[j].link == segs[i].link {
			j++
		}
		run := segs[i:j]
		if segs[i].link == "" {
			sb.WriteString(formatted(run, !cell && sb.Len() == lineStart && (start || lineStart > 0)))
		} else if text := formatted(run, false); strings.TrimSpace(text) != "" {
			sb.WriteString("[" + text + "](" + destination(segs[i].link) + ")")
		}
		i = j
	}
	return strings.TrimRight(sb.String(), " \t")
}

func trimTrailingSpace(sb *strings.Builder, from int) {
	s := sb.String()
	t := strings.TrimRight(s[from:], " \t")
	if len(t) != len(s)-from {
		sb.Reset()
		sb.WriteString(s[:from] + t)
	}
}

// formatted renders segments that share a link, or have none, opening and
// closing the marks of bold, italic and so on as they change.
//
// Markdown wants a mark to touch the text it applies to: "**bold **text" is
// not bold. So white space at the edge of a formatted run is written outside
// its marks.
func formatted(segs []segment, atLineStart bool) string {
	var sb strings.Builder
	var open []int // indexes into nesting, in the order opened
	pending := ""  // white space not yet written
	closeTo := func(keep int) {
		for len(open) > keep {
			sb.WriteString(nesting[open[len(open)-1]].close)
			open = open[:len(open)-1]
		}
	}
	isOpen := func(i int) bool {
		for _, o := range open {
			if o == i {
				return true
			}
		}
		return false
	}
	for _, s := range segs {
		core := strings.TrimSpace(s.text)
		if core == "" {
			pending += s.text
			continue
		}
		lead := s.text[:strings.Index(s.text, core)]
		trail := s.text[len(lead)+len(core):]
		if atLineStart && sb.Len() == 0 {
			lead, pending = "", ""
		}
		// Keep the marks that go on, close the rest.
		keep := 0
		for keep < len(open) && s.m&nesting[open[keep]].m != 0 {
			keep++
		}
		closeTo(keep)
		sb.WriteString(pending)
		sb.WriteString(lead)
		start := atLineStart && sb.Len() == 0
		for i, n := range nesting {
			if s.m&n.m != 0 && !isOpen(i) {
				sb.WriteString(n.open)
				open = append(open, i)
			}
		}
		if s.m&code != 0 {
			sb.WriteString(codeSpan(core))
		} else {
			sb.WriteString(escape(core, start))
		}
		pending = trail
	}
	closeTo(0)
	sb.WriteString(pending)
	return sb.String()
}

// codeSpan wraps text in backticks: more of them than the text has in a
// row, and with a space of padding when the text itself begins or ends
// with one.
func codeSpan(s string) string {
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}

// destination writes a link address so that it cannot end the link early.
func destination(link string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "|", "%7C", "\n", "", "\r", "", "\t", "%20").Replace(link)
}

// escape protects the characters of s that Markdown would read as markup.
//
// It escapes only where a character would be taken as markup. Escaping
// every punctuation mark is always safe for a renderer, but it turns
// snake_case into snake\_case, which is worse to read and to search.
// atLineStart says that s begins a line, where a few more things are markup:
// a heading, a quotation, a list item, a rule.
func escape(s string, atLineStart bool) string {
	if !strings.ContainsAny(s, "\\`*_[]<&~#>+-=.)0123456789") {
		return s
	}
	var sb strings.Builder
	sb.Grow(len(s) + 8)
	rest := s
	if atLineStart {
		rest = lineStart(&sb, s)
	}
	prev := rune(-1)
	if len(rest) < len(s) {
		prev, _ = utf8.DecodeLastRuneInString(s[:len(s)-len(rest)])
	}
	for i, r := range rest {
		next := rune(-1)
		if j := i + utf8.RuneLen(r); j < len(rest) {
			next, _ = utf8.DecodeRuneInString(rest[j:])
		}
		esc := false
		switch r {
		case '\\', '`', '*':
			esc = true
		case '_':
			// An underscore inside a word is not emphasis.
			esc = !(alnum(prev) && alnum(next))
		case '[':
			esc = next == '^'
		case ']':
			esc = next == '(' || next == '[' || next == ':'
		case '<':
			esc = unicode.IsLetter(next) || next == '/' || next == '!' || next == '?'
		case '&':
			esc = entity(rest[i:])
		case '~':
			esc = prev == '~' || next == '~'
		}
		if esc {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
		prev = r
	}
	return sb.String()
}

// lineStart writes the start of s escaped, if it is something that only
// counts at the start of a line, and returns the rest of s.
func lineStart(sb *strings.Builder, s string) string {
	// A line of nothing but = or - underlines the line before as a heading,
	// and three - make a rule.
	if t := strings.TrimSpace(s); t != "" && (strings.Trim(t, "=") == "" || strings.Trim(t, "- ") == "" || strings.Trim(t, "_ ") == "" && len(t) >= 3) {
		sb.WriteByte('\\')
		sb.WriteByte(s[0])
		return s[1:]
	}
	switch s[0] {
	case '#':
		n := 0
		for n < len(s) && s[n] == '#' {
			n++
		}
		if n <= 6 && (n == len(s) || s[n] == ' ' || s[n] == '\t') {
			sb.WriteString(`\#`)
			return s[1:]
		}
	case '>':
		sb.WriteString(`\>`)
		return s[1:]
	case '-', '+':
		if len(s) == 1 || s[1] == ' ' || s[1] == '\t' {
			sb.WriteByte('\\')
			sb.WriteByte(s[0])
			return s[1:]
		}
	}
	// "12. " and "12) " begin a numbered list item.
	n := 0
	for n < len(s) && n < 9 && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	if n > 0 && n < len(s) && (s[n] == '.' || s[n] == ')') && (n+1 == len(s) || s[n+1] == ' ' || s[n+1] == '\t') {
		sb.WriteString(s[:n])
		sb.WriteByte('\\')
		sb.WriteByte(s[n])
		return s[n+1:]
	}
	return s
}

func alnum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// entity reports whether s, which starts with an ampersand, starts with
// something Markdown would read as a character reference: &amp; or &#38;.
func entity(s string) bool {
	for i := 1; i < len(s) && i < 34; i++ {
		c := s[i]
		switch {
		case c == ';':
			return i > 1
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '#' && i == 1:
		default:
			return false
		}
	}
	return false
}
