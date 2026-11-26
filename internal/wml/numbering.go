package wml

import (
	"encoding/xml"
	"io"
	"strings"

	"github.com/gomcpgo/undocx/internal/xmlstream"
)

// Levels is the number of levels a list has: w:ilvl runs from 0 to 8.
const Levels = 9

// maxLvlText bounds the pattern of a label that is kept. Word allows 31
// characters after the numbers are filled in.
const maxLvlText = 256

// Level is one level of a list definition (w:lvl).
type Level struct {
	Defined bool
	// Start is the number the level counts from. The standard makes it 0
	// when the definition leaves it out.
	Start int
	// Format is the w:numFmt value: "decimal", "lowerLetter", "bullet" and
	// so on. Custom is the w:format of a "custom" format, such as
	// "001, 002, 003, ...".
	Format, Custom string
	// Text is the pattern of the label: %1 to %9 stand for the numbers of
	// the levels. NoText reports that w:lvlText was missing.
	Text   string
	NoText bool
	// Restart is the w:lvlRestart value plus one, or 0 when it is unset:
	// 1 means the level never restarts, 2 that it restarts after level 1
	// is used (w:ilvl 0), and so on.
	Restart int
	// Legal reports w:isLgl: every number in the label is written in
	// decimal.
	Legal bool
	// Style is the ID of the paragraph style tied to this level.
	Style string
	// Font is the font of the label, which a bullet's glyph is in.
	Font string
	// Picture reports a picture bullet.
	Picture bool
}

// AbstractNum is a list definition (w:abstractNum).
type AbstractNum struct {
	ID     int
	Levels [Levels]Level
	// StyleLink names a numbering style whose definition this one stands
	// for (w:numStyleLink).
	StyleLink string
	// RestartAfterBreak reports that the list starts again in each section
	// (w15:restartNumberingAfterBreak).
	RestartAfterBreak bool
}

// Override changes one level of a list for one numbering instance.
type Override struct {
	// Start, when HasStart, restarts the level at this number the first
	// time the instance is used (w:startOverride).
	Start    int
	HasStart bool
	// Level, when Defined, replaces the level's definition.
	Level Level
}

// Num is a numbering instance (w:num), which paragraphs refer to by its ID.
type Num struct {
	ID        int
	Abstract  int
	Overrides [Levels]Override
}

// Numbering is the numbering part of a document.
type Numbering struct {
	abstracts map[int]*AbstractNum
	nums      map[int]*Num
}

// NoNumbering is the numbering of a document that has none.
func NoNumbering() *Numbering {
	return &Numbering{abstracts: map[int]*AbstractNum{}, nums: map[int]*Num{}}
}

// Num returns the numbering instance with the given ID, or nil.
func (n *Numbering) Num(id int) *Num {
	return n.nums[id]
}

// Abstract returns the list definition with the given ID, or nil.
func (n *Numbering) Abstract(id int) *AbstractNum {
	return n.abstracts[id]
}

// Empty reports whether the part defines no numbering instance.
func (n *Numbering) Empty() bool {
	return len(n.nums) == 0
}

// ReadNumbering reads the numbering part. On an error it returns what was
// read before it, which is still usable. The first definition of an ID
// wins; Word writes each ID once.
func ReadNumbering(x *xmlstream.Reader) (*Numbering, error) {
	n := NoNumbering()
	for {
		tok, err := x.Token()
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, err
		}
		el, ok := tok.(xml.StartElement)
		if !ok || x.Depth() == 1 {
			continue
		}
		if x.Depth() != 2 || el.Name.Space != NS {
			if err := x.Skip(); err != nil {
				return n, err
			}
			continue
		}
		switch el.Name.Local {
		case "abstractNum":
			id, idOK := number(Attr(el, "abstractNumId"))
			a := &AbstractNum{ID: id}
			for _, at := range el.Attr {
				if at.Name.Local == "restartNumberingAfterBreak" {
					a.RestartAfterBreak = at.Value == "1" || at.Value == "true" || at.Value == "on"
				}
			}
			err := readAbstract(x, a)
			if _, dup := n.abstracts[id]; idOK && !dup {
				n.abstracts[id] = a
			}
			if err != nil {
				return n, err
			}
		case "num":
			id, idOK := number(Attr(el, "numId"))
			num := &Num{ID: id, Abstract: -1}
			err := readNum(x, num)
			if _, dup := n.nums[id]; idOK && !dup && num.Abstract >= 0 {
				n.nums[id] = num
			}
			if err != nil {
				return n, err
			}
		default:
			if err := x.Skip(); err != nil {
				return n, err
			}
		}
	}
}

// children calls f for each child element of the element whose start tag
// was read last, then reads to its end. f must read or skip the child.
func children(x *xmlstream.Reader, f func(xml.StartElement) error) error {
	depth := x.Depth()
	for {
		tok, err := x.Token()
		if err != nil {
			return unexpectedEOF(err)
		}
		switch el := tok.(type) {
		case xml.EndElement:
			if x.Depth() < depth {
				return nil
			}
		case xml.StartElement:
			if el.Name.Space != NS {
				err = x.Skip()
			} else {
				err = f(el)
			}
			if err != nil {
				return err
			}
		}
	}
}

func readAbstract(x *xmlstream.Reader, a *AbstractNum) error {
	return children(x, func(el xml.StartElement) error {
		switch el.Name.Local {
		case "lvl":
			ilvl, ok := number(Attr(el, "ilvl"))
			var lvl Level
			err := readLevel(x, &lvl)
			if ok && ilvl < Levels && !a.Levels[ilvl].Defined {
				a.Levels[ilvl] = lvl
			}
			return err
		case "numStyleLink":
			a.StyleLink = Val(el)
		}
		return x.Skip()
	})
}

func readNum(x *xmlstream.Reader, num *Num) error {
	return children(x, func(el xml.StartElement) error {
		switch el.Name.Local {
		case "abstractNumId":
			if id, ok := number(Val(el)); ok {
				num.Abstract = id
			}
		case "lvlOverride":
			ilvl, ok := number(Attr(el, "ilvl"))
			var o Override
			err := children(x, func(c xml.StartElement) error {
				switch c.Name.Local {
				case "startOverride":
					o.Start, o.HasStart = number(Val(c))
				case "lvl":
					return readLevel(x, &o.Level)
				}
				return x.Skip()
			})
			if ok && ilvl < Levels {
				num.Overrides[ilvl] = o
			}
			return err
		}
		return x.Skip()
	})
}

func readLevel(x *xmlstream.Reader, l *Level) error {
	l.Defined, l.NoText, l.Format = true, true, "decimal"
	return children(x, func(el xml.StartElement) error {
		switch el.Name.Local {
		case "start":
			l.Start, _ = number(Val(el))
		case "numFmt":
			l.Format = Val(el)
			if l.Format == "custom" {
				l.Custom = Attr(el, "format")
			}
		case "lvlText":
			l.Text, l.NoText = Val(el), false
			if len(l.Text) > maxLvlText {
				l.Text = strings.ToValidUTF8(l.Text[:maxLvlText], "")
			}
		case "lvlRestart":
			if n, ok := number(Val(el)); ok {
				l.Restart = min(n, Levels) + 1
			}
		case "isLgl":
			l.Legal = Bool(el) == On
		case "pStyle":
			l.Style = Val(el)
		case "lvlPicBulletId":
			l.Picture = true
		case "rPr":
			props, err := ReadRunProps(x)
			l.Font = props.Font
			return err
		}
		return x.Skip()
	})
}

// maxLinks bounds how many numbering styles are followed to find the
// definition a list stands for.
const maxLinks = 8

// Definition returns the list definition a numbering instance uses, after
// following numbering styles (w:numStyleLink), or nil.
func (n *Numbering) Definition(num *Num, styles *Styles) *AbstractNum {
	a := n.abstracts[num.Abstract]
	for i := 0; i < maxLinks && a != nil && a.StyleLink != ""; i++ {
		link := styles.NumPr(a.StyleLink)
		other := n.nums[link.ID]
		if !link.HasID || other == nil {
			break
		}
		next := n.abstracts[other.Abstract]
		if next == nil || next == a {
			break
		}
		a = next
	}
	return a
}
