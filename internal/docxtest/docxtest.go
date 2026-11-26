// Package docxtest builds Word documents in memory for tests.
//
// A test says what goes in each part as WordprocessingML, written by hand
// from ECMA-376, and Doc wraps it in the root elements, relationships and
// content types that make a package. Nothing here comes from a file written by
// Word or any other program, so the fixtures can be published with the code.
//
// Markup is given as strings with the usual prefixes (w, r, mc, wp, a, pic,
// wps, v, w14, w15, m). The root element of each part declares them all. The
// helper functions P, R, Tbl and so on cover the common cases; anything else
// is written out in full.
package docxtest

import (
	"fmt"
	"sort"
	"strings"
)

// Namespaces of the Transitional conformance class, and the Strict ones that
// replace some of them.
const (
	nsW   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsR   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	nsA   = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsWP  = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	nsPic = "http://schemas.openxmlformats.org/drawingml/2006/picture"
	nsM   = "http://schemas.openxmlformats.org/officeDocument/2006/math"

	strictW   = "http://purl.oclc.org/ooxml/wordprocessingml/main"
	strictR   = "http://purl.oclc.org/ooxml/officeDocument/relationships"
	strictA   = "http://purl.oclc.org/ooxml/drawingml/main"
	strictWP  = "http://purl.oclc.org/ooxml/drawingml/wordprocessingDrawing"
	strictPic = "http://purl.oclc.org/ooxml/drawingml/picture"
	strictM   = "http://purl.oclc.org/ooxml/officeDocument/math"

	nsMC  = "http://schemas.openxmlformats.org/markup-compatibility/2006"
	nsWPS = "http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
	nsV   = "urn:schemas-microsoft-com:vml"
	nsO   = "urn:schemas-microsoft-com:office:office"
	nsW14 = "http://schemas.microsoft.com/office/word/2010/wordml"
	nsW15 = "http://schemas.microsoft.com/office/word/2012/wordml"

	ctMain          = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
	ctMacro         = "application/vnd.ms-word.document.macroEnabled.main+xml"
	ctTemplate      = "application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml"
	ctMacroTemplate = "application/vnd.ms-word.template.macroEnabledTemplate.main+xml"
	ctWML           = "application/vnd.openxmlformats-officedocument.wordprocessingml."
)

// Doc is a Word document under construction. The zero value is not usable;
// call New.
type Doc struct {
	strict   bool
	mainName string
	mainType string
	body     strings.Builder
	sectPr   string
	parts    []part
	rels     []rel // relationships of the main document part
	pkgRels  []rel // relationships of the package
	exts     map[string]string
	nextRel  int
}

type part struct {
	name        string // without a leading slash
	contentType string
	data        []byte
}

type rel struct {
	id, typ, target string
	external        bool
}

// New returns an empty document: a main part with no paragraphs.
func New() *Doc {
	return &Doc{
		mainName: "word/document.xml",
		mainType: ctMain,
		sectPr:   `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/></w:sectPr>`,
		exts:     map[string]string{},
	}
}

// Strict makes the document use the namespaces of the Strict conformance
// class, for the elements and for the relationship types.
func (d *Doc) Strict() *Doc {
	d.strict = true
	return d
}

// MainName stores the main part under another name than word/document.xml.
// A reader has to find it through the package relationships.
func (d *Doc) MainName(name string) *Doc {
	d.mainName = name
	return d
}

// MacroEnabled gives the main part the content type of a .docm file.
func (d *Doc) MacroEnabled() *Doc {
	d.mainType = ctMacro
	return d
}

// Template gives the main part the content type of a .dotx file.
func (d *Doc) Template() *Doc {
	d.mainType = ctTemplate
	return d
}

// MacroEnabledTemplate gives the main part the content type of a .dotm file.
func (d *Doc) MacroEnabledTemplate() *Doc {
	d.mainType = ctMacroTemplate
	return d
}

// Body appends block-level markup (paragraphs, tables, content controls) to
// the document body.
func (d *Doc) Body(xml ...string) *Doc {
	for _, x := range xml {
		d.body.WriteString(x)
	}
	return d
}

// SectPr replaces the final section properties. Pass "" to leave them out.
func (d *Doc) SectPr(xml string) *Doc {
	d.sectPr = xml
	return d
}

// Styles adds the styles part. inner is the content of w:styles.
func (d *Doc) Styles(inner string) *Doc {
	return d.wmlPart("styles.xml", "styles", "styles", inner)
}

// Numbering adds the numbering part. inner is the content of w:numbering.
func (d *Doc) Numbering(inner string) *Doc {
	return d.wmlPart("numbering.xml", "numbering", "numbering", inner)
}

// Settings adds the settings part. inner is the content of w:settings.
func (d *Doc) Settings(inner string) *Doc {
	return d.wmlPart("settings.xml", "settings", "settings", inner)
}

// Footnotes adds the footnotes part. inner is the content of w:footnotes
// after the two separator notes, which are added here.
func (d *Doc) Footnotes(inner string) *Doc {
	return d.wmlPart("footnotes.xml", "footnotes", "footnotes", separators("footnote")+inner)
}

// Endnotes adds the endnotes part, in the same way as Footnotes.
func (d *Doc) Endnotes(inner string) *Doc {
	return d.wmlPart("endnotes.xml", "endnotes", "endnotes", separators("endnote")+inner)
}

// Comments adds the comments part. inner is the content of w:comments.
func (d *Doc) Comments(inner string) *Doc {
	return d.wmlPart("comments.xml", "comments", "comments", inner)
}

// CommentsExtended adds the part that holds comment threading and the
// resolved state. inner is the content of w15:commentsEx.
func (d *Doc) CommentsExtended(inner string) *Doc {
	data := d.root("w15:commentsEx", inner)
	d.parts = append(d.parts, part{"word/commentsExtended.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.commentsExtended+xml", data})
	d.addRel("http://schemas.microsoft.com/office/2011/relationships/commentsExtended", "commentsExtended.xml", false)
	return d
}

// Header adds a header part and returns its relationship ID, for use in a
// w:headerReference. inner is the content of w:hdr.
func (d *Doc) Header(inner string) string {
	n := d.count("word/header") + 1
	name := fmt.Sprintf("header%d.xml", n)
	d.parts = append(d.parts, part{"word/" + name, ctWML + "header+xml", d.root("w:hdr", inner)})
	return d.addRel(d.relType("header"), name, false)
}

// Footer adds a footer part and returns its relationship ID. inner is the
// content of w:ftr.
func (d *Doc) Footer(inner string) string {
	n := d.count("word/footer") + 1
	name := fmt.Sprintf("footer%d.xml", n)
	d.parts = append(d.parts, part{"word/" + name, ctWML + "footer+xml", d.root("w:ftr", inner)})
	return d.addRel(d.relType("footer"), name, false)
}

// Link adds an external hyperlink relationship and returns its ID, for use in
// w:hyperlink r:id.
func (d *Doc) Link(url string) string {
	return d.addRel(d.relType("hyperlink"), url, true)
}

// Image stores data as word/media/name and returns its relationship ID, for
// use in a:blip r:embed. The content type follows the extension of name.
func (d *Doc) Image(name string, data []byte) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		ext := strings.ToLower(name[i+1:])
		types := map[string]string{"png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif", "svg": "image/svg+xml", "emf": "image/x-emf", "wmf": "image/x-wmf"}
		if t, ok := types[ext]; ok {
			d.exts[ext] = t
		}
	}
	d.parts = append(d.parts, part{"word/media/" + name, "", data})
	return d.addRel(d.relType("image"), "media/"+name, false)
}

// LinkedImage adds a relationship to an image outside the package and returns
// its ID. A reader must report such an image and never fetch it.
func (d *Doc) LinkedImage(url string) string {
	return d.addRel(d.relType("image"), url, true)
}

// Rel adds a relationship from the main part and returns its ID. typ is the
// full relationship type URI.
func (d *Doc) Rel(typ, target string, external bool) string {
	return d.addRel(typ, target, external)
}

// Part adds any other part. name has no leading slash. With an empty
// contentType the part relies on a default for its extension.
func (d *Doc) Part(name, contentType string, data []byte) *Doc {
	d.parts = append(d.parts, part{name, contentType, data})
	return d
}

// Core adds the core properties part. inner is the content of
// cp:coreProperties, with the prefixes cp, dc, dcterms and xsi declared.
func (d *Doc) Core(inner string) *Doc {
	data := []byte(xmlDecl + `<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` + inner + `</cp:coreProperties>`)
	d.parts = append(d.parts, part{"docProps/core.xml", "application/vnd.openxmlformats-package.core-properties+xml", data})
	d.pkgRels = append(d.pkgRels, rel{typ: "http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties", target: "docProps/core.xml"})
	return d
}

// App adds the extended properties part. inner is the content of Properties.
func (d *Doc) App(inner string) *Doc {
	ns := "http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"
	if d.strict {
		ns = "http://purl.oclc.org/ooxml/officeDocument/extendedProperties"
	}
	data := []byte(xmlDecl + `<Properties xmlns="` + ns + `">` + inner + `</Properties>`)
	d.parts = append(d.parts, part{"docProps/app.xml", "application/vnd.openxmlformats-officedocument.extended-properties+xml", data})
	d.pkgRels = append(d.pkgRels, rel{typ: d.relType("extended-properties"), target: "docProps/app.xml"})
	return d
}

// Document returns the main part as it will be stored.
func (d *Doc) Document() []byte {
	return d.root("w:document", "<w:body>"+d.body.String()+d.sectPr+"</w:body>")
}

// Bytes returns the finished package. The result is the same on every call.
func (d *Doc) Bytes() []byte {
	return Zip(d.Entries()...)
}

// Entries returns the entries of the package in the order they are stored.
// A test that needs a package that is wrong in some way takes them, changes
// them and writes them with Zip.
func (d *Doc) Entries() []Entry {
	var entries []Entry
	add := func(name string, data []byte) {
		entries = append(entries, Entry{Name: name, Data: data})
	}

	var ct strings.Builder
	ct.WriteString(xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	ct.WriteString(`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/>`)
	exts := make([]string, 0, len(d.exts))
	for e := range d.exts {
		exts = append(exts, e)
	}
	sort.Strings(exts)
	for _, e := range exts {
		fmt.Fprintf(&ct, `<Default Extension="%s" ContentType="%s"/>`, e, d.exts[e])
	}
	fmt.Fprintf(&ct, `<Override PartName="/%s" ContentType="%s"/>`, d.mainName, d.mainType)
	for _, p := range d.parts {
		if p.contentType != "" {
			fmt.Fprintf(&ct, `<Override PartName="/%s" ContentType="%s"/>`, p.name, p.contentType)
		}
	}
	ct.WriteString(`</Types>`)
	add("[Content_Types].xml", []byte(ct.String()))

	pkg := append([]rel{{typ: d.relType("officeDocument"), target: d.mainName}}, d.pkgRels...)
	for i := range pkg {
		pkg[i].id = fmt.Sprintf("rId%d", i+1)
	}
	add("_rels/.rels", relsXML(pkg))

	add(d.mainName, d.Document())
	if len(d.rels) > 0 {
		dir, file := "", d.mainName
		if i := strings.LastIndexByte(d.mainName, '/'); i >= 0 {
			dir, file = d.mainName[:i+1], d.mainName[i+1:]
		}
		add(dir+"_rels/"+file+".rels", relsXML(d.rels))
	}
	for _, p := range d.parts {
		add(p.name, p.data)
	}
	return entries
}

// MainType sets the content type of the main part, for packages that are
// some other kind of file or that say nothing useful.
func (d *Doc) MainType(contentType string) *Doc {
	d.mainType = contentType
	return d
}

const xmlDecl = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

// root wraps inner in a root element that declares every prefix the fixtures
// use.
func (d *Doc) root(name, inner string) []byte {
	w, r, a, wp, pic, m := nsW, nsR, nsA, nsWP, nsPic, nsM
	if d.strict {
		w, r, a, wp, pic, m = strictW, strictR, strictA, strictWP, strictPic, strictM
	}
	var sb strings.Builder
	sb.WriteString(xmlDecl)
	fmt.Fprintf(&sb, `<%s xmlns:w="%s" xmlns:r="%s" xmlns:a="%s" xmlns:wp="%s" xmlns:pic="%s" xmlns:m="%s"`, name, w, r, a, wp, pic, m)
	fmt.Fprintf(&sb, ` xmlns:mc="%s" xmlns:wps="%s" xmlns:v="%s" xmlns:o="%s" xmlns:w14="%s" xmlns:w15="%s" mc:Ignorable="w14 w15"`, nsMC, nsWPS, nsV, nsO, nsW14, nsW15)
	if d.strict && name == "w:document" {
		sb.WriteString(` w:conformance="strict"`)
	}
	sb.WriteString(">")
	sb.WriteString(inner)
	sb.WriteString("</" + name + ">")
	return []byte(sb.String())
}

func (d *Doc) wmlPart(file, element, kind, inner string) *Doc {
	d.parts = append(d.parts, part{"word/" + file, ctWML + kind + "+xml", d.root("w:"+element, inner)})
	d.addRel(d.relType(kind), file, false)
	return d
}

func (d *Doc) relType(kind string) string {
	if d.strict {
		if kind == "extended-properties" {
			kind = "extendedProperties"
		}
		return "http://purl.oclc.org/ooxml/officeDocument/relationships/" + kind
	}
	return nsR + "/" + kind
}

func (d *Doc) addRel(typ, target string, external bool) string {
	d.nextRel++
	id := fmt.Sprintf("rId%d", d.nextRel)
	d.rels = append(d.rels, rel{id, typ, target, external})
	return id
}

func (d *Doc) count(prefix string) int {
	n := 0
	for _, p := range d.parts {
		if strings.HasPrefix(p.name, prefix) {
			n++
		}
	}
	return n
}

func relsXML(rels []rel) []byte {
	var sb strings.Builder
	sb.WriteString(xmlDecl + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for _, r := range rels {
		fmt.Fprintf(&sb, `<Relationship Id="%s" Type="%s" Target="%s"`, r.id, r.typ, Esc(r.target))
		if r.external {
			sb.WriteString(` TargetMode="External"`)
		}
		sb.WriteString("/>")
	}
	sb.WriteString(`</Relationships>`)
	return []byte(sb.String())
}

// separators returns the two notes that every footnotes and endnotes part
// starts with. They hold the rule drawn above the notes, not text.
func separators(kind string) string {
	return fmt.Sprintf(`<w:%[1]s w:type="separator" w:id="-1"><w:p><w:r><w:separator/></w:r></w:p></w:%[1]s>`+
		`<w:%[1]s w:type="continuationSeparator" w:id="0"><w:p><w:r><w:continuationSeparator/></w:r></w:p></w:%[1]s>`, kind)
}

// Esc escapes text for use in element content or an attribute value.
func Esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// R returns a run holding text.
func R(text string) string {
	return `<w:r><w:t xml:space="preserve">` + Esc(text) + `</w:t></w:r>`
}

// RPr returns a run holding text, with rPr as the content of its w:rPr.
func RPr(rPr, text string) string {
	return `<w:r><w:rPr>` + rPr + `</w:rPr><w:t xml:space="preserve">` + Esc(text) + `</w:t></w:r>`
}

// P returns a paragraph with one run of text.
func P(text string) string {
	return `<w:p>` + R(text) + `</w:p>`
}

// PStyle returns a paragraph in the style with the given ID, with one run of
// text.
func PStyle(styleID, text string) string {
	return `<w:p><w:pPr><w:pStyle w:val="` + styleID + `"/></w:pPr>` + R(text) + `</w:p>`
}

// PNum returns a paragraph numbered by the list numID at level, with one run
// of text.
func PNum(numID, level int, text string) string {
	return fmt.Sprintf(`<w:p><w:pPr><w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr></w:pPr>%s</w:p>`, level, numID, R(text))
}

// PPr returns a paragraph with pPr as the content of its w:pPr, followed by
// content, which is runs and other inline markup.
func PPr(pPr string, content ...string) string {
	return `<w:p><w:pPr>` + pPr + `</w:pPr>` + strings.Join(content, "") + `</w:p>`
}

// Para returns a paragraph with the given inline content.
func Para(content ...string) string {
	return `<w:p>` + strings.Join(content, "") + `</w:p>`
}

// Tbl returns a table with cols columns of equal width and the given rows.
func Tbl(cols int, rows ...string) string {
	var sb strings.Builder
	sb.WriteString(`<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/></w:tblPr><w:tblGrid>`)
	for range cols {
		sb.WriteString(`<w:gridCol w:w="2000"/>`)
	}
	sb.WriteString(`</w:tblGrid>`)
	for _, r := range rows {
		sb.WriteString(r)
	}
	sb.WriteString(`</w:tbl>`)
	return sb.String()
}

// Tr returns a table row with the given cells.
func Tr(cells ...string) string {
	return `<w:tr>` + strings.Join(cells, "") + `</w:tr>`
}

// TrPr returns a table row with trPr as the content of its w:trPr.
func TrPr(trPr string, cells ...string) string {
	return `<w:tr><w:trPr>` + trPr + `</w:trPr>` + strings.Join(cells, "") + `</w:tr>`
}

// Tc returns a table cell holding one paragraph of text.
func Tc(text string) string {
	return `<w:tc>` + P(text) + `</w:tc>`
}

// TcPr returns a table cell with tcPr as the content of its w:tcPr, followed
// by block-level content. A cell must end with a paragraph.
func TcPr(tcPr string, content ...string) string {
	s := `<w:tc>`
	if tcPr != "" {
		s += `<w:tcPr>` + tcPr + `</w:tcPr>`
	}
	return s + strings.Join(content, "") + `</w:tc>`
}

// Style returns a paragraph style definition. basedOn may be empty. pPr and
// rPr are the contents of w:pPr and w:rPr.
func Style(id, name, basedOn, pPr, rPr string) string {
	return styleXML("paragraph", id, name, basedOn, pPr, rPr)
}

// CharStyle returns a character style definition.
func CharStyle(id, name, basedOn, rPr string) string {
	return styleXML("character", id, name, basedOn, "", rPr)
}

func styleXML(typ, id, name, basedOn, pPr, rPr string) string {
	s := `<w:style w:type="` + typ + `" w:styleId="` + id + `"><w:name w:val="` + Esc(name) + `"/>`
	if basedOn != "" {
		s += `<w:basedOn w:val="` + basedOn + `"/>`
	}
	if pPr != "" {
		s += `<w:pPr>` + pPr + `</w:pPr>`
	}
	if rPr != "" {
		s += `<w:rPr>` + rPr + `</w:rPr>`
	}
	return s + `</w:style>`
}

// NormalStyle is the default paragraph style, which most style parts start
// with.
const NormalStyle = `<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>`

// Lvl returns one level of a numbering definition. extra is markup placed
// between w:numFmt and w:lvlText, in schema order: w:lvlRestart, w:pStyle,
// w:isLgl, w:suff. rPr, when given, is the content of the level's w:rPr,
// which sets the font of the label.
func Lvl(ilvl, start int, numFmt, lvlText, extra string, rPr ...string) string {
	s := fmt.Sprintf(`<w:lvl w:ilvl="%d"><w:start w:val="%d"/><w:numFmt w:val="%s"/>%s<w:lvlText w:val="%s"/><w:lvlJc w:val="left"/>`,
		ilvl, start, numFmt, extra, Esc(lvlText))
	if len(rPr) > 0 {
		s += `<w:rPr>` + strings.Join(rPr, "") + `</w:rPr>`
	}
	return s + `</w:lvl>`
}

// AbstractNum returns an abstract numbering definition with the given levels.
func AbstractNum(id int, levels ...string) string {
	return fmt.Sprintf(`<w:abstractNum w:abstractNumId="%d"><w:multiLevelType w:val="multilevel"/>%s</w:abstractNum>`, id, strings.Join(levels, ""))
}

// Num returns a numbering instance that uses the abstract definition.
// overrides is the w:lvlOverride markup, or "".
func Num(numID, abstractID int, overrides string) string {
	return fmt.Sprintf(`<w:num w:numId="%d"><w:abstractNumId w:val="%d"/>%s</w:num>`, numID, abstractID, overrides)
}

// StartOverride returns a w:lvlOverride that restarts a level at start.
func StartOverride(ilvl, start int) string {
	return fmt.Sprintf(`<w:lvlOverride w:ilvl="%d"><w:startOverride w:val="%d"/></w:lvlOverride>`, ilvl, start)
}

// Field returns the runs of a complex field: its instruction and the result
// that the last program to save the file left in it.
func Field(instr string, result ...string) string {
	return `<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText xml:space="preserve">` + Esc(instr) + `</w:instrText></w:r>` +
		`<w:r><w:fldChar w:fldCharType="separate"/></w:r>` + strings.Join(result, "") + `<w:r><w:fldChar w:fldCharType="end"/></w:r>`
}
