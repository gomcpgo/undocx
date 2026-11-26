package undocx_test

import (
	"strings"
	"testing"

	"github.com/gomcpgo/undocx"
	"github.com/gomcpgo/undocx/internal/docxtest"
)

var (
	pnum  = docxtest.PNum
	lvl   = docxtest.Lvl
	abs   = docxtest.AbstractNum
	num   = docxtest.Num
	ppr   = docxtest.PPr
	style = docxtest.Style
)

// labels returns each block of the document as "label|text".
func labels(t *testing.T, d *docxtest.Doc) []string {
	t.Helper()
	var out []string
	for b, err := range mustOpen(t, d.Bytes()).Blocks(ctx, undocx.ReadOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b.Label+"|"+undocx.Block{Inlines: b.Inlines}.Text())
	}
	return out
}

func TestNumbering(t *testing.T) {
	decimal3 := abs(0, lvl(0, 1, "decimal", "%1.", ""), lvl(1, 1, "lowerLetter", "%1.%2", ""), lvl(2, 1, "lowerRoman", "%1.%2.%3", ""))
	cases := []struct {
		name string
		doc  *docxtest.Doc
		want []string
	}{
		{
			"a list definition reached through a numbering style",
			docxtest.New().Styles(docxtest.NormalStyle+`<w:style w:type="numbering" w:styleId="Legal"><w:name w:val="Legal"/><w:pPr><w:numPr><w:numId w:val="2"/></w:numPr></w:pPr></w:style>`).
				Numbering(`<w:abstractNum w:abstractNumId="0"><w:styleLink w:val="Legal"/>`+lvl(0, 3, "upperLetter", "%1:", "")+`</w:abstractNum>`+
					`<w:abstractNum w:abstractNumId="1"><w:numStyleLink w:val="Legal"/></w:abstractNum>`+num(1, 1, "")+num(2, 0, "")).
				Body(pnum(1, 0, "x"), pnum(1, 0, "y"), pnum(2, 0, "z")),
			[]string{"C:|x", "D:|y", "E:|z"},
		},
		{
			"an override that replaces a level keeps the count, not the override's start",
			docxtest.New().Numbering(decimal3+num(1, 0, "")+
				num(2, 0, `<w:lvlOverride w:ilvl="0"><w:lvl w:ilvl="0"><w:start w:val="5"/><w:numFmt w:val="upperRoman"/><w:lvlText w:val="[%1]"/></w:lvl></w:lvlOverride>`)).
				Body(pnum(1, 0, "a"), pnum(2, 0, "b"), pnum(2, 0, "c"), pnum(1, 0, "d")),
			[]string{"1.|a", "[II]|b", "[III]|c", "4.|d"},
		},
		{
			"a start override first used before the other instances",
			docxtest.New().Numbering(decimal3+num(1, 0, "")+num(2, 0, docxtest.StartOverride(0, 5))).
				Body(pnum(2, 0, "a"), pnum(1, 0, "b"), pnum(2, 0, "c")),
			[]string{"5.|a", "6.|b", "7.|c"},
		},
		{
			"start left out counts from 0",
			docxtest.New().Numbering(`<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl></w:abstractNum>`+num(1, 0, "")).
				Body(pnum(1, 0, "a"), pnum(1, 0, "b")),
			[]string{"0.|a", "1.|b"},
		},
		{
			"a pattern that refers to a deeper level shows nothing; isLgl makes every number decimal",
			docxtest.New().Numbering(abs(0, lvl(0, 1, "upperRoman", "%1.", ""), lvl(1, 1, "lowerLetter", "%1-%2-%3", ""),
				lvl(2, 1, "upperRoman", "%1.%2.%3", `<w:isLgl/>`))+num(1, 0, "")).
				Body(pnum(1, 0, "a"), pnum(1, 1, "b"), pnum(1, 2, "c"), pnum(1, 2, "d")),
			[]string{"I.|a", "|b", "1.1.1|c", "1.1.2|d"},
		},
		{
			"the level comes from the w:pStyle tie when the style gives no level",
			docxtest.New().Styles(docxtest.NormalStyle+
				style("H1", "heading 1", "Normal", `<w:numPr><w:numId w:val="1"/></w:numPr><w:outlineLvl w:val="0"/>`, "")+
				style("H2", "heading 2", "Normal", `<w:numPr><w:numId w:val="1"/></w:numPr><w:outlineLvl w:val="1"/>`, "")+
				style("H2b", "Sub heading", "H2", "", "")).
				Numbering(abs(0, lvl(0, 1, "decimal", "%1", `<w:pStyle w:val="H1"/>`), lvl(1, 1, "decimal", "%1.%2", `<w:pStyle w:val="H2"/>`))+num(1, 0, "")).
				Body(docxtest.PStyle("H1", "a"), docxtest.PStyle("H2", "b"), docxtest.PStyle("H2b", "c"),
					ppr(`<w:pStyle w:val="H2"/><w:numPr><w:ilvl w:val="0"/></w:numPr>`, r("d")), docxtest.PStyle("H2", "e")),
			[]string{"1|a", "1.1|b", "1.2|c", "2|d", "2.1|e"},
		},
		{
			"a paragraph whose mark is deleted joins the next and takes no number",
			docxtest.New().Numbering(decimal3+num(1, 0, "")).
				Body(pnum(1, 0, "a"),
					ppr(`<w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr><w:rPr><w:del w:id="1" w:author="A" w:date="2026-01-01T00:00:00Z"/></w:rPr>`, r("b ")),
					pnum(1, 0, "c"), pnum(1, 0, "d")),
			[]string{"1.|a", "2.|b c", "3.|d"},
		},
		{
			"bullets in symbol fonts, a picture bullet and a bullet with no glyph",
			docxtest.New().Numbering(abs(0, lvl(0, 1, "bullet", "\uF0B7", "", `<w:rFonts w:ascii="Symbol" w:hAnsi="Symbol"/>`),
				lvl(1, 1, "bullet", "\uF0FC", "", `<w:rFonts w:ascii="Wingdings" w:hAnsi="Wingdings"/>`),
				lvl(2, 1, "bullet", "\uF0B7", `<w:lvlPicBulletId w:val="0"/>`),
				lvl(3, 1, "bullet", "", ""))+num(1, 0, "")).
				Body(pnum(1, 0, "a"), pnum(1, 1, "b"), pnum(1, 2, "c"), pnum(1, 3, "d")),
			[]string{"•|a", "✓|b", "•|c", "|d"},
		},
		{
			"a list that restarts in each section",
			docxtest.New().Numbering(`<w:abstractNum w:abstractNumId="0" w15:restartNumberingAfterBreak="1" xmlns:w15="http://schemas.microsoft.com/office/word/2012/wordml">`+
				lvl(0, 1, "decimal", "%1.", "")+`</w:abstractNum>`+num(1, 0, "")).
				Body(pnum(1, 0, "a"), ppr(`<w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr><w:sectPr/>`, r("b")), pnum(1, 0, "c")),
			[]string{"1.|a", "2.|b", "1.|c"},
		},
		{
			"numbering switched off, undefined, or at a level the list does not define",
			docxtest.New().Numbering(abs(0, lvl(0, 1, "decimal", "%1.", ""))+num(1, 0, "")).
				Body(pnum(1, 0, "a"), pnum(0, 0, "b"), pnum(9, 0, "c"), pnum(1, 4, "d"), pnum(1, 0, "e")),
			[]string{"1.|a", "|b", "|c", "|d", "2.|e"},
		},
	}
	for _, c := range cases {
		got := labels(t, c.doc)
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%s:\ngot  %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestNumberingWarnings(t *testing.T) {
	doc := mustOpen(t, docxtest.New().Numbering(abs(0, lvl(0, 1, "thaiNumbers", "%1.", ""))+num(1, 0, "")).
		Body(pnum(1, 0, "a"), pnum(1, 0, "b"), pnum(4, 0, "c")).Bytes())
	text, err := doc.Text(ctx, undocx.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if text != "1. a\n2. b\nc\n" {
		t.Errorf("text %q", text)
	}
	codes := map[string]string{}
	for _, w := range doc.Warnings() {
		codes[w.Code] = w.Message
	}
	if !strings.Contains(codes[undocx.WarnNumberFormat], `2 list labels use the number format "thaiNumbers"`) {
		t.Errorf("format warning: %q", codes[undocx.WarnNumberFormat])
	}
	if !strings.Contains(codes[undocx.WarnNumbering], "1 paragraphs") {
		t.Errorf("numbering warning: %q", codes[undocx.WarnNumbering])
	}

	// A malformed numbering part is a warning; the text is still read.
	doc = mustOpen(t, docxtest.New().Numbering(abs(0, lvl(0, 1, "decimal", "%1.", ""))+num(1, 0, "")+`<w:num w:numId="2">`).
		Body(pnum(1, 0, "a")).Bytes())
	if text, err := doc.Text(ctx, undocx.ReadOptions{}); err != nil || text != "1. a\n" {
		t.Errorf("malformed numbering: %q, %v", text, err)
	}
	if ws := doc.Warnings(); len(ws) == 0 || ws[0].Code != undocx.WarnNumbering {
		t.Errorf("malformed numbering warnings: %v", ws)
	}
}

func TestListBlocks(t *testing.T) {
	doc := mustOpen(t, docxtest.New().Styles(docxtest.NormalStyle+
		style("H1", "heading 1", "Normal", `<w:numPr><w:numId w:val="2"/></w:numPr><w:outlineLvl w:val="0"/>`, "")).
		Numbering(abs(0, lvl(0, 1, "decimal", "%1.", ""), lvl(1, 1, "lowerLetter", "(%2)", ""))+abs(1, lvl(0, 1, "decimal", "%1", ""))+num(1, 0, "")+num(2, 1, "")).
		Body(docxtest.PStyle("H1", "Terms"), pnum(1, 0, "Pay"), pnum(1, 1, "on time")).Bytes())
	var got []string
	for b, err := range doc.Blocks(ctx, undocx.ReadOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, strings.Join([]string{b.Kind.String(), b.Label, b.Text(), strings.Join(b.Path, " > ")}, "|"))
	}
	want := []string{"heading|1|1 Terms|", "list item|1.|1. Pay|1 Terms", "list item|(a)|(a) on time|1 Terms"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestListMarkdown(t *testing.T) {
	bullets := abs(1, lvl(0, 1, "bullet", "•", ""), lvl(1, 1, "bullet", "o", ""), lvl(2, 1, "bullet", "▪", ""))
	numbers := abs(2, lvl(0, 1, "decimal", "%1.", ""), lvl(1, 1, "decimal", "%2)", ""), lvl(2, 1, "lowerLetter", "(%3)", ""))
	cases := []struct {
		name string
		body []string
		want string
	}{
		{"nested bullets are tight and indented by their marker",
			[]string{pnum(1, 0, "a"), pnum(1, 1, "b"), pnum(1, 2, "c"), pnum(1, 0, "d")},
			"- a\n  - b\n    - c\n- d\n"},
		{"numbers nest under the width of their marker, and a text label stays inside its item",
			[]string{pnum(2, 0, "a"), pnum(2, 1, "b"), pnum(2, 2, "c"), pnum(2, 1, "d"), pnum(2, 0, "e")},
			"1. a\n   1) b\n\n      (a) c\n\n   2) d\n2. e\n"},
		{"a list that restarts straight after another is kept apart",
			[]string{pnum(2, 0, "a"), pnum(2, 0, "b"), pnum(3, 0, "c")},
			"1. a\n2. b\n\n<!-- -->\n\n1. c\n"},
		{"a paragraph ends the list; the list goes on at its number",
			[]string{pnum(2, 0, "a"), p("Between."), pnum(2, 0, "b")},
			"1. a\n\nBetween.\n\n2. b\n"},
		{"a number other than 1 does not follow a bullet on the next line",
			[]string{pnum(1, 0, "a"), pnum(4, 0, "b")},
			"- a\n\n3. b\n"},
		{"a line break inside an item stays in the item",
			[]string{pnum(2, 0, "a"), docxtest.PPr(`<w:numPr><w:ilvl w:val="1"/><w:numId w:val="2"/></w:numPr>`, `<w:r><w:t>one</w:t><w:br/><w:t>two</w:t></w:r>`)},
			"1. a\n   1) one\\\n      two\n"},
		{"a label that looks like Markdown is escaped; text after it is not at a line start",
			[]string{pnum(5, 0, "# not a heading"), pnum(5, 0, "two")},
			"01\\. # not a heading\n\n02\\. two\n"},
	}
	for _, c := range cases {
		d := docxtest.New().Numbering(bullets + numbers +
			abs(3, lvl(0, 1, "decimalZero", "%1.", "")) +
			num(1, 1, "") + num(2, 2, "") + num(3, 2, docxtest.StartOverride(0, 1)) + num(4, 2, docxtest.StartOverride(0, 3)) +
			num(5, 3, "")).
			Body(c.body...)
		if got := markdown(t, d); got != c.want {
			t.Errorf("%s:\ngot\n%s\nwant\n%s", c.name, got, c.want)
		}
	}
}
