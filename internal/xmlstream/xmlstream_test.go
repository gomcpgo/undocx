package xmlstream

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/gomcpgo/undocx/internal/diag"
)

const (
	nsW   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	nsWPS = "http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
)

// dump reads everything and writes it in a compact form: <name> for a start
// tag, </name> for an end tag and the text in between. Names are local.
func dump(r *Reader) (string, error) {
	var sb strings.Builder
	for {
		tok, err := r.Token()
		if err == io.EOF {
			return sb.String(), nil
		}
		if err != nil {
			return sb.String(), err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			sb.WriteString("<" + t.Name.Local + ">")
		case xml.EndElement:
			sb.WriteString("</" + t.Name.Local + ">")
		case xml.CharData:
			sb.Write(bytes.TrimSpace(t))
		}
	}
}

func read(t *testing.T, input string, opt Options) (string, error) {
	t.Helper()
	return dump(New(context.Background(), strings.NewReader(input), opt))
}

func TestTokens(t *testing.T) {
	got, err := read(t, `<?xml version="1.0"?><!-- c --><a><b x="1">one</b><?pi?><c/>two</a>`, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if want := "<a><b>one</b><c></c>two</a>"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestAlternateContent(t *testing.T) {
	const head = `<w:r xmlns:w="` + nsW + `" xmlns:mc="` + NSMarkupCompatibility + `" xmlns:wps="` + nsWPS + `" xmlns:x="urn:unknown">`
	understood := func(ns string) bool { return ns == nsWPS }
	tests := []struct {
		name, input, want string
		understood        func(string) bool
	}{
		{"choice understood",
			`<mc:AlternateContent><mc:Choice Requires="wps"><w:new/></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent>`,
			"<new></new>", understood},
		{"choice not understood",
			`<mc:AlternateContent><mc:Choice Requires="x"><w:new/></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent>`,
			"<old></old>", understood},
		{"nothing understood",
			`<mc:AlternateContent><mc:Choice Requires="wps"><w:new/></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent>`,
			"<old></old>", nil},
		{"every required namespace must be understood",
			`<mc:AlternateContent><mc:Choice Requires="wps x"><w:new/></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent>`,
			"<old></old>", understood},
		{"first choice that fits",
			`<mc:AlternateContent><mc:Choice Requires="x"><w:a/></mc:Choice><mc:Choice Requires="wps"><w:b/></mc:Choice><mc:Choice Requires="wps"><w:c/></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent>`,
			"<b></b>", understood},
		{"no fallback",
			`<mc:AlternateContent><mc:Choice Requires="x"><w:new/></mc:Choice></mc:AlternateContent>`,
			"", understood},
		{"unbound prefix",
			`<mc:AlternateContent><mc:Choice Requires="nowhere"><w:new/></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent>`,
			"<old></old>", understood},
		{"nested, both levels resolved",
			`<mc:AlternateContent><mc:Choice Requires="wps"><w:new><mc:AlternateContent><mc:Choice Requires="x"><w:deep/></mc:Choice><mc:Fallback><w:shallow/></mc:Fallback></mc:AlternateContent></w:new></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent>`,
			"<new><shallow></shallow></new>", understood},
		{"nested inside the branch not taken",
			`<mc:AlternateContent><mc:Choice Requires="x"><mc:AlternateContent><mc:Fallback><w:lost/></mc:Fallback></mc:AlternateContent></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent>`,
			"<old></old>", understood},
		{"two in a row",
			`<mc:AlternateContent><mc:Choice Requires="wps"><w:a/></mc:Choice><mc:Fallback><w:b/></mc:Fallback></mc:AlternateContent><mc:AlternateContent><mc:Choice Requires="x"><w:c/></mc:Choice><mc:Fallback><w:d/></mc:Fallback></mc:AlternateContent>`,
			"<a></a><d></d>", understood},
		{"prefix bound on the choice itself",
			`<mc:AlternateContent><mc:Choice xmlns:q="` + nsWPS + `" Requires="q"><w:new/></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent>`,
			"<new></new>", understood},
	}
	for _, tt := range tests {
		got, err := read(t, head+"<w:t>before</w:t>"+tt.input+"<w:t>after</w:t></w:r>", Options{Understood: tt.understood})
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if want := "<r><t>before</t>" + tt.want + "<t>after</t></r>"; got != want {
			t.Errorf("%s:\n got %s\nwant %s", tt.name, got, want)
		}
	}
}

func TestStrictNamespaces(t *testing.T) {
	tests := []struct{ strict, want string }{
		{"http://purl.oclc.org/ooxml/wordprocessingml/main", nsW},
		{"http://purl.oclc.org/ooxml/officeDocument/relationships", "http://schemas.openxmlformats.org/officeDocument/2006/relationships"},
		{"http://purl.oclc.org/ooxml/officeDocument/relationships/officeDocument", "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"},
		{"http://purl.oclc.org/ooxml/officeDocument/relationships/extendedProperties", "http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties"},
		{"http://purl.oclc.org/ooxml/officeDocument/extendedProperties", "http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"},
		{"http://purl.oclc.org/ooxml/officeDocument/math", "http://schemas.openxmlformats.org/officeDocument/2006/math"},
		{"http://purl.oclc.org/ooxml/drawingml/main", "http://schemas.openxmlformats.org/drawingml/2006/main"},
		{"http://purl.oclc.org/ooxml/drawingml/wordprocessingDrawing", "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"},
	}
	for _, tt := range tests {
		if got, ok := Transitional(tt.strict); !ok || got != tt.want {
			t.Errorf("Transitional(%q) = %q, %v; want %q", tt.strict, got, ok, tt.want)
		}
	}
	for _, same := range []string{nsW, "", "http://purl.oclc.org/ooxml/", "urn:other"} {
		if got, ok := Transitional(same); ok || got != same {
			t.Errorf("Transitional(%q) = %q, %v; want it unchanged", same, got, ok)
		}
	}

	r := New(context.Background(), strings.NewReader(
		`<w:document xmlns:w="http://purl.oclc.org/ooxml/wordprocessingml/main" xmlns:r="http://purl.oclc.org/ooxml/officeDocument/relationships"><w:p r:id="x"/></w:document>`), Options{})
	tok, err := r.Token()
	if err != nil {
		t.Fatal(err)
	}
	if el := tok.(xml.StartElement); el.Name.Space != nsW {
		t.Errorf("root namespace is %q", el.Name.Space)
	}
	tok, _ = r.Token()
	if a := tok.(xml.StartElement).Attr[0]; a.Name.Space != "http://schemas.openxmlformats.org/officeDocument/2006/relationships" {
		t.Errorf("attribute namespace is %q", a.Name.Space)
	}
	if !r.Strict() {
		t.Errorf("Strict() is false")
	}
	plain := New(context.Background(), strings.NewReader(`<w:document xmlns:w="`+nsW+`"/>`), Options{})
	dump(plain)
	if plain.Strict() {
		t.Errorf("Strict() is true for a Transitional document")
	}
}

func TestSkipAndDepth(t *testing.T) {
	r := New(context.Background(), strings.NewReader(`<a><b><c><d/></c>text</b><e/></a>`), Options{})
	var seen []string
	for {
		tok, err := r.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if el, ok := tok.(xml.StartElement); ok {
			seen = append(seen, el.Name.Local)
			if el.Name.Local == "b" {
				if r.Depth() != 2 {
					t.Errorf("depth of b is %d", r.Depth())
				}
				if err := r.Skip(); err != nil {
					t.Fatal(err)
				}
				if r.Depth() != 1 {
					t.Errorf("depth after skipping b is %d", r.Depth())
				}
			}
		}
	}
	if got := strings.Join(seen, " "); got != "a b e" {
		t.Errorf("saw %q", got)
	}
}

func TestLimits(t *testing.T) {
	var lim *diag.LimitError

	deep := strings.Repeat("<a>", 50) + strings.Repeat("</a>", 50)
	if _, err := read(t, deep, Options{MaxDepth: 50}); err != nil {
		t.Errorf("depth 50 with a limit of 50: %v", err)
	}
	if _, err := read(t, deep, Options{MaxDepth: 49}); !errors.As(err, &lim) {
		t.Errorf("depth 50 with a limit of 49: got %v, want a LimitError", err)
	}
	// The limit holds inside a part that is being skipped, too.
	r := New(context.Background(), strings.NewReader("<r>"+deep+"</r>"), Options{MaxDepth: 20})
	r.Token()
	if err := r.Skip(); !errors.As(err, &lim) {
		t.Errorf("skipping past the depth limit: got %v, want a LimitError", err)
	}

	for _, input := range []string{
		"<a>" + strings.Repeat("x", 5000) + "</a>",
		`<a b="` + strings.Repeat("x", 5000) + `"/>`,
		"<a><!--" + strings.Repeat("x", 5000) + "--></a>",
	} {
		if _, err := read(t, input, Options{MaxTokenBytes: 6000}); err != nil {
			t.Errorf("a token of 5000 bytes with a limit of 6000: %v", err)
		}
		if _, err := read(t, input, Options{MaxTokenBytes: 4000}); !errors.As(err, &lim) {
			t.Errorf("a token of 5000 bytes with a limit of 4000: got %v, want a LimitError", err)
		}
	}
}

func TestCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := New(ctx, strings.NewReader("<a>"+strings.Repeat("<b/>", 5000)+"</a>"), Options{})
	r.Token()
	cancel()
	_, err := dump(r)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("got %v, want context.Canceled", err)
	}
}

func TestDamaged(t *testing.T) {
	for name, input := range map[string]string{
		"cut short":         "<a><b>text",
		"mismatched tags":   "<a><b></a></b>",
		"undefined entity":  `<!DOCTYPE a [<!ENTITY e "x">]><a>&e;</a>`,
		"invalid UTF-8":     "<a>\xff\xfe</a>",
		"unknown encoding":  `<?xml version="1.0" encoding="EBCDIC"?><a/>`,
		"stray angle":       "<a><</a>",
		"cut in a tag":      "<a><b attr=",
		"nothing but a tag": "<a>",
	} {
		_, err := read(t, input, Options{})
		if !errors.Is(err, diag.ErrDamaged) {
			t.Errorf("%s: got %v, want ErrDamaged", name, err)
		}
	}
	// No input at all is the end of the input, not damage.
	if got, err := read(t, "", Options{}); err != nil || got != "" {
		t.Errorf("empty input: %q, %v", got, err)
	}
}

func TestEncodings(t *testing.T) {
	const doc = `<?xml version="1.0" encoding="UTF-16"?><a>caf` + "\u00e9 \u4e2d \U0001F600" + `</a>`
	const want = "<a>caf\u00e9 \u4e2d \U0001F600</a>"
	encode := func(little, bom bool) []byte {
		var out []byte
		if bom {
			if little {
				out = append(out, 0xFF, 0xFE)
			} else {
				out = append(out, 0xFE, 0xFF)
			}
		}
		for _, u := range utf16.Encode([]rune(doc)) {
			if little {
				out = append(out, byte(u), byte(u>>8))
			} else {
				out = append(out, byte(u>>8), byte(u))
			}
		}
		return out
	}
	inputs := map[string][]byte{
		"UTF-16 little-endian with a mark":    encode(true, true),
		"UTF-16 big-endian with a mark":       encode(false, true),
		"UTF-16 little-endian without a mark": encode(true, false),
		"UTF-16 big-endian without a mark":    encode(false, false),
		"UTF-8 with a mark":                   append([]byte{0xEF, 0xBB, 0xBF}, []byte(`<?xml version="1.0" encoding="UTF-8"?><a>caf`+"\u00e9 \u4e2d \U0001F600"+`</a>`)...),
	}
	for name, input := range inputs {
		got, err := dump(New(context.Background(), bytes.NewReader(input), Options{}))
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v", name, got, err)
		}
	}
	got, err := dump(New(context.Background(), strings.NewReader("<?xml version=\"1.0\" encoding=\"ISO-8859-1\"?><a>caf\xe9</a>"), Options{}))
	if err != nil || got != "<a>caf\u00e9</a>" {
		t.Errorf("ISO-8859-1: got %q, %v", got, err)
	}
	// An odd number of bytes: the stray one is dropped.
	odd := append(encode(true, true), 0x41)
	if got, err := dump(New(context.Background(), bytes.NewReader(odd), Options{})); err != nil || got != want {
		t.Errorf("UTF-16 with a stray byte: got %q, %v", got, err)
	}
}

func FuzzReader(f *testing.F) {
	f.Add([]byte(`<a><b x="1">text</b></a>`), 8)
	f.Add([]byte(`<w:r xmlns:w="`+nsW+`" xmlns:mc="`+NSMarkupCompatibility+`" xmlns:wps="`+nsWPS+`"><mc:AlternateContent><mc:Choice Requires="wps"><w:a/></mc:Choice><mc:Fallback><w:b/></mc:Fallback></mc:AlternateContent></w:r>`), 16)
	f.Add([]byte("\xff\xfe<\x00a\x00/\x00>\x00"), 4)
	f.Add([]byte(`<mc:Choice xmlns:mc="`+NSMarkupCompatibility+`"><mc:Fallback/></mc:Choice>`), 3)
	f.Fuzz(func(t *testing.T, data []byte, depth int) {
		if depth < 1 || depth > 64 {
			depth = 16
		}
		r := New(context.Background(), bytes.NewReader(data), Options{
			MaxDepth:      depth,
			MaxTokenBytes: 4096,
			Understood:    func(ns string) bool { return ns == nsWPS },
		})
		open := 0
		for i := 0; ; i++ {
			tok, err := r.Token()
			if err != nil {
				return
			}
			switch tok.(type) {
			case xml.StartElement:
				open++
				if r.Depth() > depth {
					t.Fatalf("depth %d is past the limit %d", r.Depth(), depth)
				}
				if i%7 == 3 {
					if r.Skip() != nil {
						return
					}
					open--
				}
			case xml.EndElement:
				open--
				if open < 0 {
					t.Fatalf("an end tag without its start tag")
				}
			}
		}
	})
}

// Skipping out of a branch of mc:AlternateContent leaves the reader in step:
// what follows is read as usual, and a later alternate is still resolved.
func TestSkipToAcrossAlternateContent(t *testing.T) {
	const input = `<w:body xmlns:w="` + nsW + `" xmlns:mc="` + NSMarkupCompatibility + `" xmlns:wps="` + nsWPS + `">` +
		`<w:row><mc:AlternateContent><mc:Choice Requires="wps"><w:inner><w:deep/></w:inner></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent><w:lost/></w:row>` +
		`<mc:AlternateContent><mc:Choice Requires="wps"><w:kept/></mc:Choice><mc:Fallback><w:old/></mc:Fallback></mc:AlternateContent></w:body>`
	r := New(context.Background(), strings.NewReader(input), Options{Understood: func(ns string) bool { return ns == nsWPS }})
	var sb strings.Builder
	row := 0
	for {
		tok, err := r.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		switch el := tok.(type) {
		case xml.StartElement:
			sb.WriteString("<" + el.Name.Local + ">")
			if el.Name.Local == "row" {
				row = r.Depth()
			}
			if el.Name.Local == "deep" {
				if err := r.SkipTo(row); err != nil {
					t.Fatal(err)
				}
			}
		case xml.EndElement:
			sb.WriteString("</" + el.Name.Local + ">")
		}
	}
	if got, want := sb.String(), "<body><row><inner><deep><kept></kept></body>"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
