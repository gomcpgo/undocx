package body

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gomcpgo/undocx/internal/diag"
	"github.com/gomcpgo/undocx/internal/model"
	"github.com/gomcpgo/undocx/internal/numfmt"
	"github.com/gomcpgo/undocx/internal/symbols"
	"github.com/gomcpgo/undocx/internal/wml"
)

// Warning codes for numbering.
const (
	WarnNumbering    = "numbering"
	WarnNumberFormat = "number-format"
)

// lists keeps the counters of the document's lists as the paragraphs go by.
//
// The counters belong to a list definition (w:abstractNum), not to a
// numbering instance (w:num): two instances of one definition count on from
// each other, as Word and LibreOffice both number them. An instance with a
// w:startOverride restarts the levels it names the first time a paragraph
// uses it, and only then.
type lists struct {
	numbering *wml.Numbering
	styles    *wml.Styles
	counters  map[*wml.AbstractNum]*counters
	seen      map[int]bool // numbering instances already used

	missing int            // paragraphs whose numbering is not defined
	formats map[string]int // labels in a number format not supported
}

type counters struct {
	def     *wml.AbstractNum
	count   [wml.Levels]int
	used    [wml.Levels]bool // the level has a number since it last restarted
	pending [wml.Levels]int  // a start override waiting for the level's next use, plus one
}

// item is the numbering of one paragraph.
type item struct {
	level int // from 0
	label string
	kind  model.LabelKind
}

func newLists(n *wml.Numbering, s *wml.Styles) *lists {
	if n == nil {
		n = wml.NoNumbering()
	}
	return &lists{numbering: n, styles: s, counters: map[*wml.AbstractNum]*counters{}, seen: map[int]bool{}, formats: map[string]int{}}
}

// number works out the label of a paragraph and moves the counters on. It
// is called once for every paragraph of the document in order, including
// empty ones: Word gives an empty numbered paragraph its number.
func (l *lists) number(props wml.ParaProps) (item, bool) {
	num, level, ok := l.numPr(props)
	if !ok {
		return item{}, false
	}
	inst := l.numbering.Num(num)
	var def *wml.AbstractNum
	if inst != nil {
		def = l.numbering.Definition(inst, l.styles)
	}
	if def == nil {
		l.missing++
		return item{}, false
	}
	var levels [wml.Levels]wml.Level
	for i := range levels {
		levels[i] = def.Levels[i]
		if o := inst.Overrides[i].Level; o.Defined {
			// The override replaces the level, except that Word ignores
			// its w:start and w:lvlRestart ([MS-OI29500] 17.9.10, 17.9.25).
			o.Start, o.Restart = levels[i].Start, levels[i].Restart
			levels[i] = o
		}
	}
	if !props.Num.HasLevel {
		level = l.styleLevel(props.Style, levels, level)
	}
	if !levels[level].Defined {
		return item{}, false
	}

	c := l.counters[def]
	if c == nil {
		c = &counters{def: def}
		l.counters[def] = c
	}
	if !l.seen[num] {
		l.seen[num] = true
		for i, o := range inst.Overrides {
			if o.HasStart {
				c.used[i], c.pending[i] = false, o.Start+1
			}
		}
	}
	// A level above this one that has no number yet takes its start value,
	// so that an item at level 2 straight after the start of a list reads
	// 1.1, and counts as used: the next item at level 1 is 2.
	for k := range level {
		if !c.used[k] {
			c.use(k, levels)
		}
	}
	c.use(level, levels)
	it := item{level: level, label: l.label(c, levels, level), kind: labelKind(levels[level], level)}
	if props.SectionEnd && def.RestartAfterBreak {
		*c = counters{def: def}
	}
	return it, true
}

// numPr finds the numbering instance and level of a paragraph: from its own
// properties, or else from its style.
func (l *lists) numPr(props wml.ParaProps) (num, level int, ok bool) {
	n := props.Num
	if !n.HasID {
		fromStyle := l.styles.NumPr(l.styleID(props.Style))
		n.ID, n.HasID = fromStyle.ID, fromStyle.HasID
		if !n.HasLevel {
			n.Level = fromStyle.Level
		}
	}
	if !n.HasID || n.ID == 0 {
		// numId 0 switches off numbering that a style would give.
		return 0, 0, false
	}
	return n.ID, min(max(n.Level, 0), wml.Levels-1), true
}

func (l *lists) styleID(id string) string {
	if st := l.styles.Para(id); st != nil {
		return st.ID
	}
	return ""
}

// styleLevel returns the level tied to the paragraph's style by a w:pStyle
// in the list definition, or level when there is none. The standard makes
// this tie decide the level of a paragraph numbered through its style
// (17.9.23); Word writes the same level in the style's own w:ilvl.
func (l *lists) styleLevel(styleID string, levels [wml.Levels]wml.Level, level int) int {
	found := -1
	l.styles.Chain(l.styleID(styleID), func(id string) bool {
		for i, lv := range levels {
			if lv.Defined && lv.Style != "" && lv.Style == id {
				found = i
				return false
			}
		}
		return true
	})
	if found >= 0 {
		return found
	}
	return level
}

// use gives level k its next number, and restarts the levels below it that
// restart when k is used.
func (c *counters) use(k int, levels [wml.Levels]wml.Level) {
	switch {
	case c.used[k]:
		c.count[k]++
	case c.pending[k] > 0:
		c.count[k], c.pending[k] = c.pending[k]-1, 0
	default:
		c.count[k] = levels[k].Start
	}
	c.used[k] = true
	for m := k + 1; m < wml.Levels; m++ {
		if restartsAfter(c.def.Levels[m], m, k) {
			c.used[m] = false
		}
	}
}

// restartsAfter reports whether level m starts again when level k is used.
// By default a level restarts when any level above it is used. With
// w:lvlRestart it restarts when the level it names or any above that is
// used, and with the value 0 never ([MS-OI29500] 17.9.10).
func restartsAfter(lv wml.Level, m, k int) bool {
	switch v := lv.Restart - 1; {
	case lv.Restart == 0 || v > m:
		return k < m
	case v == 0:
		return false
	default:
		return k < v
	}
}

// label fills in the pattern of a level.
func (l *lists) label(c *counters, levels [wml.Levels]wml.Level, level int) string {
	lv := levels[level]
	if lv.NoText || lv.Text == "" {
		return ""
	}
	if lv.Format == "bullet" || lv.Picture {
		return bullet(lv)
	}
	var sb strings.Builder
	text := lv.Text
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch != '%' || i+1 == len(text) || text[i+1] < '1' || text[i+1] > '9' {
			sb.WriteByte(ch)
			continue
		}
		k := int(text[i+1] - '1')
		i++
		if k > level {
			// Word ignores a pattern that refers to a level below its own
			// ([MS-OI29500] 17.9.11).
			return ""
		}
		format, custom := levels[k].Format, levels[k].Custom
		if format == "bullet" {
			// A bullet level has no number: "%1.%2.%3" over a bullet
			// at the second level reads "1..1".
			continue
		}
		if lv.Legal && format != "none" {
			format, custom = "decimal", ""
		}
		s, ok := numfmt.Format(c.count[k], format, custom)
		if !ok {
			l.formats[format]++
		}
		sb.WriteString(s)
	}
	return cleanLabel(sb.String())
}

// bullet returns the glyph of a bullet as a character. A bullet in a symbol
// font is a position in that font; one that has no Unicode equivalent, and a
// picture bullet, become "•".
func bullet(lv wml.Level) string {
	if lv.Picture {
		return "•"
	}
	var sb strings.Builder
	for _, r := range lv.Text {
		pos, inFont := symbols.Position(r)
		switch {
		case inFont && symbols.Font(lv.Font):
			if m, ok := symbols.Map(lv.Font, pos); ok {
				r = m
			} else {
				r = '•'
			}
		case r >= 0xE000 && r <= 0xF8FF:
			r = '•' // a private-use character of a font we cannot name
		}
		sb.WriteRune(r)
	}
	return cleanLabel(sb.String())
}

// cleanLabel makes a label one line of ordinary characters.
func cleanLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r' || r == 0xA0:
			return ' '
		case r < 0x20 || r == utf8.RuneError:
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(clean(s))
}

// labelKind says whether Markdown can show a level's labels with its own
// list syntax: a bullet, or a decimal number of the level alone followed by
// a full stop or a parenthesis.
func labelKind(lv wml.Level, level int) model.LabelKind {
	if lv.Format == "bullet" || lv.Picture {
		return model.LabelBullet
	}
	own := "%" + strconv.Itoa(level+1)
	if lv.Format == "decimal" && !lv.NoText && (lv.Text == own+"." || lv.Text == own+")") {
		return model.LabelNumber
	}
	return model.LabelText
}

func (l *lists) report(log *diag.Log) {
	if l.missing > 0 {
		log.Add(WarnNumbering, "%d paragraphs refer to numbering that the document does not define, and were read without their labels", l.missing)
	}
	for _, f := range sortedKeys(l.formats) {
		log.Add(WarnNumberFormat, "%d list labels use the number format %q, which is not supported, and were written in decimal", l.formats[f], f)
	}
}
