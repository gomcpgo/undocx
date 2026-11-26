// Package body reads the main part of a Word document as a stream and turns
// it into blocks of the model.
//
// The part is read once, front to back. Blocks are handed on as they are
// completed, so memory does not grow with the document. What carries over
// from one paragraph to the next is small: the fields still open, the
// headings the text stands under, and a paragraph waiting to be joined to the
// next one.
package body

import (
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gomcpgo/undocx/internal/diag"
	"github.com/gomcpgo/undocx/internal/model"
	"github.com/gomcpgo/undocx/internal/symbols"
	"github.com/gomcpgo/undocx/internal/wml"
	"github.com/gomcpgo/undocx/internal/xmlstream"
)

const (
	nsMath = "http://schemas.openxmlformats.org/officeDocument/2006/math"

	// Warning codes.
	WarnSymbol      = "unmapped-symbol"
	WarnLink        = "link-dropped"
	WarnEquation    = "equation-as-text"
	WarnAltChunk    = "embedded-content"
	WarnPlaceholder = "placeholders-omitted"

	maxFields    = 64   // nested fields
	maxInstr     = 4096 // bytes of one field instruction kept
	maxPathTitle = 200  // characters of a heading kept in a heading path
)

// Understood reports whether the reader handles the elements of a namespace
// that an mc:Choice requires. It handles text boxes in drawing shapes,
// groups and canvases, and the Word extensions that only add attributes and
// markers. For anything else, such as newer chart and ink formats, the
// fallback is the better branch: it holds a picture or plain markup.
func Understood(ns string) bool {
	switch ns {
	case "http://schemas.microsoft.com/office/word/2010/wordprocessingShape",
		"http://schemas.microsoft.com/office/word/2010/wordprocessingGroup",
		"http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas",
		"http://schemas.microsoft.com/office/word/2010/wordprocessingDrawing",
		"http://schemas.microsoft.com/office/word/2010/wordml",
		"http://schemas.microsoft.com/office/word/2012/wordml":
		return true
	}
	return strings.HasPrefix(ns, "http://schemas.microsoft.com/office/word/20") && strings.Contains(ns, "/wordml")
}

// Config is what the reader needs besides the part itself.
type Config struct {
	Styles *wml.Styles
	// Numbering is the list definitions, or nil when there are none.
	Numbering *wml.Numbering
	// Links maps the relationship ID of an external hyperlink to its
	// address.
	Links map[string]string
	Log   *diag.Log
	// TOC keeps a generated table of contents, which is otherwise left
	// out because it repeats the headings.
	TOC bool
	// Placeholders keeps the prompt text of content controls that nobody
	// has filled in.
	Placeholders bool
}

// field is a field that has begun and not ended. Between its begin and its
// separator comes the instruction, which is not text of the document; then
// the result that the last program to save the file left in it.
type field struct {
	instr    strings.Builder
	inResult bool
	hide     bool   // the result is left out
	link     string // the result links here
}

type heading struct {
	level int
	title string
}

type walker struct {
	x      *xmlstream.Reader
	cfg    Config
	out    func(model.Block) bool
	stop   bool
	blocks int

	fields   []*field
	overflow int // fields begun beyond maxFields

	headings []heading
	path     []string
	lists    *lists

	// carry is the content of a paragraph that runs on into the next: one
	// whose paragraph mark is a tracked deletion, or a drop cap.
	carry []model.Inline
	// box counts the text boxes being read. Their paragraphs do not open
	// sections of the document.
	box int

	placeholders int
	unmapped     map[string]int
	equations    int
	badSchemes   map[string]int
}

// Read reads the main part from x, which must be at its start, and calls
// yield with each block in turn until it returns false.
//
// It returns the number of blocks completed. On malformed XML the error
// matches diag.ErrDamaged and the blocks before the damage have been yielded.
func Read(x *xmlstream.Reader, cfg Config, yield func(model.Block) bool) (int, error) {
	if cfg.Styles == nil {
		cfg.Styles = wml.NoStyles()
	}
	w := &walker{x: x, cfg: cfg, out: yield, unmapped: map[string]int{}, badSchemes: map[string]int{}}
	w.lists = newLists(cfg.Numbering, cfg.Styles)
	err := w.readBlocks(0)
	if err == nil && !w.stop {
		w.flushCarry()
	}
	w.report()
	return w.blocks, err
}

// report turns what was counted along the way into warnings.
func (w *walker) report() {
	for _, font := range sortedKeys(w.unmapped) {
		w.cfg.Log.Add(WarnSymbol, "%d characters in the symbol font %s have no Unicode equivalent and appear as U+FFFD", w.unmapped[font], font)
	}
	for _, scheme := range sortedKeys(w.badSchemes) {
		w.cfg.Log.Add(WarnLink, "%d links of the kind %q were left as plain text", w.badSchemes[scheme], scheme)
	}
	if w.placeholders > 0 {
		w.cfg.Log.Add(WarnPlaceholder, "%d content controls that nobody has filled in were left out; they hold prompts such as \"Click here to enter text\"", w.placeholders)
	}
	if w.equations > 0 {
		w.cfg.Log.Add(WarnEquation, "%d equations were written as plain text", w.equations)
	}
	w.lists.report(w.cfg.Log)
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Few keys; insertion sort keeps the package free of imports for it.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

func (w *walker) emit(b model.Block) {
	if w.stop {
		return
	}
	w.blocks++
	if !w.out(b) {
		w.stop = true
	}
}

// readBlocks reads block-level content until the element at depth until has
// ended, or to the end of the part when until is 0.
//
// It does not call itself for nested containers. Table rows and cells,
// content controls and custom XML wrappers are simply passed through, and
// each paragraph found is a block, however deep. Tables get their structure
// in a later version; here their text is kept in reading order.
func (w *walker) readBlocks(until int) error {
	var rows, controls []int // depths of the open w:tr and w:sdt elements
	for !w.stop {
		tok, err := w.x.Token()
		if err == io.EOF {
			if until == 0 {
				return nil
			}
			return diag.Damaged("the document ends inside a text box")
		}
		if err != nil {
			return err
		}
		switch el := tok.(type) {
		case xml.EndElement:
			d := w.x.Depth()
			rows, controls = popAbove(rows, d), popAbove(controls, d)
			if d < until {
				return nil
			}
		case xml.StartElement:
			if el.Name.Space != wml.NS {
				continue
			}
			switch el.Name.Local {
			case "p":
				if err := w.paragraph(); err != nil {
					return err
				}
			case "tr":
				rows = append(rows, w.x.Depth())
			case "trPr":
				// A row that is a tracked deletion is gone once the
				// change is accepted.
				deleted, err := w.hasChild("del")
				if err != nil {
					return err
				}
				if deleted && len(rows) > 0 {
					if err := w.x.SkipTo(rows[len(rows)-1]); err != nil {
						return err
					}
					rows = rows[:len(rows)-1]
				}
			case "sdt":
				controls = append(controls, w.x.Depth())
			case "sdtPr":
				// A content control that nobody has filled in shows a
				// prompt such as "Click here to enter text", which is
				// not content.
				placeholder, err := w.hasChild("showingPlcHdr")
				if err != nil {
					return err
				}
				if placeholder && len(controls) > 0 && !w.cfg.Placeholders {
					w.placeholders++
					if err := w.x.SkipTo(controls[len(controls)-1]); err != nil {
						return err
					}
					controls = controls[:len(controls)-1]
				}
			case "altChunk":
				w.cfg.Log.Add(WarnAltChunk, "the document embeds content in another format (w:altChunk), which was not read")
				if err := w.x.Skip(); err != nil {
					return err
				}
			case "sectPr", "tblPr", "tblGrid", "tcPr", "tblPrEx":
				if err := w.x.Skip(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// popAbove drops the depths of elements that have ended, given the depth
// the reader is at now.
func popAbove(stack []int, depth int) []int {
	for n := len(stack); n > 0 && stack[n-1] > depth; n = len(stack) {
		stack = stack[:n-1]
	}
	return stack
}

// hasChild reads the element whose start tag was read last, and reports
// whether it has a child of the given name that is not switched off.
func (w *walker) hasChild(name string) (bool, error) {
	depth := w.x.Depth()
	found := false
	for {
		tok, err := w.x.Token()
		if err != nil {
			if err == io.EOF {
				err = diag.Damaged("the document ends inside an element")
			}
			return found, err
		}
		switch el := tok.(type) {
		case xml.EndElement:
			if w.x.Depth() < depth {
				return found, nil
			}
		case xml.StartElement:
			if w.x.Depth() == depth+1 && el.Name.Space == wml.NS && el.Name.Local == name && wml.Bool(el) == wml.On {
				found = true
			}
			if err := w.x.Skip(); err != nil {
				return found, err
			}
		}
	}
}

// visible reports whether text at this point is text of the document: not
// part of a field instruction, and not in a result that is left out.
func (w *walker) visible() bool {
	for _, f := range w.fields {
		if !f.inResult || f.hide {
			return false
		}
	}
	return true
}

// para is the state of the paragraph being read.
type para struct {
	depth    int
	props    wml.ParaProps
	style    *wml.Style
	styled   bool
	inlines  []model.Inline
	after    []model.Block // text boxes anchored in the paragraph
	links    []frame       // open w:hyperlink elements
	simple   []int         // depths of the open w:fldSimple elements
	controls []int         // depths of the open w:sdt elements
	deleted  []int         // depths of the open w:del and w:moveFrom elements

	runDepth int // 0 outside a run
	runProps wml.RunProps
	format   wml.Format
	resolved bool
}

type frame struct {
	depth int
	link  string
}

func (w *walker) paragraph() error {
	p := &para{depth: w.x.Depth(), inlines: w.carry}
	w.carry = nil
	for {
		tok, err := w.x.Token()
		if err != nil {
			if err == io.EOF {
				err = diag.Damaged("the document ends inside a paragraph")
			}
			// Keep what was read of the paragraph that the damage cut.
			w.finish(p)
			return err
		}
		switch el := tok.(type) {
		case xml.EndElement:
			d := w.x.Depth()
			if d < p.depth {
				w.finish(p)
				return nil
			}
			if p.runDepth > d {
				p.runDepth = 0
			}
			for n := len(p.links); n > 0 && p.links[n-1].depth > d; n = len(p.links) {
				p.links = p.links[:n-1]
			}
			for n := len(p.simple); n > 0 && p.simple[n-1] > d; n = len(p.simple) {
				p.simple = p.simple[:n-1]
				w.endField()
			}
			p.controls, p.deleted = popAbove(p.controls, d), popAbove(p.deleted, d)
		case xml.StartElement:
			if err := w.inline(p, el); err != nil {
				w.finish(p)
				return err
			}
		}
	}
}

// inline handles a start tag inside a paragraph.
func (w *walker) inline(p *para, el xml.StartElement) error {
	x := w.x
	if el.Name.Space == nsMath {
		if el.Name.Local == "oMath" || el.Name.Local == "oMathPara" {
			text, err := w.mathText()
			if err != nil {
				return err
			}
			w.equations++
			w.text(p, text, true)
		}
		return nil
	}
	if el.Name.Space != wml.NS {
		// Drawings and VML shapes: nothing of theirs is text, but a text
		// box inside one holds paragraphs, so the search goes on inside.
		return nil
	}
	switch el.Name.Local {
	case "pPr":
		props, err := wml.ReadParaProps(x)
		p.props = props
		return err
	case "r":
		p.runDepth, p.runProps, p.resolved = x.Depth(), wml.RunProps{}, false
	case "rPr":
		if p.runDepth != 0 && x.Depth() == p.runDepth+1 {
			props, err := wml.ReadRunProps(x)
			p.runProps, p.resolved = props, false
			return err
		}
		return x.Skip()
	case "t":
		s, err := w.chars()
		if p.runDepth != 0 {
			w.text(p, s, false) // also what was read before an error
		}
		if err != nil {
			return err
		}
	case "tab", "ptab":
		w.text(p, "\t", true)
		return x.Skip()
	case "br", "cr":
		if w.shown(p) {
			p.inlines = append(p.inlines, model.Inline{Kind: model.Break})
		}
		return x.Skip()
	case "noBreakHyphen":
		w.text(p, "-", true)
		return x.Skip()
	case "softHyphen":
		return x.Skip()
	case "sym":
		w.symbol(p, wml.Attr(el, "font"), wml.Attr(el, "char"))
		return x.Skip()
	case "instrText":
		s, err := w.chars()
		if err != nil {
			return err
		}
		if n := len(w.fields); n > 0 && !w.fields[n-1].inResult && len(p.deleted) == 0 {
			if f := w.fields[n-1]; f.instr.Len() < maxInstr {
				f.instr.WriteString(s)
			}
		}
	case "fldChar":
		switch wml.Attr(el, "fldCharType") {
		case "begin":
			w.beginField("")
		case "separate":
			if n := len(w.fields); n > 0 && w.overflow == 0 {
				w.separate(w.fields[n-1])
			}
		case "end":
			w.endField()
		}
		return x.Skip()
	case "fldSimple":
		w.beginField(wml.Attr(el, "instr"))
		if n := len(w.fields); n > 0 && w.overflow == 0 {
			w.separate(w.fields[n-1])
		}
		p.simple = append(p.simple, x.Depth())
	case "hyperlink":
		link := ""
		for _, a := range el.Attr {
			if a.Name.Local == "id" && a.Name.Space != wml.NS {
				link = w.cfg.Links[a.Value]
			}
		}
		if link != "" {
			if anchor := wml.Attr(el, "anchor"); anchor != "" && !strings.Contains(link, "#") {
				link += "#" + anchor
			}
		}
		p.links = append(p.links, frame{x.Depth(), w.safeLink(link)})
	case "del", "moveFrom":
		// Deleted and moved-away text is read past, not skipped: a field
		// may begin or end inside it, and the count of open fields has
		// to stay right.
		p.deleted = append(p.deleted, x.Depth())
	case "sdt":
		p.controls = append(p.controls, x.Depth())
	case "sdtPr":
		placeholder, err := w.hasChild("showingPlcHdr")
		if err != nil {
			return err
		}
		if placeholder && len(p.controls) > 0 && !w.cfg.Placeholders {
			w.placeholders++
			depth := p.controls[len(p.controls)-1]
			p.controls = p.controls[:len(p.controls)-1]
			return x.SkipTo(depth)
		}
	case "txbxContent":
		return w.textBox(p)
	case "delText", "delInstrText", "rt", "footnoteReference", "endnoteReference", "commentReference":
		return x.Skip()
	}
	// Everything else is passed through: w:ins, w:moveTo, w:smartTag,
	// w:customXml, w:dir, w:bdo, w:sdtContent, w:ruby and whatever a later
	// version of Word adds. Text is only ever taken from w:t in a run, so
	// passing through a wrapper cannot add anything that is not text.
	return nil
}

// textBox reads the paragraphs of a text box, which are shown after the
// paragraph it is anchored in.
func (w *walker) textBox(p *para) error {
	out, carry := w.out, w.carry
	w.out = func(b model.Block) bool {
		p.after = append(p.after, b)
		return true
	}
	w.carry = nil
	w.box++
	blocks := w.blocks
	err := w.readBlocks(w.x.Depth())
	w.flushCarry()
	w.box--
	w.blocks = blocks // counted when the anchoring paragraph hands them on
	w.out, w.carry = out, carry
	return err
}

// chars returns the text content of the element whose start tag was read
// last.
func (w *walker) chars() (string, error) {
	depth := w.x.Depth()
	var first string
	var sb *strings.Builder
	for {
		tok, err := w.x.Token()
		if err != nil {
			if err == io.EOF {
				err = diag.Damaged("the document ends inside a run of text")
			}
			return first, err
		}
		switch t := tok.(type) {
		case xml.CharData:
			if sb == nil && first == "" {
				first = string(t)
			} else {
				if sb == nil {
					sb = &strings.Builder{}
					sb.WriteString(first)
				}
				sb.Write(t)
			}
		case xml.EndElement:
			if w.x.Depth() < depth {
				if sb != nil {
					return sb.String(), nil
				}
				return first, nil
			}
		}
	}
}

// mathText returns the text of an equation: the content of its m:t elements,
// with a slash between the two halves of a fraction. It is a stand-in until
// equations are converted properly.
func (w *walker) mathText() (string, error) {
	depth := w.x.Depth()
	var sb strings.Builder
	for {
		tok, err := w.x.Token()
		if err != nil {
			if err == io.EOF {
				err = diag.Damaged("the document ends inside an equation")
			}
			return sb.String(), err
		}
		switch el := tok.(type) {
		case xml.EndElement:
			if w.x.Depth() < depth {
				return sb.String(), nil
			}
		case xml.StartElement:
			if el.Name.Space != nsMath {
				continue
			}
			switch el.Name.Local {
			case "t":
				s, err := w.chars()
				if err != nil {
					return sb.String(), err
				}
				sb.WriteString(s)
			case "den":
				sb.WriteByte('/')
			}
		}
	}
}

// shown reports whether content of the current run is part of the document
// as it reads with all changes accepted and hidden text left out.
func (w *walker) shown(p *para) bool {
	if len(p.deleted) > 0 || !w.visible() {
		return false
	}
	return !w.runFormat(p).Hidden
}

func (w *walker) runFormat(p *para) wml.Format {
	if !p.styled {
		p.style, p.styled = w.cfg.Styles.Para(p.props.Style), true
	}
	if p.runDepth == 0 {
		return w.cfg.Styles.Resolve(p.style, wml.RunProps{})
	}
	if !p.resolved {
		p.format, p.resolved = w.cfg.Styles.Resolve(p.style, p.runProps), true
	}
	return p.format
}

// text adds text to the paragraph. literal text is a character the reader
// made, such as the hyphen for w:noBreakHyphen, and is not looked up in a
// symbol font.
func (w *walker) text(p *para, s string, literal bool) {
	if s == "" || !w.shown(p) {
		return
	}
	f := w.runFormat(p)
	if !literal {
		if symbols.Font(f.Font) {
			s = w.mapSymbols(f.Font, s)
		}
		s = clean(s)
	}
	in := model.Inline{
		Text:        s,
		Bold:        f.Bold,
		Italic:      f.Italic,
		Strike:      f.Strike,
		Code:        f.CodeStyle || wml.Monospace(f.Font),
		Superscript: f.Vert == wml.VertSuperscript,
		Subscript:   f.Vert == wml.VertSubscript,
		Link:        w.link(p),
	}
	p.inlines = model.Append(p.inlines, in)
}

// clean makes text easier to search and to read back. A soft hyphen is only
// a place where a word may break. The non-breaking hyphen and space stand
// for a hyphen and a space, and a search for "well-known" should find them.
// Line ends and tabs typed into the XML are not line ends and tabs of the
// document, which have elements of their own.
func clean(s string) string {
	if !strings.ContainsAny(s, "­‑ \n\r\t") {
		return s
	}
	return strings.NewReplacer("­", "", "‑", "-", " ", " ", "\r\n", " ", "\n", " ", "\r", " ", "\t", " ").Replace(s)
}

func (w *walker) mapSymbols(font, s string) string {
	var sb strings.Builder
	for _, r := range s {
		pos, ok := symbols.Position(r)
		if !ok {
			sb.WriteRune(r)
			continue
		}
		if m, ok := symbols.Map(font, pos); ok {
			sb.WriteRune(m)
		} else {
			w.unmapped[font]++
			sb.WriteRune(utf8.RuneError)
		}
	}
	return sb.String()
}

// symbol handles w:sym, a single character of a named font.
func (w *walker) symbol(p *para, font, char string) {
	code, err := strconv.ParseUint(char, 16, 32)
	if err != nil || !w.shown(p) {
		return
	}
	r := rune(code)
	if symbols.Font(font) {
		if pos, ok := symbols.Position(r); ok {
			if m, ok := symbols.Map(font, pos); ok {
				r = m
			} else {
				w.unmapped[font]++
				r = utf8.RuneError
			}
		}
	} else if r >= 0xF000 && r <= 0xF0FF {
		// A private-use position in a font that is not known to be a
		// symbol font: there is no telling what it shows.
		w.unmapped[font]++
		r = utf8.RuneError
	}
	if !utf8.ValidRune(r) || r < 0x20 {
		return
	}
	w.text(p, string(r), true)
}

// link returns the address that text at this point links to.
func (w *walker) link(p *para) string {
	for i := len(w.fields) - 1; i >= 0; i-- {
		if w.fields[i].link != "" {
			return w.fields[i].link
		}
	}
	if n := len(p.links); n > 0 {
		return p.links[n-1].link
	}
	return ""
}

// safeLink returns the address when it is of a kind that is safe to hand to
// a program that shows Markdown: a web page, a mail address or a telephone
// number. Anything else, such as a script or a file path, is dropped and
// counted, and the text of the link stays as plain text.
func (w *walker) safeLink(link string) string {
	link = strings.TrimSpace(link)
	if link == "" {
		return ""
	}
	scheme, _, ok := strings.Cut(link, ":")
	if ok {
		switch strings.ToLower(scheme) {
		case "http", "https", "mailto", "tel":
			return link
		}
	}
	if !ok || strings.ContainsAny(scheme, "/?#. ") {
		scheme = "relative"
	}
	if len(scheme) > 20 {
		scheme = scheme[:20]
	}
	w.badSchemes[strings.ToLower(scheme)]++
	return ""
}

func (w *walker) beginField(instr string) {
	if len(w.fields) >= maxFields {
		w.overflow++
		return
	}
	f := &field{}
	f.instr.WriteString(instr)
	w.fields = append(w.fields, f)
}

func (w *walker) endField() {
	if w.overflow > 0 {
		w.overflow--
		return
	}
	if n := len(w.fields); n > 0 {
		w.fields = w.fields[:n-1]
	}
}

// separate ends the instruction of a field and decides what to do with its
// result.
func (w *walker) separate(f *field) {
	f.inResult = true
	args := fieldArgs(f.instr.String())
	if len(args) == 0 {
		return
	}
	switch strings.ToUpper(args[0]) {
	case "TOC":
		f.hide = !w.cfg.TOC
	case "HYPERLINK":
		// HYPERLINK "address" \l "bookmark" \o "tip" \t "frame" \n \m
		link, anchor := "", ""
		for i := 1; i < len(args); i++ {
			switch a := args[i]; {
			case strings.EqualFold(a, `\l`):
				if i+1 < len(args) {
					anchor = args[i+1]
					i++
				}
			case strings.EqualFold(a, `\o`), strings.EqualFold(a, `\t`):
				i++
			case strings.HasPrefix(a, `\`):
			case link == "":
				link = a
			}
		}
		if link != "" {
			if anchor != "" && !strings.Contains(link, "#") {
				link += "#" + anchor
			}
			f.link = w.safeLink(link)
		}
	}
}

// fieldArgs splits a field instruction into its words. A word in double
// quotes may hold spaces, and a backslash inside the quotes makes the next
// character literal.
func fieldArgs(instr string) []string {
	var args []string
	var cur strings.Builder
	quoted, any := false, false
	flush := func() {
		if any {
			args = append(args, cur.String())
			cur.Reset()
			any = false
		}
	}
	for i := 0; i < len(instr); i++ {
		c := instr[i]
		switch {
		case quoted && c == '\\' && i+1 < len(instr) && (instr[i+1] == '"' || instr[i+1] == '\\'):
			cur.WriteByte(instr[i+1])
			i++
		case c == '"':
			quoted = !quoted
			any = true
		case !quoted && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			flush()
		default:
			cur.WriteByte(c)
			any = true
		}
	}
	flush()
	return args
}

// finish makes a block of the paragraph and hands it on, with the text
// boxes anchored in it after it.
func (w *walker) finish(p *para) {
	// A w:fldSimple cut off by damage must not stay open.
	for range p.simple {
		w.endField()
	}
	p.simple = nil
	switch {
	case p.props.DeletedMark || p.props.DropCap:
		// The paragraph runs on into the next one.
		w.carry = p.inlines
	default:
		w.block(p.props, p.inlines)
	}
	for _, b := range p.after {
		w.emit(b)
	}
}

// flushCarry hands on a paragraph that was waiting to be joined to a next
// one that never came.
func (w *walker) flushCarry() {
	if len(w.carry) > 0 {
		w.block(wml.ParaProps{}, w.carry)
		w.carry = nil
	}
}

func (w *walker) block(props wml.ParaProps, inlines []model.Inline) {
	// An empty numbered paragraph still takes a number, so the counting
	// comes before the check for content.
	num, numbered := w.lists.number(props)
	inlines = trim(inlines)
	if model.Empty(inlines) {
		return
	}
	style := w.cfg.Styles.Para(props.Style)
	b := model.Block{Inlines: inlines, Label: num.label}
	if style != nil {
		b.Style = style.Name
	}
	level := props.Outline
	switch {
	case level == 0:
		level = w.cfg.Styles.Outline(style)
	case level > 9:
		level = 0 // set to body text on the paragraph itself
	}
	switch {
	case w.box > 0 && numbered:
		b.Kind, b.Level, b.LabelKind = model.ListItem, num.level+1, num.kind
	case w.box > 0:
		// A heading in a text box is not a section of the document.
	case level > 0:
		b.Kind, b.Level = model.Heading, level
	case strings.EqualFold(b.Style, "title"):
		b.Kind = model.Title
	case numbered:
		b.Kind, b.Level, b.LabelKind = model.ListItem, num.level+1, num.kind
	}
	if b.Kind == model.Heading {
		for n := len(w.headings); n > 0 && w.headings[n-1].level >= level; n = len(w.headings) {
			w.headings = w.headings[:n-1]
		}
		b.Path = titles(w.headings)
		w.headings = append(w.headings, heading{level, pathTitle(b.Label, inlines)})
		w.path = titles(w.headings)
	} else {
		b.Path = w.path
	}
	w.emit(b)
}

func titles(hs []heading) []string {
	if len(hs) == 0 {
		return nil
	}
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.title
	}
	return out
}

// pathTitle returns the text of a heading as it appears in a heading path:
// on one line and of bounded length.
func pathTitle(label string, inlines []model.Inline) string {
	s := strings.Join(strings.Fields(label+" "+model.PlainText(inlines)), " ")
	if utf8.RuneCountInString(s) > maxPathTitle {
		s = string([]rune(s)[:maxPathTitle]) + "…"
	}
	return s
}

// trim removes white space and line breaks from the two ends of a
// paragraph.
func trim(inlines []model.Inline) []model.Inline {
	for len(inlines) > 0 {
		first := &inlines[0]
		if first.Kind == model.Text {
			first.Text = strings.TrimLeft(first.Text, " \t")
			if first.Text != "" {
				break
			}
		}
		inlines = inlines[1:]
	}
	for len(inlines) > 0 {
		last := &inlines[len(inlines)-1]
		if last.Kind == model.Text {
			last.Text = strings.TrimRight(last.Text, " \t")
			if last.Text != "" {
				break
			}
		}
		inlines = inlines[:len(inlines)-1]
	}
	return inlines
}
