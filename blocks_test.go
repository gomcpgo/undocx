package undocx_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gomcpgo/undocx"
	"github.com/gomcpgo/undocx/internal/docxtest"
)

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// failures applies a fixture's checks to the text and Markdown made of it.
func failures(f docxtest.Fixture, text, md string) []string {
	var out []string
	text, mdc := collapse(text), collapse(md)
	for _, s := range f.Must {
		if !strings.Contains(text, collapse(s)) {
			out = append(out, "text lacks "+s)
		}
	}
	for _, s := range f.MustNot {
		if strings.Contains(text, s) || strings.Contains(mdc, s) {
			out = append(out, "has "+s)
		}
	}
	for _, s := range f.Once {
		if n := strings.Count(text, s); n != 1 {
			out = append(out, "text has "+s+" not once")
		}
	}
	for _, s := range f.Markdown {
		found := false
		for _, alt := range strings.Split(s, " || ") {
			found = found || strings.Contains(mdc, alt)
		}
		if !found {
			out = append(out, "Markdown lacks "+s)
		}
	}
	for _, s := range f.MarkdownNot {
		if strings.Contains(mdc, s) {
			out = append(out, "Markdown has "+s)
		}
	}
	return out
}

// later lists the fixtures whose features come with a later version, and
// why. Every other fixture must pass all its checks. A fixture listed here
// that passes is an error too, so the list cannot go stale.
var later = map[string]string{
	"headings-numbered":   "list labels",
	"list-legal":          "list labels",
	"list-restart":        "list labels",
	"list-formats":        "list labels",
	"list-legal-override": "list labels",
	"list-from-style":     "list labels",
	"strict":              "list labels",
	"table-simple":        "tables",
	"table-merged":        "tables",
	"notes":               "footnotes and endnotes",
	"comments":            "comments",
	"form-fields":         "form fields",
	"image":               "images",
	"equation":            "equations",
}

func TestFixtures(t *testing.T) {
	for _, f := range docxtest.Fixtures() {
		doc := mustOpen(t, f.Doc().Bytes())
		text, err := doc.Text(ctx, undocx.ReadOptions{})
		if err != nil {
			t.Errorf("%s: Text: %v", f.Name, err)
		}
		md, err := doc.Markdown(ctx, undocx.MarkdownOptions{})
		if err != nil {
			t.Errorf("%s: Markdown: %v", f.Name, err)
		}
		fails := failures(f, text, md)
		switch why, isLater := later[f.Name]; {
		case isLater && len(fails) == 0:
			t.Errorf("%s passes; take it off the list of fixtures waiting for %s", f.Name, why)
		case !isLater && len(fails) > 0:
			t.Errorf("%s:\n  %s\n--- text\n%s--- Markdown\n%s", f.Name, strings.Join(fails, "\n  "), text, md)
		}
	}
}

func markdown(t *testing.T, d *docxtest.Doc) string {
	t.Helper()
	md, err := mustOpen(t, d.Bytes()).Markdown(ctx, undocx.MarkdownOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return md
}

var (
	p    = docxtest.P
	r    = docxtest.R
	para = docxtest.Para
	fld  = docxtest.Field
)

func TestHeadingPath(t *testing.T) {
	styles := docxtest.NormalStyle +
		docxtest.Style("H1", "heading 1", "", "", "") + docxtest.Style("H2", "heading 2", "", "", "") + docxtest.Style("H3", "heading 3", "", "", "") +
		docxtest.Style("T", "Title", "", "", "")
	doc := mustOpen(t, docxtest.New().Styles(styles).Body(
		docxtest.PStyle("T", "The Agreement"), p("Preamble."),
		docxtest.PStyle("H1", "Terms"), p("a"),
		docxtest.PStyle("H2", "Payment"), p("b"),
		docxtest.PStyle("H3", "Late  fees"), p("c"),
		docxtest.PStyle("H2", "Delivery"), p("d"),
		docxtest.PStyle("H1", "General"), p("e"),
		docxtest.PStyle("H3", "Skipped a level"), p("f"),
	).Bytes())
	var got []string
	for b, err := range doc.Blocks(ctx, undocx.ReadOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, b.Kind.String()+" "+b.Text()+" <"+strings.Join(b.Path, " > ")+">")
	}
	want := []string{
		"title The Agreement <>", "paragraph Preamble. <>",
		"heading Terms <>", "paragraph a <Terms>",
		"heading Payment <Terms>", "paragraph b <Terms > Payment>",
		"heading Late  fees <Terms > Payment>", "paragraph c <Terms > Payment > Late fees>",
		"heading Delivery <Terms>", "paragraph d <Terms > Delivery>",
		"heading General <>", "paragraph e <General>",
		"heading Skipped a level <General>", "paragraph f <General > Skipped a level>",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestStyleFormatting(t *testing.T) {
	styles := `<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri"/></w:rPr></w:rPrDefault></w:docDefaults>` + docxtest.NormalStyle +
		docxtest.Style("Loud", "Loud", "Normal", "", `<w:b/>`) +
		docxtest.Style("Louder", "Louder", "Loud", "", `<w:i/>`) +
		docxtest.Style("Calm", "Calm", "Loud", "", `<w:b w:val="0"/>`) +
		docxtest.Style("Listing", "Source Code", "Normal", "", "") +
		docxtest.Style("Mono", "Mono", "Normal", "", `<w:rFonts w:ascii="Consolas"/>`) +
		docxtest.CharStyle("Strong", "Strong", "", `<w:b/>`) +
		docxtest.CharStyle("Sub", "Sub", "Strong", `<w:vertAlign w:val="subscript"/>`)
	md := markdown(t, docxtest.New().Styles(styles).Body(
		docxtest.PStyle("Louder", "based on a bold style"),
		docxtest.PStyle("Calm", "bold switched off down the chain"),
		// A paragraph style and a character style that both say bold cancel out.
		docxtest.PPr(`<w:pStyle w:val="Loud"/>`, r("loud "), docxtest.RPr(`<w:rStyle w:val="Strong"/>`, "toggled"), r(" loud")),
		// Direct formatting states the result outright.
		docxtest.PPr(`<w:pStyle w:val="Loud"/>`, docxtest.RPr(`<w:rStyle w:val="Strong"/><w:b/>`, "forced")),
		para(r("H"), docxtest.RPr(`<w:rStyle w:val="Sub"/>`, "2"), r("O")),
		docxtest.PStyle("Listing", "x := 1"),
		docxtest.PStyle("Mono", "y := 2"),
		// Right-to-left text takes bold from the complex-script property.
		para(docxtest.RPr(`<w:b/><w:rtl/>`, "plain rtl"), r(" "), docxtest.RPr(`<w:bCs/><w:rtl/>`, "bold rtl")),
		// Text formatted as all capitals stays as it was typed, as it does
		// when copied out of Word.
		para(docxtest.RPr(`<w:caps/>`, "shouted"), r(" and "), docxtest.RPr(`<w:smallCaps/>`, "Small Caps")),
		// The old formatting of a tracked change is not the formatting.
		para(`<w:r><w:rPr><w:rPrChange w:id="1" w:author="a" w:date="2026-01-01T00:00:00Z"><w:rPr><w:b/><w:i/></w:rPr></w:rPrChange></w:rPr><w:t>was bold</w:t></w:r>`),
	))
	want := "***based on a bold style***\n\nbold switched off down the chain\n\n**loud** toggled **loud**\n\n**forced**\n\n" +
		"H**<sub>2</sub>**O\n\n`x := 1`\n\n`y := 2`\n\nplain rtl **bold rtl**\n\nshouted and Small Caps\n\nwas bold\n"
	if md != want {
		t.Errorf("got\n%s\nwant\n%s", md, want)
	}
}

func TestFields(t *testing.T) {
	d := docxtest.New()
	md := markdown(t, d.Body(
		// A field whose result runs over two paragraphs.
		para(r("Before "), `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> QUOTE "x" </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r>`, r("first line")),
		para(r("second line"), `<w:r><w:fldChar w:fldCharType="end"/></w:r>`, r(" after.")),
		// A field with no result at all.
		para(r("Empty "), `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> XE "index entry" </w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`, r("field.")),
		// Links: with a bookmark, to a bookmark only, with a tip, of an unsafe kind.
		para(fld(` HYPERLINK "https://example.com/page" \l "part2" \o "A tip" `, r("deep link"))),
		para(fld(` HYPERLINK \l "Section1" `, r("internal link"))),
		para(fld(` HYPERLINK "javascript:alert(1)" `, r("script link"))),
		para(fld(` hyperlink mailto:someone@example.com `, r("mail link"))),
		// A field that begins in deleted text and ends in kept text, and one
		// that is deleted whole: the count of open fields stays right.
		para(`<w:del w:id="1" w:author="a" w:date="2026-01-01T00:00:00Z"><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:delInstrText> DATE </w:delInstrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:delText>old date</w:delText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:del>`, r("Still here.")),
		p("And the next paragraph."),
	))
	want := "Before first line\n\nsecond line after.\n\nEmpty field.\n\n[deep link](https://example.com/page#part2)\n\ninternal link\n\nscript link\n\n" +
		"[mail link](mailto:someone@example.com)\n\nStill here.\n\nAnd the next paragraph.\n"
	if md != want {
		t.Errorf("got\n%s\nwant\n%s", md, want)
	}

	// The prompts of unfilled content controls can be kept, and leaving
	// them out is reported.
	var controls docxtest.Fixture
	for _, f := range docxtest.Fixtures() {
		if f.Name == "content-controls" {
			controls = f
		}
	}
	doc := mustOpen(t, controls.Doc().Bytes())
	if text, _ := doc.Text(ctx, undocx.ReadOptions{}); strings.Contains(text, "Click or tap") || codes(doc) != undocx.WarnPlaceholders {
		t.Errorf("prompts left out: warnings %s\n%s", codes(doc), text)
	}
	doc = mustOpen(t, controls.Doc().Bytes())
	if text, _ := doc.Text(ctx, undocx.ReadOptions{Placeholders: true}); !strings.Contains(text, "Reference: Click or tap here to enter text.") || codes(doc) != "" {
		t.Errorf("prompts kept: warnings %s\n%s", codes(doc), text)
	}

	// The table of contents can be kept.
	var toc docxtest.Fixture
	for _, f := range docxtest.Fixtures() {
		if f.Name == "toc" {
			toc = f
		}
	}
	text, err := mustOpen(t, toc.Doc().Bytes()).Text(ctx, undocx.ReadOptions{TOC: true})
	if err != nil || strings.Count(text, "Chapter One") != 2 || strings.Contains(text, "PAGEREF") {
		t.Errorf("with the table of contents kept: %v\n%s", err, text)
	}
}

func TestLinkWarnings(t *testing.T) {
	d := docxtest.New()
	js, file, web := d.Link("javascript:alert(1)"), d.Link(`file:///C:/secret.txt`), d.Link("HTTPS://Example.com/")
	rel := d.Link("other.docx")
	doc := mustOpen(t, d.Body(para(
		`<w:hyperlink r:id="`+js+`">`+r("one")+`</w:hyperlink>`, r(" "), `<w:hyperlink r:id="`+file+`">`+r("two")+`</w:hyperlink>`, r(" "),
		`<w:hyperlink r:id="`+web+`">`+r("three")+`</w:hyperlink>`, r(" "), `<w:hyperlink r:id="`+rel+`">`+r("four")+`</w:hyperlink>`, r(" "),
		`<w:hyperlink r:id="rIdMissing">`+r("five")+`</w:hyperlink>`)).Bytes())
	md, err := doc.Markdown(ctx, undocx.MarkdownOptions{})
	if err != nil || md != "one two [three](HTTPS://Example.com/) four five\n" {
		t.Errorf("%v\n%s", err, md)
	}
	if got := codes(doc); got != strings.Repeat(","+undocx.WarnLink, 3)[1:] {
		t.Errorf("warnings: %s", got)
	}
}

func TestSymbols(t *testing.T) {
	doc := mustOpen(t, docxtest.New().Body(
		// Letters in the Symbol font are Greek; so are their private-use twins.
		para(docxtest.RPr(`<w:rFonts w:ascii="Symbol" w:hAnsi="Symbol"/>`, "abg \uF044"), r(" plain abg")),
		para(`<w:r><w:sym w:font="Wingdings" w:char="F0FC"/></w:r>`, r(" done "), `<w:r><w:sym w:font="Webdings" w:char="F022"/></w:r>`,
			`<w:r><w:sym w:font="Arial" w:char="2022"/></w:r>`, `<w:r><w:sym w:font="Symbol" w:char="nothex"/></w:r>`),
	).Bytes())
	text, err := doc.Text(ctx, undocx.ReadOptions{})
	if err != nil || text != "\u03B1\u03B2\u03B3 \u0394 plain abg\n\u2713 done \uFFFD\u2022\n" {
		t.Errorf("%v\n%q", err, text)
	}
	if got := codes(doc); got != undocx.WarnSymbol {
		t.Errorf("warnings: %s", got)
	}
}

func TestTrackedChanges(t *testing.T) {
	rev := ` w:id="1" w:author="a" w:date="2026-01-01T00:00:00Z"`
	md := markdown(t, docxtest.New().Styles(docxtest.NormalStyle+docxtest.Style("H1", "heading 1", "", "", "")).Body(
		// Three paragraphs joined by two deleted paragraph marks; the result
		// takes the style of the last, whose mark survives.
		docxtest.PPr(`<w:rPr><w:del`+rev+`/></w:rPr>`, r("One ")),
		docxtest.PPr(`<w:rPr><w:del`+rev+`/></w:rPr>`, r("two ")),
		docxtest.PStyle("H1", "three"),
		// An inserted paragraph mark changes nothing.
		docxtest.PPr(`<w:rPr><w:ins`+rev+`/></w:rPr>`, r("Inserted paragraph.")),
		// The last paragraph's mark deleted: there is nothing to join it to.
		docxtest.PPr(`<w:rPr><w:del`+rev+`/></w:rPr>`, r("Left over.")),
	))
	if want := "# One two three\n\nInserted paragraph.\n\nLeft over.\n"; md != want {
		t.Errorf("got\n%s\nwant\n%s", md, want)
	}
}

func TestMaxBytes(t *testing.T) {
	d := docxtest.New()
	for i := 0; i < 50; i++ {
		d.Body(p("A paragraph of forty characters, or so.."))
	}
	doc := mustOpen(t, d.Bytes())
	md, err := doc.Markdown(ctx, undocx.MarkdownOptions{MaxBytes: 500})
	if err != nil || len(md) > 500+120 || strings.Count(md, "A paragraph") != 11 || !strings.HasSuffix(md, "-->\n") {
		t.Errorf("%v, %d bytes, %d paragraphs\n%s", err, len(md), strings.Count(md, "A paragraph"), md)
	}
	// The first block alone is too big: it is cut, on a character boundary.
	md, err = mustOpen(t, docxtest.New().Body(p(strings.Repeat("\u00e9t\u00e9 ", 100))).Bytes()).Markdown(ctx, undocx.MarkdownOptions{MaxBytes: 31})
	if err != nil || !strings.HasPrefix(md, "\u00e9t\u00e9 \u00e9t\u00e9") || len(md) > 31+120 || strings.ContainsRune(md, '\uFFFD') {
		t.Errorf("%v\n%q", err, md)
	}
	whole, _ := doc.Markdown(ctx, undocx.MarkdownOptions{})
	if strings.Contains(whole, "<!--") || strings.Count(whole, "A paragraph") != 50 {
		t.Errorf("without a limit: %d paragraphs", strings.Count(whole, "A paragraph"))
	}
}

func TestStopEarly(t *testing.T) {
	d := docxtest.New()
	for i := 0; i < 1000; i++ {
		d.Body(p("paragraph"))
	}
	doc := mustOpen(t, d.Bytes())
	n := 0
	for _, err := range doc.Blocks(ctx, undocx.ReadOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
		if n++; n == 3 {
			break
		}
	}
	// The document can be read again, from the start.
	text, err := doc.Text(ctx, undocx.ReadOptions{})
	if err != nil || strings.Count(text, "paragraph") != 1000 {
		t.Errorf("second reading: %v, %d paragraphs", err, strings.Count(text, "paragraph"))
	}

	c, cancel := context.WithCancel(ctx)
	defer cancel()
	n = 0
	var last error
	for _, err := range doc.Blocks(c, undocx.ReadOptions{}) {
		last = err
		if n++; n == 2 {
			cancel()
		}
	}
	if !errors.Is(last, context.Canceled) || n >= 1000 {
		t.Errorf("cancelled after 2 blocks: %d blocks, last error %v", n, last)
	}
}

func TestDamagedBody(t *testing.T) {
	body := func(xml string) []byte {
		entries := docxtest.New().Entries()
		entries[2].Data = []byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + xml)
		return docxtest.Zip(entries...)
	}
	// Cut in the middle of the third paragraph: everything up to the cut
	// is read.
	doc := mustOpen(t, body(p("One.")+p("Two.")+`<w:p><w:r><w:t>Three </w:t></w:r><w:r><w:t>is cut`))
	text, err := doc.Text(ctx, undocx.ReadOptions{})
	if err != nil || text != "One.\nTwo.\nThree is cut\n" || codes(doc) != undocx.WarnDamagedBody {
		t.Errorf("cut short: %v, %q, warnings %s", err, text, codes(doc))
	}
	// Malformed before any text: an error.
	doc = mustOpen(t, body(`<w:p><oops`))
	if _, err := doc.Text(ctx, undocx.ReadOptions{}); !errors.Is(err, undocx.ErrDamaged) {
		t.Errorf("damaged from the start: %v", err)
	}
	// An entity of the document's own is malformed XML.
	doc = mustOpen(t, body(p("Kept.")+`<w:p><w:r><w:t>&mine;</w:t></w:r></w:p>`+p("Lost.")+`</w:body></w:document>`))
	text, err = doc.Text(ctx, undocx.ReadOptions{})
	if err != nil || text != "Kept.\n" || codes(doc) != undocx.WarnDamagedBody {
		t.Errorf("custom entity: %v, %q, warnings %s", err, text, codes(doc))
	}
}

func TestBodyLimits(t *testing.T) {
	// Tables nested far deeper than any real document: a limit, not a hang
	// and not a stack overflow.
	deep := strings.Repeat(`<w:tbl><w:tr><w:tc>`, 200) + p("bottom") + strings.Repeat(`</w:tc></w:tr></w:tbl>`, 200)
	doc := mustOpen(t, docxtest.New().Body(p("top"), deep).Bytes())
	var lim *undocx.LimitError
	text, err := doc.Text(ctx, undocx.ReadOptions{})
	if !errors.As(err, &lim) || text != "top\n" {
		t.Errorf("deep tables: %v, %q", err, text)
	}
	// With the limit raised the same document reads to the bottom.
	raised, err := open(docxtest.New().Body(p("top"), deep).Bytes(), undocx.Options{Limits: undocx.Limits{MaxDepth: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	if text, err := raised.Text(ctx, undocx.ReadOptions{}); err != nil || text != "top\nbottom\n" {
		t.Errorf("deep tables with the limit raised: %v, %q", err, text)
	}
	// Fields nested past the cap do not hide what follows them.
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		sb.WriteString(`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText> IF </w:instrText></w:r>`)
	}
	for i := 0; i < 100; i++ {
		sb.WriteString(`<w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r>`)
	}
	if md := markdown(t, docxtest.New().Body(para(sb.String()), p("after the fields"))); md != "after the fields\n" {
		t.Errorf("nested fields: %q", md)
	}
}

// Time must grow in step with the document, not faster.
func TestLinearTime(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	build := func(n int) []byte {
		var sb strings.Builder
		for i := 0; i < n; i++ {
			// Each paragraph differs, as in a real document. Identical
			// ones compress so well that the part trips the limit on
			// compression ratio, which is what that limit is for.
			sb.WriteString(para(r(fmt.Sprintf("Clause %d: the quick brown fox ", i*7919%100003)), docxtest.RPr(`<w:b/>`, "jumps over"), r(fmt.Sprintf(" the lazy dog %x times.", i*i))))
		}
		return docxtest.New().Body(sb.String()).Bytes()
	}
	timeIt := func(data []byte) time.Duration {
		best := time.Hour
		for i := 0; i < 3; i++ {
			doc := mustOpen(t, data)
			start := time.Now()
			if err := doc.WriteMarkdown(ctx, &bytes.Buffer{}, undocx.MarkdownOptions{}); err != nil {
				t.Fatal(err)
			}
			best = min(best, time.Since(start))
		}
		return best
	}
	small, large := timeIt(build(5000)), timeIt(build(50000))
	if large > 30*small {
		t.Errorf("10 times the document took %s against %s: more than 30 times as long", large, small)
	}
	t.Logf("5,000 paragraphs: %s; 50,000 paragraphs: %s", small, large)
}

func FuzzBody(f *testing.F) {
	for _, fx := range docxtest.Fixtures() {
		f.Add(fx.Doc().Document(), []byte(nil))
	}
	styled := docxtest.New().Styles(docxtest.NormalStyle + docxtest.Style("H1", "heading 1", "Normal", "", `<w:b/>`))
	entries := styled.Entries()
	f.Add([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr><w:r><w:t>x</w:t></w:r></w:p></w:body></w:document>`), entries[4].Data)
	f.Fuzz(func(t *testing.T, document, styles []byte) {
		e := append([]docxtest.Entry(nil), entries...)
		e[2].Data = document
		if styles != nil {
			e[4].Data = styles
		}
		doc, err := open(docxtest.Zip(e...), undocx.Options{Limits: undocx.Limits{MaxDepth: 64, MaxTokenBytes: 1 << 16, MaxPartBytes: 1 << 20}})
		if err != nil {
			return
		}
		md, err := doc.Markdown(ctx, undocx.MarkdownOptions{})
		text, err2 := doc.Text(ctx, undocx.ReadOptions{TOC: true})
		if err == nil && err2 == nil && strings.TrimSpace(md) == "" && strings.TrimSpace(text) != "" {
			// Text shows the table of contents too, so it may have more;
			// but it cannot have text where Markdown has none at all
			// unless a table of contents is all there is.
			if md2, _ := doc.Markdown(ctx, undocx.MarkdownOptions{ReadOptions: undocx.ReadOptions{TOC: true}}); strings.TrimSpace(md2) == "" {
				t.Fatalf("text %q but no Markdown", text)
			}
		}
		if !strings.Contains(md, "\uFFFD") && strings.ContainsRune(text, 0) {
			t.Fatalf("a NUL in the text")
		}
	})
}
