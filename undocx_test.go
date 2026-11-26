package undocx_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gomcpgo/undocx"
	"github.com/gomcpgo/undocx/internal/docxtest"
)

var ctx = context.Background()

func open(data []byte, opts undocx.Options) (*undocx.Document, error) {
	return undocx.Open(ctx, bytes.NewReader(data), int64(len(data)), opts)
}

func mustOpen(t *testing.T, data []byte) *undocx.Document {
	t.Helper()
	doc, err := open(data, undocx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func codes(doc *undocx.Document) string {
	var out []string
	for _, w := range doc.Warnings() {
		out = append(out, w.Code)
	}
	return strings.Join(out, ",")
}

// Every fixture is a sound document: it opens as a .docx with no warnings.
func TestFixturesOpen(t *testing.T) {
	for _, f := range docxtest.Fixtures() {
		doc, err := open(f.Doc().Bytes(), undocx.Options{})
		if err != nil {
			t.Errorf("%s: %v", f.Name, err)
			continue
		}
		info := doc.Info()
		if info.Format != "docx" {
			t.Errorf("%s: format %q", f.Name, info.Format)
		}
		if info.Strict != (f.Name == "strict") {
			t.Errorf("%s: Strict is %v", f.Name, info.Strict)
		}
		if got := codes(doc); got != "" {
			t.Errorf("%s: warnings %s", f.Name, got)
		}
		doc.Close()
	}
}

func TestInfo(t *testing.T) {
	var fixture docxtest.Fixture
	for _, f := range docxtest.Fixtures() {
		if f.Name == "metadata" {
			fixture = f
		}
	}
	got := mustOpen(t, fixture.Doc().Bytes()).Info()
	want := undocx.Info{
		Format:         "docx",
		Title:          "Supply Agreement",
		Author:         "Ann Lee",
		Subject:        "Widgets",
		Keywords:       "supply, widgets",
		LastModifiedBy: "Raj Patel",
		Revision:       7,
		Created:        time.Date(2026, 1, 2, 8, 0, 0, 0, time.UTC),
		Modified:       time.Date(2026, 2, 3, 17, 30, 0, 0, time.UTC),
		Application:    "Microsoft Office Word",
		Pages:          4,
		Words:          1234,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	// The same properties in a Strict document, whose extended properties
	// use another namespace and relationship type.
	strict := docxtest.New().Strict().
		Core(`<dc:title>Strict title</dc:title><dcterms:created xsi:type="dcterms:W3CDTF">2026-05-06</dcterms:created>`).
		App(`<Application>LibreOffice</Application><AppVersion>26.2</AppVersion><Template>Normal.dotm</Template><Company>Acme</Company><Characters>99</Characters><Paragraphs>3</Paragraphs>`)
	got = mustOpen(t, strict.Bytes()).Info()
	want = undocx.Info{Format: "docx", Strict: true, Title: "Strict title", Created: time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC),
		Application: "LibreOffice", AppVersion: "26.2", Template: "Normal.dotm", Company: "Acme", Characters: 99, Paragraphs: 3}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("strict: got  %+v\nwant %+v", got, want)
	}
}

func TestProperties(t *testing.T) {
	// Values are trimmed; odd numbers and dates give zero, not an error;
	// the first of a repeated property wins; nested markup contributes its
	// text.
	doc := mustOpen(t, docxtest.New().
		Core(`<dc:title>  Spaced  out </dc:title><dc:title>Second</dc:title><cp:revision>seven</cp:revision>`+
			`<dcterms:created>yesterday</dcterms:created><dcterms:modified>2026-03-04T05:06:07.5+05:30</dcterms:modified>`+
			`<cp:keywords><cp:value>alpha</cp:value> <cp:value>beta</cp:value></cp:keywords><cp:lastPrinted>2025-12-31T23:59:00</cp:lastPrinted>`).
		App(`<Pages>-3</Pages><Words>12 000</Words><Characters>42</Characters>`).Bytes())
	info := doc.Info()
	if info.Title != "Spaced  out" || info.Revision != 0 || !info.Created.IsZero() || info.Keywords != "alpha beta" ||
		info.Pages != 0 || info.Words != 0 || info.Characters != 42 {
		t.Errorf("%+v", info)
	}
	if want := time.Date(2026, 3, 3, 23, 36, 7, 500000000, time.UTC); !info.Modified.Equal(want) {
		t.Errorf("Modified is %v, want %v", info.Modified, want)
	}
	if want := time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC); !info.LastPrinted.Equal(want) {
		t.Errorf("LastPrinted is %v, want %v", info.LastPrinted, want)
	}
	if got := codes(doc); got != "" {
		t.Errorf("warnings: %s", got)
	}

	// Malformed properties are a warning, and what came before the damage
	// is kept.
	entries := docxtest.New().Core(`<dc:title>Kept</dc:title>`).Entries()
	for i := range entries {
		if entries[i].Name == "docProps/core.xml" {
			entries[i].Data = bytes.Replace(entries[i].Data, []byte("</cp:coreProperties>"), []byte("<dc:creator>Lost"), 1)
		}
	}
	doc = mustOpen(t, docxtest.Zip(entries...))
	if doc.Info().Title != "Kept" || doc.Info().Author != "" || codes(doc) != undocx.WarnMetadata {
		t.Errorf("malformed properties: %+v, warnings %s", doc.Info(), codes(doc))
	}

	// A properties part that is there but not named by a relationship is
	// still found by its usual name.
	entries = docxtest.New().Core(`<dc:title>By name</dc:title>`).Entries()
	for i := range entries {
		if entries[i].Name == "_rels/.rels" {
			entries[i].Data = bytes.Replace(entries[i].Data, []byte("metadata/core-properties"), []byte("metadata/something-else"), 1)
		}
	}
	if doc = mustOpen(t, docxtest.Zip(entries...)); doc.Info().Title != "By name" {
		t.Errorf("properties by name: %+v", doc.Info())
	}
}

func TestFormats(t *testing.T) {
	for want, d := range map[string]*docxtest.Doc{
		"docx": docxtest.New(),
		"docm": docxtest.New().MacroEnabled(),
		"dotx": docxtest.New().Template(),
		"dotm": docxtest.New().MacroEnabledTemplate(),
	} {
		if got := mustOpen(t, d.Body(docxtest.P("x")).Bytes()).Info().Format; got != want {
			t.Errorf("got format %q, want %q", got, want)
		}
	}
}

// Packages that are incomplete in ways a reader can work around.
func TestRecoverable(t *testing.T) {
	base := docxtest.New().Body(docxtest.P("x")).Entries()
	without := func(name string) []byte {
		var out []docxtest.Entry
		for _, e := range base {
			if e.Name != name {
				out = append(out, e)
			}
		}
		return docxtest.Zip(out...)
	}
	tests := []struct {
		name  string
		data  []byte
		codes string
	}{
		{"no package relationships", without("_rels/.rels"), undocx.WarnMainPart},
		{"no content types", without("[Content_Types].xml"), undocx.WarnContentTypes + "," + undocx.WarnContentTypes},
		{"neither", docxtest.Zip(base[2]), undocx.WarnContentTypes + "," + undocx.WarnMainPart + "," + undocx.WarnContentTypes},
		{"main part under another name", docxtest.New().MainName("word/document2.xml").Bytes(), ""},
		{"main part outside word/", docxtest.New().MainName("doc.xml").Bytes(), ""},
		{"main part with a generic content type", docxtest.New().MainType("application/xml").Bytes(), undocx.WarnContentTypes},
		{"data in front of the archive", append([]byte("#!/bin/sh\nexit 0\n"), docxtest.Zip(base...)...), ""},
	}
	for _, tt := range tests {
		doc, err := open(tt.data, undocx.Options{})
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if doc.Info().Format != "docx" || codes(doc) != tt.codes {
			t.Errorf("%s: format %q, warnings %q, want %q", tt.name, doc.Info().Format, codes(doc), tt.codes)
		}
	}
}

func TestNotDOCX(t *testing.T) {
	sheet := docxtest.New().MainName("xl/workbook.xml").MainType("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml")
	slides := docxtest.New().MainName("ppt/presentation.xml").MainType("application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml")

	tests := []struct {
		name   string
		data   []byte
		format string
		legacy bool
	}{
		{"spreadsheet", sheet.Bytes(), undocx.FormatXLSX, false},
		{"macro-enabled spreadsheet", docxtest.New().MainName("xl/workbook.xml").MainType("application/vnd.ms-excel.sheet.macroEnabled.main+xml").Bytes(), undocx.FormatXLSX, false},
		{"presentation", slides.Bytes(), undocx.FormatPPTX, false},
		{"spreadsheet without relationships", docxtest.Zip(sheet.Entries()[0], sheet.Entries()[2]), undocx.FormatXLSX, false},
		{"OpenDocument text", docxtest.Zip(docxtest.Entry{Name: "mimetype", Data: []byte("application/vnd.oasis.opendocument.text"), Store: true}, docxtest.Entry{Name: "content.xml", Data: []byte("<x/>")}), undocx.FormatOpenDoc, false},
		{"EPUB", docxtest.Zip(docxtest.Entry{Name: "mimetype", Data: []byte("application/epub+zip"), Store: true}), undocx.FormatEPUB, false},
		{"plain zip", docxtest.Zip(docxtest.Entry{Name: "readme.txt", Data: []byte("hello")}), undocx.FormatZip, false},
		{"empty zip", docxtest.Zip(), undocx.FormatZip, false},
		{"zip whose document.xml is something else", docxtest.Zip(docxtest.Entry{Name: "word/document.xml", Data: []byte("<html/>")}), undocx.FormatZip, false},
		{"Word 97-2003", docxtest.CompoundFile("1Table", "WordDocument"), undocx.FormatDOC, true},
		{"Excel 97-2003", docxtest.CompoundFile("Workbook"), undocx.FormatXLS, false},
		{"PowerPoint 97-2003", docxtest.CompoundFile("PowerPoint Document"), undocx.FormatPPT, false},
		{"other compound file", docxtest.CompoundFile("Something"), undocx.FormatOLE, false},
		{"RTF", []byte(`{\rtf1\ansi Hello}`), undocx.FormatRTF, true},
		{"Word 2003 XML", []byte(`<?xml version="1.0"?><w:wordDocument xmlns:w="http://schemas.microsoft.com/office/word/2003/wordml"/>`), undocx.FormatWord2003XML, true},
		{"flat OPC", []byte(`<?xml version="1.0"?><pkg:package xmlns:pkg="http://schemas.microsoft.com/office/2006/xmlPackage"/>`), undocx.FormatFlatOPC, true},
		{"PDF", []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n"), undocx.FormatPDF, false},
		{"HTML", []byte("<html><body>Saved as .doc</body></html>"), undocx.FormatHTML, false},
		{"web archive", []byte("MIME-Version: 1.0\r\n"), undocx.FormatMHT, false},
		{"XML", []byte(`<?xml version="1.0"?><root/>`), undocx.FormatXML, false},
		{"empty file", nil, undocx.FormatEmpty, false},
		{"text", []byte("Dear Sir or Madam,\n"), "", false},
	}
	for _, tt := range tests {
		_, err := open(tt.data, undocx.Options{})
		var fe *undocx.FormatError
		if !errors.As(err, &fe) {
			t.Errorf("%s: got %v, want a FormatError", tt.name, err)
			continue
		}
		if fe.Format != tt.format {
			t.Errorf("%s: format %q, want %q", tt.name, fe.Format, tt.format)
		}
		if !errors.Is(err, undocx.ErrNotDOCX) {
			t.Errorf("%s: does not match ErrNotDOCX", tt.name)
		}
		if errors.Is(err, undocx.ErrLegacyFormat) != tt.legacy {
			t.Errorf("%s: matching ErrLegacyFormat is %v, want %v", tt.name, !tt.legacy, tt.legacy)
		}
		if errors.Is(err, undocx.ErrDamaged) || errors.Is(err, undocx.ErrEncrypted) {
			t.Errorf("%s: matches an unrelated error", tt.name)
		}
		if err.Error() == "" || !strings.HasPrefix(err.Error(), "undocx: ") {
			t.Errorf("%s: message %q", tt.name, err)
		}
	}
}

func TestEncrypted(t *testing.T) {
	// A document with a password to open is a compound file holding the
	// encrypted package.
	_, err := open(docxtest.CompoundFile("\x06DataSpaces", "EncryptionInfo", "EncryptedPackage"), undocx.Options{})
	if !errors.Is(err, undocx.ErrEncrypted) || errors.Is(err, undocx.ErrNotDOCX) {
		t.Errorf("encrypted package: %v", err)
	}
	// A zip entry flagged as encrypted.
	entries := docxtest.New().Entries()
	entries[2].Encrypted = true
	if _, err := open(docxtest.Zip(entries...), undocx.Options{}); !errors.Is(err, undocx.ErrEncrypted) {
		t.Errorf("encrypted zip entry: %v", err)
	}
}

func TestDamaged(t *testing.T) {
	whole := docxtest.New().Body(docxtest.P(strings.Repeat("Some text to fill the part. ", 300))).Bytes()
	mainData := func(data string) []byte {
		entries := docxtest.New().Entries()
		entries[2].Data = []byte(data)
		return docxtest.Zip(entries...)
	}
	corrupt := bytes.Clone(whole)
	at := bytes.Index(corrupt, []byte("word/document.xml")) + len("word/document.xml") + 2
	for i := 0; i < 24; i++ {
		corrupt[at+i] ^= 0xa5
	}
	tests := map[string][]byte{
		"cut short":              whole[:len(whole)*2/3],
		"cut to the first bytes": whole[:20],
		// The directory at the end of the archive survives and points at
		// data that is gone.
		"the first half is missing":      whole[len(whole)/2:],
		"main part is not XML":           mainData("this is not XML"),
		"main part is empty":             mainData(""),
		"main part has another root":     mainData(`<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"/>`),
		"main part in another namespace": mainData(`<w:document xmlns:w="urn:other"/>`),
		"main part data is corrupt":      corrupt,
	}
	for name, data := range tests {
		_, err := open(data, undocx.Options{})
		if !errors.Is(err, undocx.ErrDamaged) {
			t.Errorf("%s: got %v, want ErrDamaged", name, err)
		}
		if errors.Is(err, undocx.ErrNotDOCX) {
			t.Errorf("%s: also matches ErrNotDOCX", name)
		}
	}
}

func TestLimits(t *testing.T) {
	var lim *undocx.LimitError
	data := docxtest.New().Body(docxtest.P("x")).Bytes()
	if _, err := open(data, undocx.Options{Limits: undocx.Limits{MaxParts: 2}}); !errors.As(err, &lim) || lim.Limit != 2 {
		t.Errorf("MaxParts: %v", err)
	}
	if _, err := open(data, undocx.Options{Limits: undocx.Limits{MaxPartBytes: 100}}); !errors.As(err, &lim) {
		t.Errorf("MaxPartBytes: %v", err)
	}
	if _, err := open(data, undocx.Options{Limits: undocx.Limits{MaxTotalBytes: 300}}); !errors.As(err, &lim) {
		t.Errorf("MaxTotalBytes: %v", err)
	}
	if _, err := open(data, undocx.Options{Limits: undocx.Limits{MaxTokenBytes: 50}}); !errors.As(err, &lim) {
		t.Errorf("MaxTokenBytes: %v", err)
	}
	if _, err := open(data, undocx.Options{Limits: undocx.DefaultLimits()}); err != nil {
		t.Errorf("default limits: %v", err)
	}
	if d := undocx.DefaultLimits(); d.MaxParts == 0 || d.MaxPartBytes == 0 || d.MaxTotalBytes == 0 || d.MaxCompressionRatio == 0 ||
		d.MaxDepth == 0 || d.MaxTokenBytes == 0 || d.MaxWarnings == 0 {
		t.Errorf("a default is zero: %+v", d)
	}

	// A main part that expands past the ratio: a zip bomb.
	entries := docxtest.New().Entries()
	entries[2].Data = append([]byte(`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">`), bytes.Repeat([]byte(" "), 9<<20)...)
	if _, err := open(docxtest.Zip(entries...), undocx.Options{}); !errors.As(err, &lim) || !strings.Contains(lim.What, "ratio") {
		t.Errorf("zip bomb: %v", err)
	}

	// Warnings are capped, and the cap is reported.
	var many []docxtest.Entry
	many = append(many, docxtest.New().Entries()...)
	for i := 0; i < 30; i++ {
		many = append(many, docxtest.Entry{Name: "word//bad" + strings.Repeat("x", i), Data: []byte("x")})
	}
	doc, err := open(docxtest.Zip(many...), undocx.Options{Limits: undocx.Limits{MaxWarnings: 5}})
	if err != nil {
		t.Fatal(err)
	}
	ws := doc.Warnings()
	if len(ws) != 6 || ws[5].Code != undocx.WarnLimit || !strings.Contains(ws[5].Message, "25") {
		t.Errorf("warnings: %+v", ws)
	}
}

func TestCancelled(t *testing.T) {
	c, cancel := context.WithCancel(ctx)
	cancel()
	data := docxtest.New().Bytes()
	if _, err := undocx.Open(c, bytes.NewReader(data), int64(len(data)), undocx.Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestOpenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "named-wrongly.pdf")
	if err := os.WriteFile(path, docxtest.New().Core(`<dc:title>From a file</dc:title>`).Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	doc, err := undocx.OpenFile(ctx, path, undocx.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Info().Title != "From a file" {
		t.Errorf("%+v", doc.Info())
	}
	if err := doc.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := doc.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := undocx.OpenFile(ctx, filepath.Join(t.TempDir(), "missing.docx"), undocx.Options{}); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
	if _, err := undocx.OpenFile(ctx, t.TempDir(), undocx.Options{}); err == nil || errors.Is(err, undocx.ErrNotDOCX) {
		t.Errorf("a folder: %v", err)
	}
}

func TestDeterministic(t *testing.T) {
	for _, f := range docxtest.Fixtures() {
		data := f.Doc().Bytes()
		a, b := mustOpen(t, data), mustOpen(t, data)
		if !reflect.DeepEqual(a.Info(), b.Info()) || !reflect.DeepEqual(a.Warnings(), b.Warnings()) {
			t.Errorf("%s: two readings differ", f.Name)
		}
	}
}

func FuzzOpen(f *testing.F) {
	for _, fx := range docxtest.Fixtures() {
		switch fx.Name {
		case "metadata", "strict", "main-part-renamed", "text-boxes":
			f.Add(fx.Doc().Bytes())
		}
	}
	f.Add(docxtest.CompoundFile("WordDocument"))
	f.Add(docxtest.CompoundFile("EncryptedPackage"))
	f.Add([]byte(`{\rtf1}`))
	f.Add(docxtest.Zip(docxtest.Entry{Name: "mimetype", Data: []byte("application/epub+zip"), Store: true}))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, err := open(data, undocx.Options{Limits: undocx.Limits{MaxParts: 64, MaxPartBytes: 1 << 20, MaxTotalBytes: 4 << 20, MaxDepth: 32, MaxTokenBytes: 1 << 16, MaxWarnings: 16}})
		if err != nil {
			if doc != nil {
				t.Fatalf("a document was returned with the error %v", err)
			}
			if !errors.Is(err, undocx.ErrNotDOCX) && !errors.Is(err, undocx.ErrEncrypted) && !errors.Is(err, undocx.ErrDamaged) {
				var lim *undocx.LimitError
				if !errors.As(err, &lim) {
					t.Fatalf("an error of no known kind: %v", err)
				}
			}
			return
		}
		switch doc.Info().Format {
		case "docx", "docm", "dotx", "dotm":
		default:
			t.Fatalf("format %q", doc.Info().Format)
		}
		if len(doc.Warnings()) > 17 {
			t.Fatalf("%d warnings, past the cap", len(doc.Warnings()))
		}
		doc.Close()
	})
}
