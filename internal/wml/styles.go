// Package wml reads the parts of a Word document that describe it rather
// than hold its text: today the styles. These parts are small and are read
// whole, under the size limit every part has.
package wml

import (
	"encoding/xml"
	"io"
	"strconv"
	"strings"

	"github.com/gomcpgo/undocx/internal/xmlstream"
)

// NS is the WordprocessingML namespace, in its Transitional form. The XML
// reader has already turned the Strict form into it.
const NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

// Tri is a property that a style or a run may leave unsaid.
type Tri int8

const (
	Unset Tri = iota
	On
	Off
)

// Bool turns a w:val attribute of an on/off element into a Tri. The element
// being there with no value means on (ECMA-376 Part 1, 17.17.4).
func Bool(el xml.StartElement) Tri {
	for _, a := range el.Attr {
		if a.Name.Local == "val" {
			switch a.Value {
			case "0", "false", "off":
				return Off
			}
			return On
		}
	}
	return On
}

// Val returns the w:val attribute of an element.
func Val(el xml.StartElement) string {
	return Attr(el, "val")
}

// Attr returns the attribute of an element with the given local name.
func Attr(el xml.StartElement, local string) string {
	for _, a := range el.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// Vertical alignment of a run.
const (
	VertUnset = iota
	VertBaseline
	VertSuperscript
	VertSubscript
)

// RunProps is the formatting a w:rPr element states. Anything it does not
// state is Unset, zero or empty.
type RunProps struct {
	Style   string // w:rStyle: the ID of a character style
	Bold    Tri
	Italic  Tri
	BoldCS  Tri // bold and italic for complex scripts, which right-to-left text uses
	ItalCS  Tri
	Strike  Tri
	DStrike Tri
	Vanish  Tri
	Complex Tri // w:cs or w:rtl: the run is complex-script text
	Vert    int
	Font    string // the font for Latin text
}

// ParaProps is what a w:pPr element states, of the properties the reader
// uses.
type ParaProps struct {
	Style   string // w:pStyle: the ID of a paragraph style
	Outline int    // w:outlineLvl plus one: 1 to 9 for a heading level, 10 for body text, 0 when unset
	// DeletedMark reports that the paragraph mark is a tracked deletion:
	// with the change accepted, this paragraph runs on into the next.
	DeletedMark bool
	// DropCap reports that the paragraph is a drop cap, which belongs to
	// the start of the next paragraph.
	DropCap bool
}

// ReadRunProps reads a w:rPr element whose start tag has just been read.
func ReadRunProps(x *xmlstream.Reader) (RunProps, error) {
	var p RunProps
	depth := x.Depth()
	for {
		tok, err := x.Token()
		if err != nil {
			return p, unexpectedEOF(err)
		}
		switch el := tok.(type) {
		case xml.EndElement:
			if x.Depth() < depth {
				return p, nil
			}
		case xml.StartElement:
			if x.Depth() != depth+1 || el.Name.Space != NS {
				// A tracked formatting change holds the old properties
				// in a nested w:rPr: they must not be taken for these.
				if err := x.Skip(); err != nil {
					return p, err
				}
				continue
			}
			switch el.Name.Local {
			case "rStyle":
				p.Style = Val(el)
			case "b":
				p.Bold = Bool(el)
			case "i":
				p.Italic = Bool(el)
			case "bCs":
				p.BoldCS = Bool(el)
			case "iCs":
				p.ItalCS = Bool(el)
			case "strike":
				p.Strike = Bool(el)
			case "dstrike":
				p.DStrike = Bool(el)
			case "vanish":
				p.Vanish = Bool(el)
			case "cs", "rtl":
				if Bool(el) == On {
					p.Complex = On
				}
			case "vertAlign":
				switch Val(el) {
				case "superscript":
					p.Vert = VertSuperscript
				case "subscript":
					p.Vert = VertSubscript
				default:
					p.Vert = VertBaseline
				}
			case "rFonts":
				for _, name := range []string{"ascii", "hAnsi", "cs", "eastAsia"} {
					if f := Attr(el, name); f != "" {
						p.Font = f
						break
					}
				}
			}
			if err := x.Skip(); err != nil {
				return p, err
			}
		}
	}
}

// ReadParaProps reads a w:pPr element whose start tag has just been read.
// The formatting of the paragraph mark (the w:rPr inside) is not run
// formatting and is read only for the tracked deletion of the mark.
func ReadParaProps(x *xmlstream.Reader) (ParaProps, error) {
	var p ParaProps
	depth := x.Depth()
	for {
		tok, err := x.Token()
		if err != nil {
			return p, unexpectedEOF(err)
		}
		switch el := tok.(type) {
		case xml.EndElement:
			if x.Depth() < depth {
				return p, nil
			}
		case xml.StartElement:
			if el.Name.Space != NS {
				if err := x.Skip(); err != nil {
					return p, err
				}
				continue
			}
			if x.Depth() == depth+1 {
				switch el.Name.Local {
				case "pStyle":
					p.Style = Val(el)
				case "outlineLvl":
					if n, err := strconv.Atoi(Val(el)); err == nil && n >= 0 && n <= 9 {
						p.Outline = n + 1
					}
				case "framePr":
					if d := Attr(el, "dropCap"); d == "drop" || d == "margin" {
						p.DropCap = true
					}
				case "rPr":
					continue // look inside for a deleted mark
				}
			} else if x.Depth() == depth+2 && el.Name.Local == "del" {
				p.DeletedMark = true
			}
			if err := x.Skip(); err != nil {
				return p, err
			}
		}
	}
}

func unexpectedEOF(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// Style is one style definition.
type Style struct {
	ID      string
	Name    string // the name Word shows; for built-in styles it is the same in every language
	Type    string // "paragraph", "character", "table" or "numbering"
	BasedOn string
	Para    ParaProps
	Run     RunProps
}

// Styles is the style sheet of a document.
type Styles struct {
	byID        map[string]*Style
	defaultPara *Style
	defaults    RunProps
}

// NoStyles is the style sheet of a document that has none.
func NoStyles() *Styles {
	return &Styles{byID: map[string]*Style{}}
}

// ReadStyles reads the styles part. On an error it returns the styles read
// before it, which are still usable.
func ReadStyles(x *xmlstream.Reader) (*Styles, error) {
	s := NoStyles()
	var cur *Style
	for {
		tok, err := x.Token()
		if err == io.EOF {
			return s, nil
		}
		if err != nil {
			return s, err
		}
		switch el := tok.(type) {
		case xml.EndElement:
			if el.Name.Local == "style" && x.Depth() == 1 {
				cur = nil
			}
		case xml.StartElement:
			if el.Name.Space != NS {
				if err := x.Skip(); err != nil {
					return s, err
				}
				continue
			}
			switch {
			case x.Depth() == 1:
				// The root element.
			case x.Depth() == 2 && el.Name.Local == "style":
				cur = &Style{ID: Attr(el, "styleId"), Type: Attr(el, "type")}
				if cur.Type == "" {
					cur.Type = "paragraph"
				}
				if _, dup := s.byID[cur.ID]; !dup {
					s.byID[cur.ID] = cur
				}
				if on := Attr(el, "default"); cur.Type == "paragraph" && s.defaultPara == nil && (on == "1" || on == "true" || on == "on") {
					s.defaultPara = cur
				}
			case x.Depth() == 2 && el.Name.Local == "docDefaults", x.Depth() == 3 && el.Name.Local == "rPrDefault":
				// Descend to the default run properties.
			case x.Depth() == 4 && el.Name.Local == "rPr" && cur == nil:
				if s.defaults, err = ReadRunProps(x); err != nil {
					return s, err
				}
			case cur != nil && x.Depth() == 3:
				switch el.Name.Local {
				case "name":
					cur.Name = Val(el)
				case "basedOn":
					cur.BasedOn = Val(el)
				case "pPr":
					if cur.Para, err = ReadParaProps(x); err != nil {
						return s, err
					}
					continue
				case "rPr":
					if cur.Run, err = ReadRunProps(x); err != nil {
						return s, err
					}
					continue
				}
				if err := x.Skip(); err != nil {
					return s, err
				}
			default:
				if err := x.Skip(); err != nil {
					return s, err
				}
			}
		}
	}
}

// maxChain bounds how far a chain of w:basedOn is followed, which also ends
// a chain that loops.
const maxChain = 32

// chain calls f for a style and the styles it is based on, nearest first,
// until f returns false.
func (s *Styles) chain(id string, f func(*Style) bool) {
	for i := 0; i < maxChain && id != ""; i++ {
		st := s.byID[id]
		if st == nil || !f(st) {
			return
		}
		id = st.BasedOn
	}
}

// Para returns the paragraph style with the given ID, or the default
// paragraph style when the ID is empty or unknown. It may return nil.
func (s *Styles) Para(id string) *Style {
	if st := s.byID[id]; st != nil && id != "" {
		return st
	}
	return s.defaultPara
}

// Outline returns the heading level a paragraph style gives its paragraphs,
// 1 to 9, or 0 for none. The outline level decides, set on the style or on
// one it is based on. Failing that the name does: the built-in heading
// styles are called "heading 1" to "heading 9" in every language, while their
// IDs are translated.
func (s *Styles) Outline(st *Style) int {
	if st == nil {
		return 0
	}
	level := 0
	s.chain(st.ID, func(c *Style) bool {
		if c.Para.Outline != 0 {
			level = c.Para.Outline
		} else if n := headingName(c.Name); n != 0 {
			level = n
		}
		return level == 0
	})
	if level > 9 {
		return 0 // the level that means body text
	}
	return level
}

// headingName returns N for the style name "heading N", 1 to 9.
func headingName(name string) int {
	rest, ok := strings.CutPrefix(strings.ToLower(name), "heading ")
	if ok && len(rest) == 1 && rest[0] >= '1' && rest[0] <= '9' {
		return int(rest[0] - '0')
	}
	return 0
}

// Format is the formatting of a run after everything that sets it has been
// taken into account.
type Format struct {
	Bold, Italic, Strike, Hidden bool
	Vert                         int
	Font                         string
	// CodeStyle reports that a style in force is one meant for code.
	CodeStyle bool
}

// Resolve works out the formatting of a run from its own properties, its
// character style and its paragraph's style.
//
// Bold, italic and the other on/off properties are toggles (ECMA-376 Part 1,
// 17.7.3): direct formatting states the result outright, but a paragraph
// style and a character style that both turn one on cancel out. This is why
// text in the Strong style inside a bold heading is not bold in Word. Within
// one chain of styles based on each other, the nearest one that speaks wins.
// The document defaults apply when no style speaks.
func (s *Styles) Resolve(para *Style, run RunProps) Format {
	var pchain, cchain []*Style
	if para != nil {
		s.chain(para.ID, func(c *Style) bool { pchain = append(pchain, c); return true })
	}
	if run.Style != "" {
		s.chain(run.Style, func(c *Style) bool { cchain = append(cchain, c); return true })
	}
	nearest := func(chain []*Style, get func(*RunProps) Tri) Tri {
		for _, c := range chain {
			if v := get(&c.Run); v != Unset {
				return v
			}
		}
		return Unset
	}
	toggle := func(get func(*RunProps) Tri) bool {
		if v := get(&run); v != Unset {
			return v == On
		}
		p, c := nearest(pchain, get), nearest(cchain, get)
		if p == Unset && c == Unset {
			return get(&s.defaults) == On
		}
		return (p == On) != (c == On)
	}
	// Right-to-left and other complex-script text takes its bold and
	// italic from the complex-script properties.
	complex := run.Complex == On || nearest(cchain, func(r *RunProps) Tri { return r.Complex }) == On ||
		nearest(pchain, func(r *RunProps) Tri { return r.Complex }) == On
	bold, italic := func(r *RunProps) Tri { return r.Bold }, func(r *RunProps) Tri { return r.Italic }
	if complex {
		bold, italic = func(r *RunProps) Tri { return r.BoldCS }, func(r *RunProps) Tri { return r.ItalCS }
	}
	f := Format{
		Bold:   toggle(bold),
		Italic: toggle(italic),
		Strike: toggle(func(r *RunProps) Tri { return r.Strike }) || toggle(func(r *RunProps) Tri { return r.DStrike }),
		Hidden: toggle(func(r *RunProps) Tri { return r.Vanish }),
	}
	// The other properties are not toggles: the nearest statement wins.
	f.Vert, f.Font = run.Vert, run.Font
	for _, chain := range [][]*Style{cchain, pchain} {
		for _, c := range chain {
			if f.Vert == VertUnset {
				f.Vert = c.Run.Vert
			}
			if f.Font == "" {
				f.Font = c.Run.Font
			}
			if codeStyleName(c.Name) {
				f.CodeStyle = true
			}
		}
	}
	if f.Font == "" {
		f.Font = s.defaults.Font
	}
	return f
}

// codeStyleName reports whether a style's name says it is for code: "HTML
// Code", "Source Code", "Verbatim Char", "HTML Preformatted" and the like.
func codeStyleName(name string) bool {
	for _, word := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == ' ' || r == '-' || r == '_' }) {
		switch word {
		case "code", "verbatim", "preformatted", "typewriter", "keyboard", "sourcecode":
			return true
		}
	}
	return false
}

// Monospace reports whether a font is one of fixed width, by its name.
func Monospace(font string) bool {
	f := strings.ToLower(font)
	switch f {
	case "courier new", "courier", "consolas", "menlo", "monaco", "lucida console", "lucida sans typewriter",
		"andale mono", "source code pro", "inconsolata", "cascadia code", "fira code", "sf mono", "courier 10 pitch", "monospace":
		return true
	}
	return strings.HasSuffix(f, " mono") || strings.Contains(f, " mono ") || strings.HasSuffix(f, "mono") && len(f) > 4
}
