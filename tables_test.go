package undocx_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gomcpgo/undocx"
	"github.com/gomcpgo/undocx/internal/docxtest"
)

var (
	tbl  = docxtest.Tbl
	tr   = docxtest.Tr
	trPr = docxtest.TrPr
	tc   = docxtest.Tc
	tcPr = docxtest.TcPr
)

func text(t *testing.T, d *docxtest.Doc) string {
	t.Helper()
	s, err := mustOpen(t, d.Bytes()).Text(ctx, undocx.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func blocksOf(t *testing.T, doc *undocx.Document) []undocx.Block {
	t.Helper()
	var out []undocx.Block
	for b, err := range doc.Blocks(ctx, undocx.ReadOptions{}) {
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

// Both outputs of a table, for each shape a table can take.
func TestTables(t *testing.T) {
	bullets := abs(0, lvl(0, 1, "bullet", "•", ""), lvl(1, 1, "decimal", "%2.", "")) + num(1, 0, "")
	cases := []struct {
		name, body, md, text string
	}{
		{
			"a plain table: the first row is the header",
			tbl(2, tr(tc("Name"), tc("Role")), tr(tc("Ann"), tc("Lead"))),
			"| Name | Role |\n| --- | --- |\n| Ann | Lead |\n",
			"Name\tRole\nAnn\tLead\n",
		},
		{
			"what would end a cell or a line is escaped, and paragraphs are joined",
			tbl(2, tr(tc("a|b"), tcPr("", p("one"), p("two"), para(r("three"), `<w:r><w:br/></w:r>`, r("four")))),
				tr(tcPr("", para(docxtest.RPr(`<w:rFonts w:ascii="Courier New"/>`, "x|y"))), tcPr("", para(docxtest.RPr(`<w:b/>`, "bold "), r("and *plain*"))))),
			"| a\\|b | one<br>two<br>three<br>four |\n| --- | --- |\n| `x\\|y` | **bold** and \\*plain\\* |\n",
			"a|b\tone two three four\nx|y\tbold and *plain*\n",
		},
		{
			"empty cells keep their place, and a row may be short",
			tbl(3, tr(tc("a"), tc(""), tc("c")), tr(tc(""), tc(""), tc("f")), tr(tc("g"))),
			"| a |  | c |\n| --- | --- | --- |\n|  |  | f |\n| g |  |  |\n",
			"a\t\tc\n\t\tf\ng\n",
		},
		{
			"columns a row leaves empty before and after its cells",
			tbl(3, tr(tc("a1"), tc("b1"), tc("c1")), trPr(`<w:gridBefore w:val="1"/>`, tc("b2"), tc("c2")), trPr(`<w:gridAfter w:val="2"/>`, tc("a3"))),
			"| a1 | b1 | c1 |\n| --- | --- | --- |\n|  | b2 | c2 |\n| a3 |  |  |\n",
			"a1\tb1\tc1\n\tb2\tc2\na3\n",
		},
		{
			"merged cells make an HTML table, with each cell in its column",
			tbl(3, trPr(`<w:tblHeader/>`, tcPr(`<w:vMerge w:val="restart"/>`, p("Region")), tcPr(`<w:gridSpan w:val="2"/>`, p("Sales"))),
				trPr(`<w:tblHeader/>`, tcPr(`<w:vMerge/>`, p("")), tc("Q1"), tc("Q2")),
				tr(tc("North"), tc("10"), tc("20"))),
			"<table>\n<tr><th rowspan=\"2\">Region</th><th colspan=\"2\">Sales</th></tr>\n<tr><th>Q1</th><th>Q2</th></tr>\n<tr><td>North</td><td>10</td><td>20</td></tr>\n</table>\n",
			"Region\tSales\n\tQ1\tQ2\nNorth\t10\t20\n",
		},
		{
			"in HTML, formatting is tags and text is escaped",
			tbl(2, tr(tcPr(`<w:gridSpan w:val="2"/>`, para(docxtest.RPr(`<w:b/><w:i/>`, "a<b & c"), r(" *d*")))), tr(tc("x"), tcPr("", p("one"), p("two")))),
			"<table>\n<tr><td colspan=\"2\"><strong><em>a&lt;b &amp; c</em></strong> *d*</td></tr>\n<tr><td>x</td><td>one<br>two</td></tr>\n</table>\n",
			"a<b & c *d*\nx\tone two\n",
		},
		{
			"an empty column in an HTML table needs a cell, a column covered from above does not",
			tbl(3, tr(tcPr(`<w:vMerge w:val="restart"/>`, p("tall")), tcPr(`<w:gridSpan w:val="2"/>`, p("wide"))),
				trPr(`<w:gridBefore w:val="0"/>`, tcPr(`<w:vMerge/>`, p("")), tc("b2"), tc("c2")),
				trPr(`<w:gridBefore w:val="2"/>`, tc("c3"))),
			"<table>\n<tr><td rowspan=\"2\">tall</td><td colspan=\"2\">wide</td></tr>\n<tr><td>b2</td><td>c2</td></tr>\n<tr><td colspan=\"2\"></td><td>c3</td></tr>\n</table>\n",
			"tall\twide\n\tb2\tc2\n\t\tc3\n",
		},
		{
			"a table in a cell",
			tbl(2, tr(tc("left"), tcPr("", p("right:"), tbl(2, tr(tc("i1"), tc("i2"))), p("after")))),
			"<table>\n<tr><td>left</td><td>right:\n<table>\n<tr><td>i1</td><td>i2</td></tr>\n</table>\nafter</td></tr>\n</table>\n",
			"left\tright: after\ni1\ti2\n",
		},
		{
			"a one-cell table in a cell is a frame, and the outer table stays a pipe table",
			tbl(2, tr(tc("left"), tcPr("", p("right:"), tbl(1, tr(tc("framed"))), p("")))),
			"| left | right:<br>framed |\n| --- | --- |\n",
			"left\tright: framed\n",
		},
		{
			"one cell in every row is a frame around its paragraphs",
			p("Before.") + tbl(1, tr(tcPr("", p("One."), p("Two."))), tr(tc("Three."))) + p("After."),
			"Before.\n\nOne.\n\nTwo.\n\nThree.\n\nAfter.\n",
			"Before.\nOne.\nTwo.\nThree.\nAfter.\n",
		},
		{
			"so is one cell that spans the grid in every row",
			tbl(2, tr(tcPr(`<w:gridSpan w:val="2"/>`, p("One."))), tr(tcPr(`<w:gridSpan w:val="2"/>`, p("Two.")))),
			"One.\n\nTwo.\n",
			"One.\nTwo.\n",
		},
		{
			"so is a table with empty cells beside the text for space",
			tbl(3, tr(tc(""), tcPr("", p("One."), p("Two.")), tc("")), tr(tc(""), tc(""), tc("Three."))),
			"One.\n\nTwo.\n\nThree.\n",
			"One.\nTwo.\nThree.\n",
		},
		{
			"a table with nothing in it is left out, but an empty row of a table is kept",
			tbl(2, tr(tc(""), tc("")), tr(tc(""), tc(""))) + tbl(2, tr(tc("Name"), tc("Date")), tr(tc(""), tc(""))),
			"| Name | Date |\n| --- | --- |\n|  |  |\n",
			"Name\tDate\n",
		},
		{
			"lists in a cell keep their labels, and a number alone in a cell is kept; outside a table it only counts",
			tbl(2, tr(tcPr("", pnum(1, 1, "")), tcPr("", pnum(1, 0, "first"), pnum(1, 0, "second"))), tr(tcPr("", pnum(1, 1, "")), tc("x"))) + pnum(1, 1, "") + pnum(1, 1, "top"),
			"| 1. | • first<br>• second |\n| --- | --- |\n| 1. | x |\n\n3. top\n",
			"1.\t• first • second\n1.\tx\n  3. top\n",
		},
		{
			"a cell continuing a merge that nothing began is a cell of its own",
			tbl(2, tr(tc("a1"), tc("b1")), tr(tcPr(`<w:vMerge/>`, p("orphan")), tc("b2"))),
			"| a1 | b1 |\n| --- | --- |\n| orphan | b2 |\n",
			"a1\tb1\norphan\tb2\n",
		},
		{
			"so is one that does not cover the columns of the cell above",
			tbl(3, tr(tcPr(`<w:gridSpan w:val="2"/><w:vMerge w:val="restart"/>`, p("wide")), tc("c1")), tr(tcPr(`<w:vMerge/>`, p("narrow")), tc("b2"), tc("c2"))),
			"<table>\n<tr><td colspan=\"2\">wide</td><td>c1</td></tr>\n<tr><td>narrow</td><td>b2</td><td>c2</td></tr>\n</table>\n",
			"wide\t\tc1\nnarrow\tb2\tc2\n",
		},
		{
			"a merge down three rows, the last of which has no other cell",
			tbl(2, tr(tcPr(`<w:vMerge w:val="restart"/>`, p("tall")), tc("b1")), tr(tcPr(`<w:vMerge/>`, p("")), tc("b2")), tr(tcPr(`<w:vMerge/>`, p(""))), tr(tc("a4"), tc("b4"))),
			"<table>\n<tr><td rowspan=\"3\">tall</td><td>b1</td></tr>\n<tr><td>b2</td></tr>\n<tr></tr>\n<tr><td>a4</td><td>b4</td></tr>\n</table>\n",
			"tall\tb1\n\tb2\na4\tb4\n",
		},
		{
			"the older horizontal merge joins cells and keeps their text",
			tbl(3, tr(tc("a"), tcPr(`<w:hMerge w:val="restart"/>`, p("start")), tcPr(`<w:hMerge/>`, p("more"))), tr(tc("d"), tc("e"), tc("f"))),
			"<table>\n<tr><td>a</td><td colspan=\"2\">start<br>more</td></tr>\n<tr><td>d</td><td>e</td><td>f</td></tr>\n</table>\n",
			"a\tstart more\nd\te\tf\n",
		},
		{
			"tracked changes: a deleted row and a deleted cell are gone, a tracked merge is made",
			tbl(2, tr(tcPr(`<w:cellMerge w:id="1" w:author="A" w:vMerge="rest"/>`, p("a1")), tc("b1")),
				tr(tcPr(`<w:cellMerge w:id="2" w:author="A" w:vMerge="cont"/>`, p("")), tc("b2")),
				trPr(`<w:del w:id="3" w:author="A"/>`, tc("gone"), tc("gone")),
				tr(tcPr(`<w:cellDel w:id="4" w:author="A"/>`, p("gone")), tc("a4"), tc("b4"))),
			"<table>\n<tr><td rowspan=\"2\">a1</td><td>b1</td></tr>\n<tr><td>b2</td></tr>\n<tr><td>a4</td><td>b4</td></tr>\n</table>\n",
			"a1\tb1\n\tb2\na4\tb4\n",
		},
		{
			"a hidden row whose text is hidden is not shown",
			tbl(2, tr(tc("a1"), tc("b1")), trPr(`<w:hidden/>`, tcPr("", para(docxtest.RPr(`<w:vanish/>`, "secret"))), tcPr("", para(docxtest.RPr(`<w:vanish/>`, "secret")))), tr(tc("a3"), tc("b3"))),
			"| a1 | b1 |\n| --- | --- |\n| a3 | b3 |\n",
			"a1\tb1\na3\tb3\n",
		},
		{
			"wrappers around rows and cells are passed through",
			tbl(2, `<w:sdt><w:sdtPr/><w:sdtContent>`+tr(tc("a1"), `<w:sdt><w:sdtPr/><w:sdtContent>`+tc("b1")+`</w:sdtContent></w:sdt>`)+`</w:sdtContent></w:sdt>`,
				`<w:customXml w:element="x">`+tr(tc("a2"), tc("b2"))+`</w:customXml>`),
			"| a1 | b1 |\n| --- | --- |\n| a2 | b2 |\n",
			"a1\tb1\na2\tb2\n",
		},
		{
			"an unfilled content control around a cell empties the cell and leaves it in place",
			tbl(3, tr(tc("Name"), `<w:sdt><w:sdtPr><w:showingPlcHdr/></w:sdtPr><w:sdtContent>`+tc("Click here")+`</w:sdtContent></w:sdt>`, tc("Date"))),
			"| Name |  | Date |\n| --- | --- | --- |\n",
			"Name\t\tDate\n",
		},
		{
			"a paragraph cannot run on into a table or out of a cell",
			docxtest.PPr(`<w:rPr><w:del w:id="1" w:author="A"/></w:rPr>`, r("before")) + tbl(2, tr(tcPr("", docxtest.PPr(`<w:rPr><w:del w:id="2" w:author="A"/></w:rPr>`, r("a1"))), tc("b1"))) + p("after"),
			"before\n\n| a1 | b1 |\n| --- | --- |\n\nafter\n",
			"before\na1\tb1\nafter\n",
		},
		{
			"a text box anchored in a cell stays in the cell, and a table in a text box is a table",
			tbl(2, tr(tcPr("", para(r("anchor"), `<w:r><w:pict><v:shape><v:textbox><w:txbxContent>`+p("boxed")+`</w:txbxContent></v:textbox></v:shape></w:pict></w:r>`)), tc("b1"))) +
				para(r("outside"), `<w:r><w:pict><v:shape><v:textbox><w:txbxContent>`+tbl(2, tr(tc("x"), tc("y")))+p("")+`</w:txbxContent></v:textbox></v:shape></w:pict></w:r>`),
			"| anchor<br>boxed | b1 |\n| --- | --- |\n\noutside\n\n| x | y |\n| --- | --- |\n",
			"anchor boxed\tb1\noutside\nx\ty\n",
		},
		{
			"rows and cells with no table around them are still read",
			tr(tc("stray"), tc("cells")) + `<w:tbl>` + p("loose") + tr(tc("a"), tc("b"), p("between")) + `</w:tbl>`,
			"stray\n\ncells\n\nloose\n\nbetween\n\n| a | b |\n| --- | --- |\n",
			"stray\ncells\nloose\nbetween\na\tb\n",
		},
	}
	for _, c := range cases {
		d := docxtest.New().Numbering(bullets).Body(c.body, p(""))
		if md := markdown(t, d); md != c.md {
			t.Errorf("%s: Markdown\n%s\nwant\n%s", c.name, md, c.md)
		}
		if got := text(t, d); got != c.text {
			t.Errorf("%s: text\n%q\nwant\n%q", c.name, got, c.text)
		}
	}
}

func TestTableWarnings(t *testing.T) {
	doc := mustOpen(t, docxtest.New().Body(tbl(2,
		tr(tcPr(`<w:vMerge w:val="restart"/>`, p("shown")), tc("b1")),
		tr(tcPr(`<w:vMerge/>`, p("NOTSHOWN")), tc("b2"))), p("")).Bytes())
	md, err := doc.Markdown(ctx, undocx.MarkdownOptions{})
	if err != nil || strings.Contains(md, "NOTSHOWN") || codes(doc) != undocx.WarnMergedCell {
		t.Errorf("%v, warnings %q\n%s", err, codes(doc), md)
	}
}

// A table is one block. Headings inside it are not sections of the
// document; headings inside a frame are.
func TestTableBlocks(t *testing.T) {
	styles := docxtest.NormalStyle + docxtest.Style("H1", "heading 1", "", "", "") + docxtest.Style("H2", "heading 2", "", "", "")
	h1, h2 := func(s string) string { return docxtest.PStyle("H1", s) }, func(s string) string { return docxtest.PStyle("H2", s) }
	doc := mustOpen(t, docxtest.New().Styles(styles).Body(
		h1("Terms"),
		tbl(3, trPr(`<w:tblHeader/>`, tcPr("", h2("Clause")), tc("Text"), tc("Note")), tr(tc("1"), tcPr(`<w:gridSpan w:val="2"/>`, p("Pay.")))),
		p("under Terms"),
		tbl(1, tr(tcPr("", h2("Framed"), p("in the frame")))),
		p("under Framed"),
	).Bytes())
	var got []string
	bs := blocksOf(t, doc)
	for _, b := range bs {
		got = append(got, b.Kind.String()+" <"+strings.Join(b.Path, " > ")+"> "+strings.ReplaceAll(b.Text(), "\n", " / "))
	}
	want := []string{
		"heading <> Terms",
		"table <Terms> Clause\tText\tNote / 1\tPay.",
		"paragraph <Terms> under Terms",
		"heading <Terms> Framed",
		"paragraph <Terms > Framed> in the frame",
		"paragraph <Terms > Framed> under Framed",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	tb := bs[1].Table
	if tb.Columns != 3 || len(tb.Rows) != 2 || !tb.Rows[0].Header || tb.Rows[1].Header || tb.Continued {
		t.Errorf("table: %+v", tb)
	}
	if c := tb.Rows[1].Cells[1]; c.Col != 1 || c.ColSpan != 2 || c.RowSpan != 1 || len(c.Blocks) != 1 {
		t.Errorf("the merged cell: %+v", c)
	}
	if c := tb.Rows[0].Cells[0].Blocks[0]; c.Kind != undocx.Heading || c.Path != nil {
		t.Errorf("a heading in a cell: %+v", c)
	}
	if md := bs[1].Markdown(); !strings.HasPrefix(md, "| **Clause** | Text | Note |\n") && !strings.Contains(md, "<th><strong>Clause</strong></th>") {
		t.Errorf("a heading in a cell is bold:\n%s", md)
	}
	// A caller may cut a table into parts of its own and render them.
	part := undocx.Block{Kind: undocx.Table, Table: &undocx.TableData{Rows: tb.Rows[:1]}}
	if md := part.Markdown(); md != "| **Clause** | Text | Note |\n| --- | --- | --- |" {
		t.Errorf("a part of the table:\n%s", md)
	}
	// Tables made by hand, however odd, are written without a panic.
	for _, td := range []*undocx.TableData{nil, {}, {Rows: []undocx.Row{{}}}, {Columns: 2, Rows: []undocx.Row{{Cells: []undocx.Cell{{Col: 5}, {Col: 1, RowSpan: 3, ColSpan: -1}}}, {Cells: []undocx.Cell{{Blocks: []undocx.Block{{Kind: undocx.Table}}}}}}}} {
		b := undocx.Block{Kind: undocx.Table, Table: td}
		_, _ = b.Markdown(), b.Text()
	}
}

// A long table comes in parts, each starting with the header rows.
func TestLongTable(t *testing.T) {
	build := func(header string, rows int) *undocx.Document {
		var sb strings.Builder
		sb.WriteString(header)
		for i := range rows {
			sb.WriteString(tr(tc(fmt.Sprintf("row %d", i)), tc("x")))
		}
		return mustOpen(t, docxtest.New().Body(tbl(2, sb.String()), p("after")).Bytes())
	}
	doc := build(trPr(`<w:tblHeader/>`, tc("Name"), tc("Value")), 2500)
	bs := blocksOf(t, doc)
	if len(bs) != 4 || bs[3].Text() != "after" {
		t.Fatalf("%d blocks", len(bs))
	}
	rows := 0
	for i, b := range bs[:3] {
		md := b.Markdown()
		if b.Kind != undocx.Table || b.Table.Continued != (i > 0) || !b.Table.Rows[0].Header || !strings.HasPrefix(md, "| Name | Value |\n| --- | --- |\n| row ") {
			t.Errorf("part %d: continued %v\n%.80s", i, b.Table.Continued, md)
		}
		rows += len(b.Table.Rows) - 1
	}
	if rows != 2500 {
		t.Errorf("%d rows in all", rows)
	}
	md, err := doc.Markdown(ctx, undocx.MarkdownOptions{})
	if err != nil || strings.Count(md, "| row ") != 2500 || strings.Count(md, "| Name | Value |") != 3 {
		t.Errorf("%v: %d rows, %d headers", err, strings.Count(md, "| row "), strings.Count(md, "| Name | Value |"))
	}
	// Without a marked header row, a later part has an empty header: its
	// first row is a row like any other.
	bs = blocksOf(t, build("", 1500))
	if len(bs) != 3 || !strings.HasPrefix(bs[0].Markdown(), "| row 0 | x |\n| --- | --- |\n| row 1 | x |") || !strings.HasPrefix(bs[1].Markdown(), "|  |  |\n| --- | --- |\n| row 1000 | x |") {
		t.Errorf("%d blocks\n%.60s\n%.60s", len(bs), bs[0].Markdown(), bs[1].Markdown())
	}
	// A table that ends where a part ends leaves no empty part behind.
	if bs = blocksOf(t, build(trPr(`<w:tblHeader/>`, tc("Name"), tc("Value")), 999)); len(bs) != 2 {
		t.Errorf("a table of exactly one part: %d blocks", len(bs))
	}
	// The output can be cut between the parts.
	md, err = doc.Markdown(ctx, undocx.MarkdownOptions{MaxBytes: 30000})
	if err != nil || strings.Count(md, "| Name | Value |") != 1 || !strings.HasSuffix(md, "-->\n") {
		t.Errorf("%v: cut after %d parts", err, strings.Count(md, "| Name | Value |"))
	}
}

// Damage inside a table keeps the rows read before it.
func TestDamagedTable(t *testing.T) {
	entries := docxtest.New().Entries()
	entries[2].Data = []byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + p("Before.") +
		`<w:tbl>` + tr(tc("a1"), tc("b1")) + `<w:tr>` + tc("a2") + `<w:tc><w:p><w:r><w:t>b2 is cut`)
	doc := mustOpen(t, docxtest.Zip(entries...))
	text, err := doc.Text(ctx, undocx.ReadOptions{})
	if err != nil || text != "Before.\na1\tb1\na2\tb2 is cut\n" || codes(doc) != undocx.WarnDamagedBody {
		t.Errorf("%v, %q, warnings %s", err, text, codes(doc))
	}
}

// Tables must not cost more than their size: not for their length, their
// width, their nesting, or cells that claim to be wide.
func TestTableTime(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	timeIt := func(body string, depth int) (time.Duration, int) {
		doc, err := open(docxtest.New().Body(body, p("")).Bytes(), undocx.Options{Limits: undocx.Limits{MaxDepth: depth}})
		if err != nil {
			t.Fatal(err)
		}
		var md, text bytes.Buffer
		start := time.Now()
		if err := doc.WriteMarkdown(ctx, &md, undocx.MarkdownOptions{}); err != nil {
			t.Fatal(err)
		}
		if err := doc.WriteText(ctx, &text, undocx.ReadOptions{}); err != nil {
			t.Fatal(err)
		}
		return time.Since(start), md.Len() + text.Len()
	}
	long := func(n int, cellProps string) string {
		var sb strings.Builder
		for i := range n {
			sb.WriteString(tr(tcPr(cellProps, p(fmt.Sprintf("row %d", i*7919%100003))), tc(fmt.Sprintf("%x", i*i)), tc("the quick brown fox")))
		}
		return tbl(3, sb.String())
	}
	small, _ := timeIt(long(5000, ""), 0)
	large, _ := timeIt(long(50000, ""), 0)
	if large > 30*small {
		t.Errorf("10 times the rows took %s against %s", large, small)
	}
	t.Logf("5,000 rows: %s; 50,000 rows: %s", small, large)

	// Every row merged with the one above, and every cell as wide as a
	// cell may claim to be.
	small, _ = timeIt(long(5000, `<w:gridSpan w:val="999999"/><w:vMerge/>`), 0)
	large, size := timeIt(long(50000, `<w:gridSpan w:val="999999"/><w:vMerge/>`), 0)
	if large > 30*small {
		t.Errorf("merged: 10 times the rows took %s against %s", large, small)
	}
	t.Logf("merged: 5,000 rows: %s; 50,000 rows: %s, %d bytes out", small, large, size)

	// One row of very many cells, then very many rows of one cell: the
	// short rows are not filled out to the width of the long one.
	var sb strings.Builder
	var cells []string
	for i := range 20000 {
		cells = append(cells, tcPr(`<w:vMerge w:val="restart"/>`, p(fmt.Sprint(i))))
	}
	sb.WriteString(tr(cells...))
	for i := range 20000 {
		sb.WriteString(trPr(`<w:gridBefore w:val="60"/><w:gridAfter w:val="60"/>`, tc(fmt.Sprint(i))))
	}
	wide, size := timeIt(tbl(1, sb.String()), 0)
	if wide > 5*time.Second || size > 20<<20 {
		t.Errorf("a wide row over many short ones: %s, %d bytes out", wide, size)
	}
	t.Logf("20,000 cells in a row, then 20,000 rows: %s, %d bytes out", wide, size)

	// Tables nested as deep as the reader is allowed to go.
	nest := func(n int) string {
		return strings.Repeat(`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>left</w:t></w:r></w:p></w:tc><w:tc>`, n) + p("bottom") +
			strings.Repeat(`</w:tc></w:tr></w:tbl><w:p/>`, n)
	}
	small, _ = timeIt(nest(300), 5000)
	large, size = timeIt(nest(1500), 5000)
	if large > 60*small || size > 4<<20 {
		t.Errorf("nested: 5 times the depth took %s against %s, %d bytes out", large, small, size)
	}
	t.Logf("nested 300 deep: %s; 1,500 deep: %s, %d bytes out", small, large, size)
}

// gridTbl returns a table whose grid columns have the given widths.
func gridTbl(widths []int, rows ...string) string {
	var sb strings.Builder
	sb.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr>`)
	if widths != nil {
		sb.WriteString(`<w:tblGrid>`)
		for _, w := range widths {
			fmt.Fprintf(&sb, `<w:gridCol w:w="%d"/>`, w)
		}
		sb.WriteString(`</w:tblGrid>`)
	}
	return sb.String() + strings.Join(rows, "") + `</w:tbl>`
}

// The grid of a file has more lines than the table has columns. The
// columns written are the ones a reader sees.
func TestTableGrid(t *testing.T) {
	span := func(n int, text string) string {
		return tcPr(fmt.Sprintf(`<w:gridSpan w:val="%d"/>`, n), p(text))
	}
	cases := []struct {
		name, body, md string
	}{
		{
			"grid lines that no cell starts or ends at are no columns",
			gridTbl([]int{1000, 1000, 1000, 1000}, tr(span(2, "a"), span(2, "b")), tr(span(2, "c"), span(2, "d"))),
			"| a | b |\n| --- | --- |\n| c | d |\n",
		},
		{
			"columns a hair wide are folded away, also where rows start a hair apart",
			gridTbl([]int{15, 900, 15, 1500, 15},
				trPr(`<w:gridBefore w:val="1"/>`, span(2, "Name"), span(2, "Value")),
				trPr(`<w:gridAfter w:val="1"/>`, span(2, "first"), span(2, "100"))),
			"| Name | Value |\n| --- | --- |\n| first | 100 |\n",
		},
		{
			"a narrow column that a cell stands in is a column",
			gridTbl([]int{1000, 60, 1000}, tr(tc("a"), tc("x"), tc("b")), tr(span(2, "c"), tc("d"))),
			"<table>\n<tr><td>a</td><td>x</td><td>b</td></tr>\n<tr><td colspan=\"2\">c</td><td>d</td></tr>\n</table>\n",
		},
		{
			"rows cut at places well apart keep their own columns",
			gridTbl([]int{1000, 500, 1000}, tr(span(2, "a"), tc("b")), tr(tc("c"), span(2, "d"))),
			"<table>\n<tr><td colspan=\"2\">a</td><td>b</td></tr>\n<tr><td>c</td><td colspan=\"2\">d</td></tr>\n</table>\n",
		},
		{
			"rows cut at places a hair apart share a column",
			gridTbl([]int{1000, 40, 1000}, tr(span(2, "a"), tc("b")), tr(tc("c"), span(2, "d"))),
			"| a | b |\n| --- | --- |\n| c | d |\n",
		},
		{
			"a cell is merged with the one above it on the columns a reader sees",
			gridTbl([]int{20, 1000, 20, 1000},
				trPr(`<w:gridBefore w:val="1"/>`, tcPr(`<w:gridSpan w:val="2"/><w:vMerge w:val="restart"/>`, p("tall")), tc("b1")),
				tr(tcPr(`<w:gridSpan w:val="2"/><w:vMerge/>`, p("")), span(2, "b2"))),
			"<table>\n<tr><td rowspan=\"2\">tall</td><td>b1</td></tr>\n<tr><td>b2</td></tr>\n</table>\n",
		},
		{
			"without widths nothing is known to be narrow",
			gridTbl(nil, tr(span(2, "a"), tc("b")), tr(tc("c"), span(2, "d"))),
			"<table>\n<tr><td colspan=\"2\">a</td><td>b</td></tr>\n<tr><td>c</td><td colspan=\"2\">d</td></tr>\n</table>\n",
		},
		{
			"nor with widths of nothing",
			gridTbl([]int{0, 0, 0}, tr(span(2, "a"), tc("b")), tr(tc("c"), span(2, "d"))),
			"<table>\n<tr><td colspan=\"2\">a</td><td>b</td></tr>\n<tr><td>c</td><td colspan=\"2\">d</td></tr>\n</table>\n",
		},
		{
			"an indent that every row has is no column",
			gridTbl([]int{700, 1000, 1000}, trPr(`<w:gridBefore w:val="1"/>`, tc("a"), tc("b")), trPr(`<w:gridBefore w:val="1"/>`, tc("c"), tc("d"))),
			"| a | b |\n| --- | --- |\n| c | d |\n",
		},
	}
	for _, c := range cases {
		if md := markdown(t, docxtest.New().Body(c.body, p(""))); md != c.md {
			t.Errorf("%s:\n%s\nwant\n%s", c.name, md, c.md)
		}
	}
}
