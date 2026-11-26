package opc

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gomcpgo/undocx/internal/diag"
	"github.com/gomcpgo/undocx/internal/docxtest"
)

var testLimits = Limits{MaxParts: 1000, MaxPartBytes: 64 << 20, MaxTotalBytes: 256 << 20, MaxRatio: 250}

func open(t *testing.T, data []byte, lim Limits) (*Package, *diag.Log, error) {
	t.Helper()
	log := diag.NewLog(50)
	p, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), lim, log)
	return p, log, err
}

func mustOpen(t *testing.T, data []byte) (*Package, *diag.Log) {
	t.Helper()
	p, log, err := open(t, data, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	return p, log
}

func codes(log *diag.Log) string {
	var out []string
	for _, w := range log.List() {
		out = append(out, w.Code)
	}
	return strings.Join(out, ",")
}

func TestPartName(t *testing.T) {
	tests := []struct {
		in, want string
		ok       bool
	}{
		{"word/document.xml", "word/document.xml", true},
		{"/word/document.xml", "word/document.xml", true},
		{"Word/Document.XML", "word/document.xml", true},
		{`word\document.xml`, "word/document.xml", true},
		{"word/media/image%201.png", "word/media/image 1.png", true},
		{"word/100%.xml", "word/100%.xml", true},
		{"word/a%zz.xml", "word/a%zz.xml", true},
		{"[Content_Types].xml", "[content_types].xml", true},
		{"", "", false},
		{"/", "", false},
		{"word//document.xml", "", false},
		{"word/../document.xml", "", false},
		{"../etc/passwd", "", false},
		{"word/./document.xml", "", false},
		{"word/%2e%2e/x", "", false},
	}
	for _, tt := range tests {
		got, ok := PartName(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("PartName(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		source, target, want string
		ok                   bool
	}{
		{"", "word/document.xml", "word/document.xml", true},
		{"", "/word/document.xml", "word/document.xml", true},
		{"word/document.xml", "styles.xml", "word/styles.xml", true},
		{"word/document.xml", "media/image1.png", "word/media/image1.png", true},
		{"word/document.xml", "../customXml/item1.xml", "customxml/item1.xml", true},
		{"word/document.xml", "/word/numbering.xml", "word/numbering.xml", true},
		{"word/document.xml", `media\image1.png`, "word/media/image1.png", true},
		{"word/document.xml", "media/image%201.png", "word/media/image 1.png", true},
		{"word/document.xml", "footnotes.xml#note", "word/footnotes.xml", true},
		{"word/glossary/document.xml", "../styles.xml", "word/styles.xml", true},
		// Steps above the root stay inside the package.
		{"word/document.xml", "../../../../etc/passwd", "etc/passwd", true},
		{"word/document.xml", "/../../etc/passwd", "etc/passwd", true},
		{"word/document.xml", "", "", false},
		{"word/document.xml", "#bookmark", "", false},
		{"word/document.xml", "..", "", false},
	}
	for _, tt := range tests {
		got, ok := Resolve(tt.source, tt.target)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Resolve(%q, %q) = %q, %v; want %q, %v", tt.source, tt.target, got, ok, tt.want, tt.ok)
		}
	}
	if got := RelsPart(""); got != "_rels/.rels" {
		t.Errorf("RelsPart of the package is %q", got)
	}
	if got := RelsPart("word/document.xml"); got != "word/_rels/document.xml.rels" {
		t.Errorf("RelsPart of the main part is %q", got)
	}
}

func TestPackage(t *testing.T) {
	d := docxtest.New()
	link := d.Link("https://example.com/")
	img := d.Image("image1.png", []byte("not really a png"))
	d.Styles(docxtest.NormalStyle).Body(docxtest.P("Hello"))
	p, log := mustOpen(t, d.Bytes())
	if got := codes(log); got != "" {
		t.Errorf("warnings: %s", got)
	}

	if !p.Has("word/document.xml") || p.Has("word/nothing.xml") {
		t.Errorf("Has is wrong")
	}
	for part, want := range map[string]string{
		"word/document.xml":            "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml",
		"word/styles.xml":              "application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml",
		"word/media/image1.png":        "image/png",
		"_rels/.rels":                  "application/vnd.openxmlformats-package.relationships+xml",
		"word/unknown.bin":             "",
		"word/noextension":             "",
		"word.dir/noextension":         "",
		"word/_rels/document.xml.rels": "application/vnd.openxmlformats-package.relationships+xml",
	} {
		if got := p.ContentType(part); got != want {
			t.Errorf("ContentType(%s) = %q, want %q", part, got, want)
		}
	}

	rels, err := p.Relationships(context.Background(), "")
	if err != nil || len(rels) != 1 || rels[0].Part != "word/document.xml" || rels[0].External ||
		rels[0].Type != "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" {
		t.Errorf("package relationships: %+v, %v", rels, err)
	}
	rels, err = p.Relationships(context.Background(), "word/document.xml")
	if err != nil || len(rels) != 3 {
		t.Fatalf("main part relationships: %+v, %v", rels, err)
	}
	for _, r := range rels {
		switch r.ID {
		case link:
			if !r.External || r.Part != "" || r.Target != "https://example.com/" {
				t.Errorf("link: %+v", r)
			}
		case img:
			if r.External || r.Part != "word/media/image1.png" {
				t.Errorf("image: %+v", r)
			}
		}
	}
	// A part with no relationships part has none.
	if rels, err := p.Relationships(context.Background(), "word/styles.xml"); err != nil || rels != nil {
		t.Errorf("relationships of styles: %+v, %v", rels, err)
	}

	data, err := p.ReadAll("word/media/image1.png", 100)
	if err != nil || string(data) != "not really a png" {
		t.Errorf("ReadAll: %q, %v", data, err)
	}
	var lim *diag.LimitError
	if _, err := p.ReadAll("word/media/image1.png", 5); !errors.As(err, &lim) {
		t.Errorf("ReadAll past its maximum: %v", err)
	}
	if _, err := p.Open("word/nothing.xml"); !errors.Is(err, ErrNoPart) {
		t.Errorf("opening a missing part: %v", err)
	}
	if got := strings.Join(p.Names(), " "); !strings.HasPrefix(got, "[content_types].xml _rels/.rels word/_rels/document.xml.rels word/document.xml") {
		t.Errorf("Names: %s", got)
	}
}

func TestStrictRelationshipTypes(t *testing.T) {
	p, _ := mustOpen(t, docxtest.New().Strict().Styles(docxtest.NormalStyle).Bytes())
	rels, _ := p.Relationships(context.Background(), "")
	if len(rels) != 1 || rels[0].Type != "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" {
		t.Errorf("package relationships: %+v", rels)
	}
	rels, _ = p.Relationships(context.Background(), "word/document.xml")
	if len(rels) != 1 || rels[0].Type != "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" {
		t.Errorf("main part relationships: %+v", rels)
	}
}

func TestOddArchives(t *testing.T) {
	base := docxtest.New().Body(docxtest.P("x")).Entries()
	with := func(extra ...docxtest.Entry) []byte {
		return docxtest.Zip(append(append([]docxtest.Entry(nil), base...), extra...)...)
	}

	// Names with a leading slash, backslashes and upper case all name parts.
	p, log := mustOpen(t, docxtest.Zip(
		docxtest.Entry{Name: "/[Content_Types].xml", Data: base[0].Data},
		docxtest.Entry{Name: `_rels\.rels`, Data: base[1].Data},
		docxtest.Entry{Name: "WORD/Document.xml", Data: base[2].Data},
		docxtest.Entry{Name: "word/", Data: nil},
	))
	if !p.Has("word/document.xml") || !p.Has("_rels/.rels") || p.ContentType("word/document.xml") == "" || codes(log) != "" {
		t.Errorf("odd names: parts %v, warnings %s", p.Names(), codes(log))
	}

	// A part stored twice: the first copy is used, with a warning.
	p, log = mustOpen(t, with(docxtest.Entry{Name: "word/document.xml", Data: []byte("<second/>")}, docxtest.Entry{Name: "Word/Document.xml", Data: []byte("<third/>")}))
	data, err := p.ReadAll("word/document.xml", 1<<20)
	if err != nil || !bytes.Equal(data, base[2].Data) || codes(log) != WarnDuplicatePart+","+WarnDuplicatePart {
		t.Errorf("duplicate part: %v, warnings %s", err, codes(log))
	}

	// Names no part may have are ignored, with a warning.
	p, log = mustOpen(t, with(docxtest.Entry{Name: "../evil.xml", Data: []byte("x")}, docxtest.Entry{Name: "word//x.xml", Data: []byte("x")}))
	if len(p.Names()) != len(base) || codes(log) != WarnPartName+","+WarnPartName {
		t.Errorf("bad names: %d parts, warnings %s", len(p.Names()), codes(log))
	}

	// No content types: the package still opens.
	p, log = mustOpen(t, docxtest.Zip(base[1:]...))
	if codes(log) != WarnContentTypes || p.ContentType("word/document.xml") != "" {
		t.Errorf("no content types: warnings %s", codes(log))
	}
	// Malformed content types likewise, keeping what was read before the damage.
	p, log = mustOpen(t, docxtest.Zip(append([]docxtest.Entry{{Name: "[Content_Types].xml", Data: []byte(`<Types><Default Extension="XML" ContentType="application/xml"/><Oops`)}}, base[1:]...)...))
	if codes(log) != WarnContentTypes || p.ContentType("word/document.xml") != "application/xml" {
		t.Errorf("malformed content types: warnings %s, type %q", codes(log), p.ContentType("word/document.xml"))
	}

	// Not a zip archive at all.
	if _, _, err := open(t, []byte("plain text, not an archive"), testLimits); !errors.Is(err, ErrNotZip) {
		t.Errorf("not a zip: %v", err)
	}
	whole := docxtest.Zip(base...)
	if _, _, err := open(t, whole[:len(whole)/2], testLimits); !errors.Is(err, ErrNotZip) {
		t.Errorf("half a zip: %v", err)
	}

	// An entry flagged as encrypted.
	p, _ = mustOpen(t, with(docxtest.Entry{Name: "word/secret.xml", Data: []byte("x"), Encrypted: true}))
	if _, err := p.Open("word/secret.xml"); !errors.Is(err, ErrEncryptedPart) {
		t.Errorf("encrypted entry: %v", err)
	}
}

func TestCorruptData(t *testing.T) {
	// Flip bytes inside the compressed data of a part: reading it reports
	// damage, and opening the package does not.
	body := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 400)
	entries := docxtest.New().Body(docxtest.P(body)).Entries()
	data := docxtest.Zip(entries...)
	at := bytes.Index(data, []byte("word/document.xml")) + len("word/document.xml") + 40
	for i := 0; i < 30; i++ {
		data[at+i] ^= 0x5a
	}
	p, _ := mustOpen(t, data)
	rc, err := p.Open("word/document.xml")
	if err == nil {
		_, err = io.ReadAll(rc)
		rc.Close()
	}
	if !errors.Is(err, diag.ErrDamaged) {
		t.Errorf("got %v, want ErrDamaged", err)
	}
}

func TestLimits(t *testing.T) {
	var lim *diag.LimitError
	entries := docxtest.New().Body(docxtest.P("x")).Entries()

	l := testLimits
	l.MaxParts = len(entries) - 1
	if _, _, err := open(t, docxtest.Zip(entries...), l); !errors.As(err, &lim) {
		t.Errorf("too many parts: %v", err)
	}
	l.MaxParts = len(entries)
	if _, _, err := open(t, docxtest.Zip(entries...), l); err != nil {
		t.Errorf("exactly as many parts as allowed: %v", err)
	}

	// One part over the size limit: it cannot be read, the rest can.
	big := docxtest.Zip(append(entries, docxtest.Entry{Name: "word/big.xml", Data: bytes.Repeat([]byte("<a/>"), 1<<18)})...)
	l = testLimits
	l.MaxPartBytes = 1 << 19
	p, _, err := open(t, big, l)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Open("word/big.xml"); !errors.As(err, &lim) {
		t.Errorf("part over the size limit: %v", err)
	}
	if _, err := p.ReadAll("word/document.xml", 1<<19); err != nil {
		t.Errorf("small part beside a big one: %v", err)
	}

	// A part that expands far more than the ratio allows.
	bomb := docxtest.Zip(append(entries, docxtest.Entry{Name: "word/bomb.xml", Data: make([]byte, RatioFloor+1<<20)})...)
	if len(bomb) > 100<<10 {
		t.Fatalf("the test archive is %d bytes; it should be tiny", len(bomb))
	}
	p, _ = mustOpen(t, bomb)
	if _, err := p.Open("word/bomb.xml"); !errors.As(err, &lim) || !strings.Contains(lim.What, "compression ratio") {
		t.Errorf("zip bomb: %v", err)
	}
	// Below the floor the ratio is not checked.
	small := docxtest.Zip(append(entries, docxtest.Entry{Name: "word/zeros.xml", Data: make([]byte, 1<<20)})...)
	p, _ = mustOpen(t, small)
	if _, err := p.ReadAll("word/zeros.xml", 2<<20); err != nil {
		t.Errorf("a small part that compresses well: %v", err)
	}

	// The total counts each part once, however often it is read.
	two := docxtest.Zip(append(entries,
		docxtest.Entry{Name: "word/a.xml", Data: bytes.Repeat([]byte("a"), 60000)},
		docxtest.Entry{Name: "word/b.xml", Data: bytes.Repeat([]byte("b"), 60000)})...)
	l = testLimits
	l.MaxTotalBytes = 100000
	p, _, err = open(t, two, l)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := p.ReadAll("word/a.xml", 1<<20); err != nil {
			t.Fatalf("reading one part again: %v", err)
		}
	}
	if _, err := p.ReadAll("word/b.xml", 1<<20); !errors.As(err, &lim) || !strings.Contains(lim.What, "total") {
		t.Errorf("total over the limit: %v", err)
	}
}

func FuzzOpen(f *testing.F) {
	f.Add(docxtest.New().Body(docxtest.P("x")).Bytes())
	f.Add(docxtest.New().Strict().Styles(docxtest.NormalStyle).Bytes())
	f.Add(docxtest.Zip(docxtest.Entry{Name: "../x", Data: []byte("x")}, docxtest.Entry{Name: "a", Data: []byte("y"), Store: true}))
	f.Fuzz(func(t *testing.T, data []byte) {
		lim := Limits{MaxParts: 64, MaxPartBytes: 1 << 20, MaxTotalBytes: 4 << 20, MaxRatio: 100}
		p, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), lim, diag.NewLog(10))
		if err != nil {
			return
		}
		ctx := context.Background()
		for _, name := range append(p.Names(), "") {
			p.ContentType(name)
			p.Relationships(ctx, name)
			if rc, err := p.Open(name); err == nil {
				n, _ := io.Copy(io.Discard, rc)
				rc.Close()
				if n > lim.MaxPartBytes {
					t.Fatalf("read %d bytes from %s, past the limit", n, name)
				}
			}
		}
	})
}
