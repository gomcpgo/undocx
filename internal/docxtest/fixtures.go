package docxtest

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
)

// Fixture is a small document that exercises one feature, with what a reader
// should make of it.
type Fixture struct {
	// Name is the file name without its extension.
	Name string
	// About says what the document is for, in a line.
	About string
	// Doc builds the document.
	Doc func() *Doc
	// Text is the text Word displays with every tracked change accepted,
	// one paragraph per line: list labels as printed, table cells separated
	// by tabs, note text after the body. Comments, headers and footers are
	// not part of it.
	Text string
	// Must lists strings that the output must contain, and MustNot strings
	// that it must not. Once lists strings that must occur exactly once.
	// All three are compared after white space is collapsed.
	Must, MustNot, Once []string
	// Markdown lists strings that Markdown output must contain, and
	// MarkdownNot strings it must not. An entry may give alternatives
	// separated by " || ".
	Markdown, MarkdownNot []string
}

const (
	revAttrs = ` w:author="Ann Lee" w:date="2026-01-05T10:00:00Z"`

	headingStyles = NormalStyle +
		`<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/></w:rPr></w:style>` +
		`<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:pPr><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/></w:rPr></w:style>`
)

func ins(id, content string) string {
	return `<w:ins w:id="` + id + `"` + revAttrs + `>` + content + `</w:ins>`
}

func del(id, text string) string {
	return `<w:del w:id="` + id + `"` + revAttrs + `><w:r><w:delText xml:space="preserve">` + Esc(text) + `</w:delText></w:r></w:del>`
}

func sdt(sdtPr, content string) string {
	return `<w:sdt><w:sdtPr>` + sdtPr + `</w:sdtPr><w:sdtContent>` + content + `</w:sdtContent></w:sdt>`
}

func sym(font, char string) string {
	return `<w:r><w:sym w:font="` + font + `" w:char="` + char + `"/></w:r>`
}

// textBox returns a run holding a text box the way Word 2010 and later store
// it: as a DrawingML shape, with a VML copy for older readers. A reader that
// takes both branches prints the text twice.
func textBox(id, content string) string {
	box := `<w:txbxContent>` + content + `</w:txbxContent>`
	return `<w:r><mc:AlternateContent><mc:Choice Requires="wps"><w:drawing>` +
		`<wp:anchor distT="0" distB="0" distL="114300" distR="114300" simplePos="0" relativeHeight="251659264" behindDoc="0" locked="0" layoutInCell="1" allowOverlap="1">` +
		`<wp:simplePos x="0" y="0"/><wp:positionH relativeFrom="column"><wp:posOffset>0</wp:posOffset></wp:positionH><wp:positionV relativeFrom="paragraph"><wp:posOffset>0</wp:posOffset></wp:positionV>` +
		`<wp:extent cx="1800000" cy="600000"/><wp:wrapSquare wrapText="bothSides"/><wp:docPr id="` + id + `" name="Text Box ` + id + `"/>` +
		`<a:graphic><a:graphicData uri="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"><wps:wsp><wps:spPr/><wps:txbx>` + box + `</wps:txbx><wps:bodyPr/></wps:wsp></a:graphicData></a:graphic>` +
		`</wp:anchor></w:drawing></mc:Choice><mc:Fallback><w:pict><v:shape style="width:140pt;height:50pt"><v:textbox>` + box + `</v:textbox></v:shape></w:pict></mc:Fallback></mc:AlternateContent></w:r>`
}

func picture(relID, id, alt, title string) string {
	return `<w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="952500" cy="952500"/>` +
		`<wp:docPr id="` + id + `" name="Picture ` + id + `" descr="` + Esc(alt) + `" title="` + Esc(title) + `"/>` +
		`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>` +
		`<pic:nvPicPr><pic:cNvPr id="0" name="image.png"/><pic:cNvPicPr/></pic:nvPicPr>` +
		`<pic:blipFill><a:blip r:embed="` + relID + `"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill>` +
		`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="952500" cy="952500"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr>` +
		`</pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r>`
}

// PNG returns a small image of one colour.
func PNG(c color.Color) []byte {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range 16 {
		img.Set(i%4, i/4, c)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func lines(s ...string) string { return strings.Join(s, "\n") }

// Fixtures returns the feature documents, in a fixed order.
func Fixtures() []Fixture {
	return []Fixture{
		{
			Name:  "headings-localised",
			About: "heading styles whose IDs are not English; the style name and outline level identify them",
			Doc: func() *Doc {
				return New().Styles(NormalStyle+
					Style("berschrift1", "heading 1", "Normal", `<w:outlineLvl w:val="0"/>`, `<w:b/>`)+
					Style("Titre2", "heading 2", "Normal", `<w:outlineLvl w:val="1"/>`, `<w:b/>`)+
					Style("Ttulo3", "heading 3", "Normal", "", `<w:b/>`)+ // no outline level in the file: the name alone says it
					Style("Chapter", "Chapter Head", "berschrift1", "", "")). // inherits the outline level
					Body(PStyle("berschrift1", "Introduction"), P("Opening text."),
						PStyle("Titre2", "Background"), P("More text."),
						PStyle("Ttulo3", "Details"), P("Fine print."),
						PStyle("Chapter", "Second chapter"), P("Closing text."))
			},
			Text:     lines("Introduction", "Opening text.", "Background", "More text.", "Details", "Fine print.", "Second chapter", "Closing text."),
			Markdown: []string{"# Introduction", "## Background", "### Details", "# Second chapter"},
		},
		{
			Name:  "headings-outline",
			About: "an outline level set on the paragraph makes a heading; a style ID that looks like a heading does not",
			Doc: func() *Doc {
				return New().Styles(NormalStyle+
					Style("Heading1", "Body Emphasis", "Normal", "", `<w:b/>`)).
					Body(PPr(`<w:outlineLvl w:val="1"/>`, R("Direct outline")), P("Text under it."),
						PStyle("Heading1", "Only a bold paragraph"),
						PPr(`<w:outlineLvl w:val="9"/>`, R("Body text level")))
			},
			Text:        lines("Direct outline", "Text under it.", "Only a bold paragraph", "Body text level"),
			Markdown:    []string{"## Direct outline"},
			MarkdownNot: []string{"# Only a bold paragraph", "# Body text level"},
		},
		{
			Name:  "headings-numbered",
			About: "headings numbered through their styles",
			Doc: func() *Doc {
				return New().Styles(NormalStyle+
					Style("Heading1", "heading 1", "Normal", `<w:numPr><w:numId w:val="1"/></w:numPr><w:outlineLvl w:val="0"/>`, `<w:b/>`)+
					Style("Heading2", "heading 2", "Normal", `<w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr><w:outlineLvl w:val="1"/>`, `<w:b/>`)).
					Numbering(AbstractNum(0,
						Lvl(0, 1, "decimal", "%1", `<w:pStyle w:val="Heading1"/>`),
						Lvl(1, 1, "decimal", "%1.%2", `<w:pStyle w:val="Heading2"/>`))+Num(1, 0, "")).
					Body(PStyle("Heading1", "Scope"), P("Scope text."), PStyle("Heading2", "Purpose"), PStyle("Heading2", "Audience"),
						PStyle("Heading1", "Terms"), PStyle("Heading2", "Payment"), P("Payment text."))
			},
			Text:     lines("1 Scope", "Scope text.", "1.1 Purpose", "1.2 Audience", "2 Terms", "2.1 Payment", "Payment text."),
			Must:     []string{"1 Scope", "1.2 Audience", "2.1 Payment"},
			Markdown: []string{"# 1 Scope", "## 2.1 Payment"},
		},
		{
			Name:  "list-legal",
			About: "a multilevel list with the labels a contract uses",
			Doc: func() *Doc {
				return New().Numbering(AbstractNum(0,
					Lvl(0, 1, "decimal", "%1.", ""), Lvl(1, 1, "decimal", "%1.%2", ""), Lvl(2, 1, "lowerLetter", "(%3)", ""))+Num(1, 0, "")).
					Body(PNum(1, 0, "Definitions"), PNum(1, 1, "Affiliate"), PNum(1, 1, "Business Day"),
						PNum(1, 2, "in London"), PNum(1, 2, "in New York"),
						P("An interrupting paragraph."),
						PNum(1, 0, "Term"), PNum(1, 1, "Initial term"), PNum(1, 2, "first period"))
			},
			Text: lines("1. Definitions", "1.1 Affiliate", "1.2 Business Day", "(a) in London", "(b) in New York",
				"An interrupting paragraph.", "2. Term", "2.1 Initial term", "(a) first period"),
			Must: []string{"1.2 Business Day", "(b) in New York", "2. Term", "2.1 Initial term", "(a) first period"},
		},
		{
			Name:  "list-restart",
			About: "a list that continues after other text, then a second list that restarts at 1",
			Doc: func() *Doc {
				return New().Numbering(AbstractNum(0, Lvl(0, 1, "decimal", "%1.", ""))+
					Num(1, 0, "")+Num(2, 0, StartOverride(0, 1))).
					Body(PNum(1, 0, "Alpha"), PNum(1, 0, "Beta"), P("Break."), PNum(1, 0, "Gamma"),
						PNum(2, 0, "Delta"), PNum(2, 0, "Epsilon"))
			},
			Text: lines("1. Alpha", "2. Beta", "Break.", "3. Gamma", "1. Delta", "2. Epsilon"),
			Must: []string{"3. Gamma", "1. Delta", "2. Epsilon"},
		},
		{
			Name:  "list-formats",
			About: "number formats and start values",
			Doc: func() *Doc {
				defs := []struct {
					fmt, text string
					start     int
				}{
					{"upperRoman", "Article %1", 4}, {"lowerRoman", "%1)", 1}, {"upperLetter", "%1.", 1},
					{"lowerLetter", "(%1)", 26}, {"decimalZero", "%1.", 9}, {"ordinal", "%1", 1},
					{"cardinalText", "%1", 1}, {"ordinalText", "%1", 1}, {"none", "", 1},
				}
				var num, body strings.Builder
				for i, d := range defs {
					num.WriteString(AbstractNum(i, Lvl(0, d.start, d.fmt, d.text, "")))
				}
				for i, d := range defs {
					num.WriteString(Num(i+1, i, ""))
					body.WriteString(PNum(i+1, 0, d.fmt+" one") + PNum(i+1, 0, d.fmt+" two"))
				}
				return New().Numbering(num.String()).Body(body.String())
			},
			Text: lines("Article IV upperRoman one", "Article V upperRoman two", "i) lowerRoman one", "ii) lowerRoman two",
				"A. upperLetter one", "B. upperLetter two", "(z) lowerLetter one", "(aa) lowerLetter two",
				"09. decimalZero one", "10. decimalZero two", "1st ordinal one", "2nd ordinal two",
				"One cardinalText one", "Two cardinalText two", "First ordinalText one", "Second ordinalText two",
				"none one", "none two"),
			Must: []string{"Article IV upperRoman", "Article V upperRoman", "ii) lowerRoman", "B. upperLetter", "(z) lowerLetter", "(aa) lowerLetter",
				"09. decimalZero", "10. decimalZero", "2nd ordinal", "Two cardinalText", "Second ordinalText"},
		},
		{
			Name:  "list-bullets",
			About: "nested bullets whose glyphs are private-use characters of Symbol and Wingdings",
			Doc: func() *Doc {
				return New().Numbering(AbstractNum(0,
					Lvl(0, 1, "bullet", "", "", `<w:rFonts w:ascii="Symbol" w:hAnsi="Symbol" w:hint="default"/>`),
					Lvl(1, 1, "bullet", "o", "", `<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/>`),
					Lvl(2, 1, "bullet", "", "", `<w:rFonts w:ascii="Wingdings" w:hAnsi="Wingdings" w:hint="default"/>`))+Num(1, 0, "")).
					Body(PNum(1, 0, "Apples"), PNum(1, 1, "Bramley"), PNum(1, 2, "for cooking"), PNum(1, 0, "Pears"))
			},
			Text:    lines("Apples", "Bramley", "for cooking", "Pears"),
			MustNot: []string{"", ""},
		},
		{
			Name:  "list-legal-override",
			About: "w:isLgl: a level that prints the levels above it in decimal whatever their own format",
			Doc: func() *Doc {
				return New().Numbering(AbstractNum(0,
					Lvl(0, 1, "upperRoman", "Article %1.", ""),
					Lvl(1, 1, "decimal", "%1.%2", `<w:isLgl/>`),
					Lvl(2, 1, "decimal", "%1.%2.%3", `<w:isLgl/>`))+Num(1, 0, "")).
					Body(PNum(1, 0, "General"), PNum(1, 1, "Scope"), PNum(1, 1, "Order"),
						PNum(1, 0, "Fees"), PNum(1, 1, "Rates"), PNum(1, 2, "Hourly"))
			},
			Text: lines("Article I. General", "1.1 Scope", "1.2 Order", "Article II. Fees", "2.1 Rates", "2.1.1 Hourly"),
			Must: []string{"Article I. General", "1.2 Order", "Article II. Fees", "2.1 Rates", "2.1.1 Hourly"},
		},
		{
			Name:  "list-from-style",
			About: "numbering that comes from the paragraph style, and one paragraph that switches it off",
			Doc: func() *Doc {
				return New().Styles(NormalStyle+Style("Clause", "Clause", "Normal", `<w:numPr><w:numId w:val="1"/></w:numPr>`, "")).
					Numbering(AbstractNum(0, Lvl(0, 1, "decimal", "%1.", `<w:pStyle w:val="Clause"/>`))+Num(1, 0, "")).
					Body(PStyle("Clause", "First clause"), PStyle("Clause", "Second clause"),
						PPr(`<w:pStyle w:val="Clause"/><w:numPr><w:numId w:val="0"/></w:numPr>`, R("Unnumbered clause")),
						PStyle("Clause", "Third clause"))
			},
			Text: lines("1. First clause", "2. Second clause", "Unnumbered clause", "3. Third clause"),
			Must: []string{"2. Second clause", "3. Third clause"},
		},
		{
			Name:  "list-instances",
			About: "three numbering instances of one list: two count on from each other, and one restarts the count the first time it is used",
			Doc: func() *Doc {
				return New().Numbering(AbstractNum(0, Lvl(0, 1, "decimal", "%1.", ""))+
					Num(1, 0, "")+Num(2, 0, "")+Num(3, 0, StartOverride(0, 1))).
					Body(PNum(1, 0, "Alpha"), PNum(1, 0, "Beta"), P("Text."), PNum(2, 0, "Gamma"),
						PNum(3, 0, "Delta"), PNum(3, 0, "Epsilon"), PNum(1, 0, "Zeta"), PNum(3, 0, "Eta"))
			},
			Text: lines("1. Alpha", "2. Beta", "Text.", "3. Gamma", "1. Delta", "2. Epsilon", "3. Zeta", "4. Eta"),
			Must: []string{"3. Gamma", "1. Delta", "2. Epsilon", "3. Zeta", "4. Eta"},
		},
		{
			Name:  "list-levels",
			About: "a list that starts below its first level, an item that skips a level, and an empty item that still takes its number",
			Doc: func() *Doc {
				return New().Numbering(AbstractNum(0,
					Lvl(0, 1, "decimal", "%1.", ""), Lvl(1, 1, "decimal", "%1.%2.", ""), Lvl(2, 1, "decimal", "%1.%2.%3.", ""))+Num(1, 0, "")).
					Body(PNum(1, 1, "Starts at level two"), PNum(1, 0, "First level"), PNum(1, 2, "Skips a level"),
						PNum(1, 0, ""), PNum(1, 0, "After the empty item"))
			},
			Text: lines("1.1. Starts at level two", "2. First level", "2.1.1. Skips a level", "3.", "4. After the empty item"),
			Must: []string{"1.1. Starts at level two", "2. First level", "2.1.1. Skips a level", "4. After the empty item"},
		},
		{
			Name:  "list-restart-levels",
			About: "w:lvlRestart: a level that never restarts, and one that restarts only after the first level",
			Doc: func() *Doc {
				return New().Numbering(AbstractNum(0,
					Lvl(0, 1, "decimal", "%1.", ""),
					Lvl(1, 1, "lowerLetter", "%2)", `<w:lvlRestart w:val="0"/>`),
					Lvl(2, 1, "lowerRoman", "%3]", `<w:lvlRestart w:val="1"/>`))+Num(1, 0, "")).
					Body(PNum(1, 0, "One"), PNum(1, 1, "Two"), PNum(1, 2, "Three"), PNum(1, 1, "Four"),
						PNum(1, 2, "Five"), PNum(1, 0, "Six"), PNum(1, 1, "Seven"), PNum(1, 2, "Eight"))
			},
			Text: lines("1. One", "a) Two", "i] Three", "b) Four", "ii] Five", "2. Six", "c) Seven", "i] Eight"),
			Must: []string{"b) Four", "ii] Five", "c) Seven", "i] Eight"},
		},
		{
			Name:  "table-simple",
			About: "a plain table with a header row",
			Doc: func() *Doc {
				return New().Body(P("Price list:"), Tbl(3,
					TrPr(`<w:tblHeader/>`, Tc("Item"), Tc("Qty"), Tc("Price")),
					Tr(Tc("Widget"), Tc("2"), Tc("10.00")),
					Tr(Tc("Gadget"), Tc("5"), Tc("7.50"))), P("End of list."))
			},
			Text:     lines("Price list:", "Item\tQty\tPrice", "Widget\t2\t10.00", "Gadget\t5\t7.50", "End of list."),
			Markdown: []string{"| Item | Qty | Price |", "| Widget | 2 | 10.00 |"},
		},
		{
			Name:  "table-merged",
			About: "cells merged across columns and down rows",
			Doc: func() *Doc {
				return New().Body(Tbl(3,
					Tr(TcPr(`<w:vMerge w:val="restart"/>`, P("Region")), TcPr(`<w:gridSpan w:val="2"/>`, P("Sales"))),
					Tr(TcPr(`<w:vMerge/>`, `<w:p/>`), Tc("Q1"), Tc("Q2")),
					Tr(Tc("North"), Tc("10"), Tc("20"))), P("After the table."))
			},
			Text:     lines("Region\tSales", "Q1\tQ2", "North\t10\t20", "After the table."),
			Once:     []string{"Region", "Sales"},
			Markdown: []string{`rowspan="2"`, `colspan="2"`},
		},
		{
			Name:  "table-nested",
			About: "a table inside a table cell",
			Doc: func() *Doc {
				inner := Tbl(2, Tr(Tc("inner A"), Tc("inner B")), Tr(Tc("inner C"), Tc("inner D")))
				return New().Body(Tbl(2,
					Tr(Tc("Outer left"), TcPr("", P("Outer right:"), inner, `<w:p/>`)),
					Tr(Tc("Last left"), Tc("Last right"))), `<w:p/>`)
			},
			Text: lines("Outer left\tOuter right:", "inner A\tinner B", "inner C\tinner D", "Last left\tLast right"),
			Once: []string{"inner A", "inner D", "Last right"},
		},
		{
			Name:  "table-layout",
			About: "a one-cell table used as a box around two paragraphs",
			Doc: func() *Doc {
				return New().Body(P("Before."), Tbl(1, Tr(TcPr("", P("Notice: read this first."), P("Then continue.")))), P("After."))
			},
			Text:        lines("Before.", "Notice: read this first.", "Then continue.", "After."),
			MarkdownNot: []string{"|", "<table"},
		},
		{
			Name:  "notes",
			About: "a footnote and an endnote",
			Doc: func() *Doc {
				ref := func(kind string) string {
					return `<w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:` + kind + `Reference w:id="1"/></w:r>`
				}
				note := func(kind, text string) string {
					return `<w:` + kind + ` w:id="1"><w:p><w:r><w:rPr><w:vertAlign w:val="superscript"/></w:rPr><w:` + kind + `Ref/></w:r>` + R(" "+text) + `</w:p></w:` + kind + `>`
				}
				return New().Footnotes(note("footnote", "See the 2025 schedule.")).Endnotes(note("endnote", "Source: annual report.")).
					Body(Para(R("Rates rise"), ref("footnote"), R(" each year"), ref("endnote"), R(".")), P("Second paragraph."))
			},
			Text:     lines("Rates rise each year.", "Second paragraph.", "See the 2025 schedule.", "Source: annual report."),
			Must:     []string{"See the 2025 schedule.", "Source: annual report."},
			Markdown: []string{"[^1]"},
		},
		{
			Name:  "revisions",
			About: "tracked insertion and deletion, a formatting change, a deleted paragraph mark and a deleted table row",
			Doc: func() *Doc {
				return New().Body(
					Para(R("The fee is "), del("1", "DELETEDFEE"), ins("2", R("250")), R(" dollars.")),
					Para(`<w:r><w:rPr><w:b/><w:rPrChange w:id="3"`+revAttrs+`><w:rPr/></w:rPrChange></w:rPr><w:t>Now bold.</w:t></w:r>`),
					PPr(`<w:rPr><w:del w:id="4"`+revAttrs+`/></w:rPr>`, R("First part ")),
					P("second part."),
					Tbl(1, Tr(Tc("Kept row")), TrPr(`<w:del w:id="5"`+revAttrs+`/>`, `<w:tc><w:p>`+del("6", "DELETEDROW")+`</w:p></w:tc>`)),
					`<w:p/>`)
			},
			Text:    lines("The fee is 250 dollars.", "Now bold.", "First part second part.", "Kept row"),
			Must:    []string{"The fee is 250 dollars."},
			MustNot: []string{"DELETEDFEE", "DELETEDROW"},
		},
		{
			Name:  "moves",
			About: "a sentence moved with change tracking on; it must appear once, where it was moved to",
			Doc: func() *Doc {
				return New().Body(
					Para(`<w:moveFromRangeStart w:id="1"`+revAttrs+` w:name="move1"/>`,
						`<w:moveFrom w:id="2"`+revAttrs+`>`+R("MOVEDSENTENCE. ")+`</w:moveFrom>`, `<w:moveFromRangeEnd w:id="1"/>`, R("Stays here.")),
					Para(R("Before. "), `<w:moveToRangeStart w:id="3"`+revAttrs+` w:name="move1"/>`,
						`<w:moveTo w:id="4"`+revAttrs+`>`+R("MOVEDSENTENCE.")+`</w:moveTo>`, `<w:moveToRangeEnd w:id="3"/>`))
			},
			Text: lines("Stays here.", "Before. MOVEDSENTENCE."),
			Once: []string{"MOVEDSENTENCE"},
		},
		{
			Name:  "comments",
			About: "a comment with a reply, marked resolved",
			Doc: func() *Doc {
				c := func(id, author, paraID, text string) string {
					return `<w:comment w:id="` + id + `" w:author="` + author + `" w:date="2026-02-01T09:30:00Z" w:initials="X"><w:p w14:paraId="` + paraID + `">` + R(text) + `</w:p></w:comment>`
				}
				ref := func(id string) string { return `<w:r><w:commentReference w:id="` + id + `"/></w:r>` }
				return New().Comments(c("0", "Ann Lee", "0000A001", "Please verify this figure.") + c("1", "Raj Patel", "0000A002", "Verified against the ledger.")).
					CommentsExtended(`<w15:commentEx w15:paraId="0000A001" w15:done="1"/><w15:commentEx w15:paraId="0000A002" w15:paraIdParent="0000A001" w15:done="1"/>`).
					Body(Para(R("Revenue grew "), `<w:commentRangeStart w:id="0"/><w:commentRangeStart w:id="1"/>`, R("12 percent"),
						`<w:commentRangeEnd w:id="0"/>`, ref("0"), `<w:commentRangeEnd w:id="1"/>`, ref("1"), R(" last year.")))
			},
			Text: "Revenue grew 12 percent last year.",
			Must: []string{"Please verify this figure.", "Verified against the ledger."},
			Once: []string{"12 percent"},
		},
		{
			Name:  "hyperlinks",
			About: "an external link, an internal link and a HYPERLINK field",
			Doc: func() *Doc {
				d := New()
				id := d.Link("https://example.com/")
				return d.Body(
					Para(R("Visit "), `<w:hyperlink r:id="`+id+`">`+R("Example site")+`</w:hyperlink>`, R(" or see "),
						`<w:hyperlink w:anchor="terms">`+R("the terms")+`</w:hyperlink>`, R(" or "),
						Field(` HYPERLINK "https://example.org/docs" `, R("the docs")), R(".")),
					Para(`<w:bookmarkStart w:id="1" w:name="terms"/>`, R("Terms"), `<w:bookmarkEnd w:id="1"/>`))
			},
			Text:     lines("Visit Example site or see the terms or the docs.", "Terms"),
			MustNot:  []string{"HYPERLINK"},
			Markdown: []string{"[Example site](https://example.com/)", "[the docs](https://example.org/docs)"},
		},
		{
			Name:  "fields",
			About: "simple and complex fields, and a field nested in another's instruction; only cached results are text",
			Doc: func() *Doc {
				nested := `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> IF </w:instrText></w:r>` +
					Field(" DOCPROPERTY Status ", R("NESTEDRESULT")) +
					`<w:r><w:instrText xml:space="preserve"> = "Final" "approved" "draft" \* MERGEFORMAT </w:instrText></w:r>` +
					`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` + R("approved") + `<w:r><w:fldChar w:fldCharType="end"/></w:r>`
				return New().Body(
					Para(R("Page "), `<w:fldSimple w:instr=" PAGE ">`+R("3")+`</w:fldSimple>`, R(" of "), Field(" NUMPAGES ", R("12")),
						R(". See clause "), Field(` REF _Ref100 \r \h `, R("4.2")), R(" dated "), Field(` DATE \@ "d MMMM yyyy" `, R("5 January 2026")), R(".")),
					Para(R("Status: "), nested))
			},
			Text:    lines("Page 3 of 12. See clause 4.2 dated 5 January 2026.", "Status: approved"),
			Must:    []string{"Page 3 of 12", "See clause 4.2", "5 January 2026", "Status: approved"},
			MustNot: []string{"NUMPAGES", "MERGEFORMAT", "_Ref100", "NESTEDRESULT", "DOCPROPERTY", "MMMM"},
		},
		{
			Name:  "toc",
			About: "a generated table of contents, which repeats the headings and is left out by default",
			Doc: func() *Doc {
				entry := func(anchor, title, page string) string {
					return `<w:hyperlink w:anchor="` + anchor + `">` + R(title) + `<w:r><w:tab/></w:r>` + Field(" PAGEREF "+anchor+` \h `, R(page)) + `</w:hyperlink>`
				}
				h := func(id, anchor, title string) string {
					return PPr(`<w:pStyle w:val="Heading1"/>`, `<w:bookmarkStart w:id="`+id+`" w:name="`+anchor+`"/>`, R(title), `<w:bookmarkEnd w:id="`+id+`"/>`)
				}
				return New().Styles(headingStyles+Style("TOC1", "toc 1", "Normal", "", "")).Body(
					PPr(`<w:pStyle w:val="TOC1"/>`, `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve"> TOC \o "1-3" \h \z \u </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r>`,
						entry("_Toc1", "Chapter One", "2")),
					PPr(`<w:pStyle w:val="TOC1"/>`, entry("_Toc2", "Chapter Two", "5")),
					Para(`<w:r><w:fldChar w:fldCharType="end"/></w:r>`),
					h("1", "_Toc1", "Chapter One"), P("Text of one."), h("2", "_Toc2", "Chapter Two"), P("Text of two."))
			},
			Text:    lines("Chapter One", "Text of one.", "Chapter Two", "Text of two."),
			Once:    []string{"Chapter One", "Chapter Two"},
			MustNot: []string{"PAGEREF", "TOC \\o"},
		},
		{
			Name:  "content-controls",
			About: "check boxes, a drop-down list, a filled text control, a date and an unfilled control showing its prompt",
			Doc: func() *Doc {
				box := func(checked, glyph string) string {
					return sdt(`<w14:checkbox><w14:checked w14:val="`+checked+`"/><w14:checkedState w14:val="2612" w14:font="MS Gothic"/><w14:uncheckedState w14:val="2610" w14:font="MS Gothic"/></w14:checkbox>`, R(glyph))
				}
				return New().Body(
					Para(box("1", "☒"), R(" Terms accepted")),
					Para(box("0", "☐"), R(" Newsletter")),
					Para(R("Plan: "), sdt(`<w:dropDownList><w:listItem w:displayText="Gold" w:value="g"/><w:listItem w:displayText="Silver" w:value="s"/></w:dropDownList>`, R("Silver"))),
					Para(R("Name: "), sdt(`<w:text/>`, R("Maria Santos"))),
					Para(R("Signed: "), sdt(`<w:date w:fullDate="2026-03-01T00:00:00Z"><w:dateFormat w:val="d MMMM yyyy"/></w:date>`, R("1 March 2026"))),
					Para(R("Reference: "), sdt(`<w:showingPlcHdr/><w:text/>`, R("Click or tap here to enter text."))))
			},
			Text:    lines("☒ Terms accepted", "☐ Newsletter", "Plan: Silver", "Name: Maria Santos", "Signed: 1 March 2026", "Reference:"),
			Must:    []string{"☒ Terms accepted", "☐ Newsletter", "Plan: Silver", "Maria Santos", "1 March 2026"},
			MustNot: []string{"Gold", "Click or tap"},
		},
		{
			Name:  "text-boxes",
			About: "a text box stored as DrawingML with a VML fallback, and one stored as VML only",
			Doc: func() *Doc {
				return New().Body(
					Para(R("Main text beside the box. "), textBox("1", P("SIDEBARNOTE call 555-0100"))),
					P("Next paragraph."),
					Para(R("Older document. "), `<w:r><w:pict><v:shape style="width:140pt;height:50pt"><v:textbox><w:txbxContent>`+P("LEGACYBOX text")+`</w:txbxContent></v:textbox></v:shape></w:pict></w:r>`),
					P("Last paragraph."))
			},
			Text: lines("Main text beside the box.", "SIDEBARNOTE call 555-0100", "Next paragraph.", "Older document.", "LEGACYBOX text", "Last paragraph."),
			Once: []string{"SIDEBARNOTE", "LEGACYBOX"},
		},
		{
			Name:  "hidden-text",
			About: "text hidden directly and through a character style, and hidden text switched back on",
			Doc: func() *Doc {
				return New().Styles(NormalStyle+CharStyle("Secret", "Secret", "", `<w:vanish/>`)).Body(
					Para(R("Visible start "), RPr(`<w:vanish/>`, "HIDDENRUN "), R("visible end.")),
					Para(R("Before "), RPr(`<w:rStyle w:val="Secret"/>`, "HIDDENSTYLE "), R("after.")),
					Para(RPr(`<w:rStyle w:val="Secret"/><w:vanish w:val="0"/>`, "Shown again.")))
			},
			Text:    lines("Visible start visible end.", "Before after.", "Shown again."),
			Must:    []string{"Visible start visible end.", "Before after.", "Shown again."},
			MustNot: []string{"HIDDENRUN", "HIDDENSTYLE"},
		},
		{
			Name:  "symbols-breaks",
			About: "w:sym, symbol-font runs, tabs, line breaks, and the two special hyphens",
			Doc: func() *Doc {
				return New().Body(
					Para(R("Angle "), sym("Symbol", "F061"), R(" is 90"), sym("Symbol", "F0B0"), R(".")),
					Para(`<w:r><w:t>Name:</w:t><w:tab/><w:t>Value</w:t><w:br/><w:t>second line</w:t></w:r>`),
					Para(`<w:r><w:t>A well</w:t><w:noBreakHyphen/><w:t>known co</w:t><w:softHyphen/><w:t>operative.</w:t></w:r>`),
					Para(RPr(`<w:rFonts w:ascii="Wingdings" w:hAnsi="Wingdings"/>`, ""), R(" next step")),
					Para(R("Before break."), `<w:r><w:br w:type="page"/></w:r>`, R("After break.")))
			},
			Text:    lines("Angle α is 90°.", "Name:\tValue", "second line", "A well-known cooperative.", "→ next step", "Before break.", "After break."),
			Must:    []string{"Angle α is 90°.", "Name: Value second line", "well-known cooperative", "next step"},
			MustNot: []string{"", "", "", "­"},
		},
		{
			Name:  "wrappers",
			About: "text inside every wrapper element that readers are known to drop",
			Doc: func() *Doc {
				return New().Body(
					Para(R("Inline: "),
						`<w:smartTag w:uri="urn:schemas-microsoft-com:office:smarttags" w:element="place">`+R("INSMARTTAG ")+`</w:smartTag>`,
						`<w:customXml w:uri="urn:example" w:element="item">`+R("INCUSTOMXML ")+`</w:customXml>`,
						ins("1", R("ININS ")),
						sdt(`<w:text/>`, R("INSDT ")),
						`<w:hyperlink w:anchor="x">`+ins("2", R("INLINKINS "))+`</w:hyperlink>`,
						`<w:dir w:val="ltr">`+R("INDIR ")+`</w:dir>`,
						`<w:bdo w:val="ltr">`+R("INBDO ")+`</w:bdo>`,
						`<w:fldSimple w:instr=" AUTHOR ">`+R("INFLDSIMPLE")+`</w:fldSimple>`),
					`<w:customXml w:uri="urn:example" w:element="block">`+P("BLOCKCUSTOMXML")+`</w:customXml>`,
					`<w:sdt><w:sdtContent>`+P("BLOCKSDT")+`</w:sdtContent></w:sdt>`,
					Tbl(2,
						`<w:sdt><w:sdtContent>`+Tr(Tc("ROWSDT"), Tc("beside"))+`</w:sdtContent></w:sdt>`,
						`<w:tr><w:sdt><w:sdtContent>`+Tc("CELLSDT")+`</w:sdtContent></w:sdt>`+Tc("plain")+`</w:tr>`),
					`<w:p/>`)
			},
			Text: lines("Inline: INSMARTTAG INCUSTOMXML ININS INSDT INLINKINS INDIR INBDO INFLDSIMPLE", "BLOCKCUSTOMXML", "BLOCKSDT", "ROWSDT\tbeside", "CELLSDT\tplain"),
			Once: []string{"INSMARTTAG", "INCUSTOMXML", "ININS", "INSDT", "INLINKINS", "INDIR", "INBDO", "INFLDSIMPLE", "BLOCKCUSTOMXML", "BLOCKSDT", "ROWSDT", "CELLSDT"},
		},
		{
			Name:  "strict",
			About: "the Strict conformance class: the same elements under other namespace URIs",
			Doc: func() *Doc {
				d := New().Strict()
				id := d.Link("https://example.com/strict")
				return d.Styles(headingStyles).
					Numbering(AbstractNum(0, Lvl(0, 1, "decimal", "%1.", ""), Lvl(1, 1, "lowerLetter", "(%2)", ""))+Num(1, 0, "")).
					Body(PStyle("Heading1", "Strict heading"), P("Strict paragraph."),
						PNum(1, 0, "Strict item"), PNum(1, 1, "Strict sub-item"),
						Para(`<w:hyperlink r:id="`+id+`">`+R("Strict link")+`</w:hyperlink>`),
						Tbl(2, Tr(Tc("Strict cell A"), Tc("Strict cell B"))), `<w:p/>`)
			},
			Text:     lines("Strict heading", "Strict paragraph.", "1. Strict item", "(a) Strict sub-item", "Strict link", "Strict cell A\tStrict cell B"),
			Must:     []string{"Strict heading", "Strict paragraph.", "Strict item", "(a) Strict sub-item", "Strict cell B"},
			Markdown: []string{"# Strict heading", "[Strict link](https://example.com/strict)"},
		},
		{
			Name:  "inline-formatting",
			About: "bold, italic, strike, sub- and superscript and code, with runs split the way Word splits them",
			Doc: func() *Doc {
				return New().Styles(NormalStyle+CharStyle("Strong", "Strong", "", `<w:b/>`)+Style("Loud", "Loud", "Normal", "", `<w:b/>`)).Body(
					Para(R("Plain "), RPr(`<w:b/>`, "bo"), RPr(`<w:b/>`, "ld"), R(" and "), RPr(`<w:i/>`, "italic"), R(" and "), RPr(`<w:strike/>`, "struck"), R(".")),
					Para(R("H"), RPr(`<w:vertAlign w:val="subscript"/>`, "2"), R("O and x"), RPr(`<w:vertAlign w:val="superscript"/>`, "2"), R(".")),
					Para(R("Call "), RPr(`<w:rFonts w:ascii="Courier New" w:hAnsi="Courier New"/>`, "os.Exit(1)"), R(" to stop.")),
					Para(R("A "), RPr(`<w:rStyle w:val="Strong"/>`, "strong phrase"), R(" by style.")),
					Para(RPr(`<w:b/>`, "Edge "), R("case.")),
					PPr(`<w:pStyle w:val="Loud"/>`, RPr(`<w:b w:val="0"/>`, "Quiet words"), R(" in a loud paragraph.")))
			},
			Text:        lines("Plain bold and italic and struck.", "H2O and x2.", "Call os.Exit(1) to stop.", "A strong phrase by style.", "Edge case.", "Quiet words in a loud paragraph."),
			Markdown:    []string{"**bold**", "*italic* || _italic_", "~~struck~~", "H<sub>2</sub>O", "x<sup>2</sup>", "`os.Exit(1)`", "**strong phrase**", "**Edge** case."},
			MarkdownNot: []string{"**Quiet"},
		},
		{
			Name:  "headers-footers",
			About: "a header and a footer, which are left out by default",
			Doc: func() *Doc {
				d := New()
				h := d.Header(P("HEADERTEXT Acme confidential"))
				f := d.Footer(Para(R("FOOTERTEXT page "), `<w:fldSimple w:instr=" PAGE ">`+R("1")+`</w:fldSimple>`))
				return d.SectPr(`<w:sectPr><w:headerReference w:type="default" r:id="` + h + `"/><w:footerReference w:type="default" r:id="` + f + `"/><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>`).
					Body(P("Body paragraph."))
			},
			Text:    "Body paragraph.",
			Must:    []string{"Body paragraph."},
			MustNot: []string{"HEADERTEXT", "FOOTERTEXT"},
		},
		{
			Name:  "image",
			About: "an inline picture with alternative text",
			Doc: func() *Doc {
				d := New()
				id := d.Image("image1.png", PNG(color.RGBA{200, 0, 0, 255}))
				return d.Body(Para(R("Before the picture. "), picture(id, "1", "A red square on white", "Logo"), R(" After the picture.")))
			},
			Text:     "Before the picture. After the picture.",
			Must:     []string{"Before the picture.", "After the picture."},
			Markdown: []string{"A red square on white"},
		},
		{
			Name:  "main-part-renamed",
			About: "the main part is not word/document.xml; the package relationships say where it is",
			Doc: func() *Doc {
				return New().MainName("word/document2.xml").Styles(headingStyles).Body(PStyle("Heading1", "Renamed part"), P("Found through the relationships."))
			},
			Text:     lines("Renamed part", "Found through the relationships."),
			Must:     []string{"Renamed part", "Found through the relationships."},
			Markdown: []string{"# Renamed part"},
		},
		{
			Name:  "form-fields",
			About: "legacy form fields: a text input, two check boxes and a drop-down list",
			Doc: func() *Doc {
				ff := func(data, instr string) string {
					return `<w:r><w:fldChar w:fldCharType="begin"><w:ffData><w:name w:val="f"/><w:enabled/><w:calcOnExit w:val="0"/>` + data + `</w:ffData></w:fldChar></w:r>` +
						`<w:r><w:instrText xml:space="preserve"> ` + instr + ` </w:instrText></w:r>`
				}
				end := `<w:r><w:fldChar w:fldCharType="end"/></w:r>`
				return New().Body(
					Para(R("Name: "), ff(`<w:textInput/>`, "FORMTEXT"), `<w:r><w:fldChar w:fldCharType="separate"/></w:r>`, R("John Smith"), end),
					Para(ff(`<w:checkBox><w:sizeAuto/><w:default w:val="0"/><w:checked/></w:checkBox>`, "FORMCHECKBOX"), end, R(" I agree")),
					Para(ff(`<w:checkBox><w:sizeAuto/><w:default w:val="0"/></w:checkBox>`, "FORMCHECKBOX"), end, R(" Send updates")),
					Para(R("Colour: "), ff(`<w:ddList><w:result w:val="1"/><w:listEntry w:val="Red"/><w:listEntry w:val="Green"/></w:ddList>`, "FORMDROPDOWN"), end))
			},
			Text:    lines("Name: John Smith", "☒ I agree", "☐ Send updates", "Colour: Green"),
			Must:    []string{"Name: John Smith", "☒ I agree", "☐ Send updates", "Colour: Green"},
			MustNot: []string{"FORMTEXT", "FORMCHECKBOX", "FORMDROPDOWN", "Red"},
		},
		{
			Name:  "drop-cap",
			About: "a drop cap, which Word stores as a paragraph of its own",
			Doc: func() *Doc {
				return New().Body(PPr(`<w:framePr w:dropCap="drop" w:lines="3" w:wrap="around" w:vAnchor="text" w:hAnchor="text"/>`, R("O")),
					P("nce upon a time there was a clause."))
			},
			Text: "Once upon a time there was a clause.",
			Must: []string{"Once upon a time"},
		},
		{
			Name:  "scripts",
			About: "right-to-left and East Asian text, which is stored in reading order",
			Doc: func() *Doc {
				return New().Body(
					PPr(`<w:bidi/>`, RPr(`<w:rtl/>`, "مرحبا بالعالم")),
					PPr(`<w:bidi/>`, RPr(`<w:rtl/>`, "שלום עולם")),
					P("こんにちは世界"), P("中文文本测试"), P("한국어 문서"))
			},
			Text: lines("مرحبا بالعالم", "שלום עולם",
				"こんにちは世界", "中文文本测试", "한국어 문서"),
			Must: []string{"مرحبا بالعالم", "שלום עולם", "こんにちは世界", "한국어 문서"},
		},
		{
			Name:  "missing-parts",
			About: "paragraphs that name a style and a list, in a package with no styles or numbering part",
			Doc: func() *Doc {
				return New().Body(PStyle("Heading1", "Orphan heading"), PNum(7, 0, "Orphan item"), P("Plain paragraph."))
			},
			Text: lines("Orphan heading", "Orphan item", "Plain paragraph."),
			Must: []string{"Orphan heading", "Orphan item", "Plain paragraph."},
		},
		{
			Name:  "equation",
			About: "a small Office Math equation",
			Doc: func() *Doc {
				return New().Body(Para(R("So "), `<m:oMath><m:r><m:t>a+b=</m:t></m:r><m:f><m:num><m:r><m:t>c</m:t></m:r></m:num><m:den><m:r><m:t>d</m:t></m:r></m:den></m:f></m:oMath>`, R(" holds.")))
			},
			Text:     "So a+b=c/d holds.",
			Must:     []string{"So", "holds."},
			Markdown: []string{`\frac{c}{d}`},
		},
		{
			Name:  "metadata",
			About: "document properties",
			Doc: func() *Doc {
				return New().
					Core(`<dc:title>Supply Agreement</dc:title><dc:subject>Widgets</dc:subject><dc:creator>Ann Lee</dc:creator><cp:keywords>supply, widgets</cp:keywords>` +
						`<cp:lastModifiedBy>Raj Patel</cp:lastModifiedBy><cp:revision>7</cp:revision>` +
						`<dcterms:created xsi:type="dcterms:W3CDTF">2026-01-02T08:00:00Z</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">2026-02-03T17:30:00Z</dcterms:modified>`).
					App(`<Application>Microsoft Office Word</Application><Pages>4</Pages><Words>1234</Words>`).
					Body(P("Body of the agreement."))
			},
			Text: "Body of the agreement.",
			Must: []string{"Body of the agreement."},
		},
	}
}
