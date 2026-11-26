// Package xmlstream reads the XML of a package part as a stream of tokens,
// with the limits and the clean-up that every part needs:
//
//   - nesting depth and the size of a single token are capped;
//   - the read stops when its context is cancelled;
//   - the namespaces of the Strict conformance class are replaced by their
//     Transitional equivalents, so that callers know one set of names;
//   - mc:AlternateContent is resolved to one of its branches, so that content
//     stored twice for old and new readers is seen once.
//
// It knows nothing about any particular document format.
package xmlstream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/gomcpgo/undocx/internal/diag"
)

// NSMarkupCompatibility is the namespace of mc:AlternateContent, mc:Choice
// and mc:Fallback (ECMA-376 Part 3).
const NSMarkupCompatibility = "http://schemas.openxmlformats.org/markup-compatibility/2006"

// Options sets the limits of a Reader.
type Options struct {
	// MaxDepth caps how deeply elements may nest.
	MaxDepth int
	// MaxTokenBytes caps the size of one text node or one tag with its
	// attributes.
	MaxTokenBytes int
	// Understood reports whether the caller handles the elements of a
	// namespace. An mc:Choice is taken only when every namespace it
	// requires is understood; otherwise the mc:Fallback is. With a nil
	// Understood the fallback is always taken.
	Understood func(namespace string) bool
}

// Reader reads tokens from one part.
type Reader struct {
	ctx    context.Context
	dec    *xml.Decoder
	guard  *guard
	opt    Options
	depth  int
	count  int
	strict bool

	// scope holds the namespace prefixes in force, innermost last.
	scope []binding
	// alternates holds the open mc:AlternateContent elements.
	alternates []alternate
	// hidden holds the depths of the open elements that were not passed on
	// to the caller: mc:AlternateContent and the branch taken from it.
	hidden []int
}

type binding struct {
	depth       int
	prefix, uri string
}

type alternate struct {
	depth int
	taken bool
}

// New returns a Reader over r.
func New(ctx context.Context, r io.Reader, opt Options) *Reader {
	if opt.MaxDepth <= 0 {
		opt.MaxDepth = 256
	}
	if opt.MaxTokenBytes <= 0 {
		opt.MaxTokenBytes = 32 << 20
	}
	g := &guard{max: opt.MaxTokenBytes}
	g.r = decode(r)
	dec := xml.NewDecoder(g)
	dec.CharsetReader = charsetReader
	return &Reader{ctx: ctx, dec: dec, guard: g, opt: opt}
}

// Strict reports whether a namespace of the Strict conformance class has
// been seen so far.
func (r *Reader) Strict() bool { return r.strict }

// Depth returns the nesting depth of the last element returned: 1 for the
// root.
func (r *Reader) Depth() int { return r.depth }

// Token returns the next start tag, end tag or text. Comments, processing
// instructions and directives are passed over. The elements of
// mc:AlternateContent are not returned; the content of the branch taken
// appears in their place.
//
// A returned xml.CharData is valid only until the next call. At the end of
// the input Token returns io.EOF. Malformed XML gives an error that matches
// diag.ErrDamaged, and a limit a *diag.LimitError.
func (r *Reader) Token() (xml.Token, error) {
	for {
		tok, err := r.raw()
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if t.Name.Space == NSMarkupCompatibility {
				if err := r.compatibility(t); err != nil {
					return nil, err
				}
				continue
			}
			r.normalise(&t)
			return t, nil
		case xml.EndElement:
			if n := len(r.hidden); n > 0 && r.hidden[n-1] == r.depth+1 {
				r.hidden = r.hidden[:n-1]
				if m := len(r.alternates); m > 0 && r.alternates[m-1].depth == r.depth+1 {
					r.alternates = r.alternates[:m-1]
				}
				continue
			}
			t.Name.Space = r.mapNS(t.Name.Space)
			return t, nil
		case xml.CharData:
			return t, nil
		}
	}
}

// Skip reads to the end of the element whose start tag Token returned last.
func (r *Reader) Skip() error {
	return r.skipFrom(r.depth)
}

// skipFrom reads until the element at the given depth has ended.
func (r *Reader) skipFrom(depth int) error {
	for r.depth >= depth {
		if _, err := r.raw(); err != nil {
			if err == io.EOF {
				return diag.Damaged("XML ends inside an element")
			}
			return err
		}
	}
	return nil
}

// raw returns the next token of the decoder, keeping the depth and the
// namespace scope up to date and applying the limits.
func (r *Reader) raw() (xml.Token, error) {
	r.count++
	if r.count&0x3ff == 0 {
		if err := r.ctx.Err(); err != nil {
			return nil, err
		}
	}
	tok, err := r.dec.Token()
	if err != nil {
		if err == io.EOF {
			if r.depth > 0 {
				return nil, diag.Damaged("XML ends inside an element")
			}
			return nil, io.EOF
		}
		var lim *diag.LimitError
		if errors.As(err, &lim) {
			return nil, lim
		}
		if r.guard.err != nil {
			return nil, r.guard.err
		}
		return nil, diag.Damaged("%v", err)
	}
	switch t := tok.(type) {
	case xml.StartElement:
		r.depth++
		if r.depth > r.opt.MaxDepth {
			return nil, &diag.LimitError{What: "XML nesting depth", Limit: int64(r.opt.MaxDepth)}
		}
		for _, a := range t.Attr {
			if a.Name.Space == "xmlns" {
				r.scope = append(r.scope, binding{r.depth, a.Name.Local, a.Value})
			}
		}
	case xml.EndElement:
		for n := len(r.scope); n > 0 && r.scope[n-1].depth == r.depth; n = len(r.scope) {
			r.scope = r.scope[:n-1]
		}
		r.depth--
	}
	return tok, nil
}

// compatibility handles a start tag in the markup-compatibility namespace.
// The tag is never passed on: either its content is, or it is skipped whole.
func (r *Reader) compatibility(t xml.StartElement) error {
	switch t.Name.Local {
	case "AlternateContent":
		r.alternates = append(r.alternates, alternate{depth: r.depth})
		r.hidden = append(r.hidden, r.depth)
		return nil
	case "Choice", "Fallback":
		take := true
		if n := len(r.alternates); n > 0 && r.alternates[n-1].depth == r.depth-1 {
			alt := &r.alternates[n-1]
			take = !alt.taken && (t.Name.Local == "Fallback" || r.understands(t))
			alt.taken = alt.taken || take
		}
		if take {
			r.hidden = append(r.hidden, r.depth)
			return nil
		}
	}
	return r.skipFrom(r.depth)
}

// understands reports whether every namespace named by the Requires
// attribute of an mc:Choice is understood. Requires lists prefixes, which
// stand for the namespaces bound to them where the element is.
func (r *Reader) understands(t xml.StartElement) bool {
	if r.opt.Understood == nil {
		return false
	}
	requires := ""
	for _, a := range t.Attr {
		if a.Name.Local == "Requires" {
			requires = a.Value
		}
	}
	prefixes := strings.Fields(requires)
	if len(prefixes) == 0 {
		return false
	}
	for _, p := range prefixes {
		uri := ""
		for i := len(r.scope) - 1; i >= 0; i-- {
			if r.scope[i].prefix == p {
				uri = r.scope[i].uri
				break
			}
		}
		if uri == "" || !r.opt.Understood(r.mapNS(uri)) {
			return false
		}
	}
	return true
}

func (r *Reader) normalise(t *xml.StartElement) {
	t.Name.Space = r.mapNS(t.Name.Space)
	for i := range t.Attr {
		if s := t.Attr[i].Name.Space; s != "" && s != "xmlns" {
			t.Attr[i].Name.Space = r.mapNS(s)
		}
	}
}

func (r *Reader) mapNS(ns string) string {
	if mapped, ok := Transitional(ns); ok {
		r.strict = true
		return mapped
	}
	return ns
}

const (
	strictPrefix       = "http://purl.oclc.org/ooxml/"
	transitionalPrefix = "http://schemas.openxmlformats.org/"
)

// Transitional returns the Transitional namespace or relationship type that
// corresponds to one of the Strict conformance class, and whether uri was a
// Strict one. Strict names look like http://purl.oclc.org/ooxml/AREA/REST and
// their counterparts like http://schemas.openxmlformats.org/AREA/2006/REST
// (ECMA-376 Part 1, annex on namespaces). A few properties namespaces are
// also spelled differently.
func Transitional(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, strictPrefix)
	if !ok {
		return uri, false
	}
	area, tail, ok := strings.Cut(rest, "/")
	if !ok {
		return uri, false
	}
	// Both as a namespace (AREA/NAME) and as the last step of a
	// relationship type (AREA/relationships/NAME).
	head, last := "", tail
	if i := strings.LastIndexByte(tail, '/'); i >= 0 {
		head, last = tail[:i+1], tail[i+1:]
	}
	switch last {
	case "extendedProperties":
		last = "extended-properties"
	case "customProperties":
		last = "custom-properties"
	}
	return transitionalPrefix + area + "/2006/" + head + last, true
}

// guard passes bytes through and fails when one token grows past its limit.
// A token is the bytes between two angle brackets: a text node, or a tag with
// its attributes. The decoder would otherwise hold all of it in memory.
type guard struct {
	r   io.Reader
	max int
	run int
	err error
}

func (g *guard) Read(p []byte) (int, error) {
	if g.err != nil {
		return 0, g.err
	}
	n, err := g.r.Read(p)
	for b := p[:n]; len(b) > 0; {
		i := bytes.IndexAny(b, "<>")
		if i < 0 {
			g.run += len(b)
			break
		}
		g.run += i
		if g.run > g.max {
			break
		}
		g.run = 0
		b = b[i+1:]
	}
	if g.run > g.max {
		g.err = &diag.LimitError{What: "size of one XML token", Limit: int64(g.max)}
		return 0, g.err
	}
	return n, err
}

// decode returns r as UTF-8. A package part is UTF-8 or UTF-16 (ECMA-376
// Part 2); UTF-16 is recognised by its byte order mark, or by the zero bytes
// around the first angle bracket, and converted.
func decode(r io.Reader) io.Reader {
	br := bufio.NewReaderSize(r, 32<<10)
	head, _ := br.Peek(4)
	switch {
	case bytes.HasPrefix(head, []byte{0xEF, 0xBB, 0xBF}):
		br.Discard(3)
		return br
	case bytes.HasPrefix(head, []byte{0xFF, 0xFE}):
		br.Discard(2)
		return &utf16Reader{r: br, little: true}
	case bytes.HasPrefix(head, []byte{0xFE, 0xFF}):
		br.Discard(2)
		return &utf16Reader{r: br}
	case len(head) >= 2 && head[0] == '<' && head[1] == 0:
		return &utf16Reader{r: br, little: true}
	case len(head) >= 2 && head[0] == 0 && head[1] == '<':
		return &utf16Reader{r: br}
	}
	return br
}

// utf16Reader converts UTF-16 to UTF-8.
type utf16Reader struct {
	r       *bufio.Reader
	little  bool
	pending []byte
	err     error
}

func (u *utf16Reader) unit() (uint16, error) {
	var b [2]byte
	if _, err := io.ReadFull(u.r, b[:]); err != nil {
		if err == io.ErrUnexpectedEOF {
			err = io.EOF // a stray last byte is dropped
		}
		return 0, err
	}
	if u.little {
		return uint16(b[0]) | uint16(b[1])<<8, nil
	}
	return uint16(b[1]) | uint16(b[0])<<8, nil
}

func (u *utf16Reader) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if len(u.pending) > 0 {
			c := copy(p[n:], u.pending)
			u.pending = u.pending[c:]
			n += c
			continue
		}
		if u.err != nil {
			break
		}
		c, err := u.unit()
		if err != nil {
			u.err = err
			break
		}
		r := rune(c)
		if utf16.IsSurrogate(r) {
			c2, err := u.unit()
			if err != nil {
				u.err = err
				r = utf8.RuneError
			} else {
				r = utf16.DecodeRune(r, rune(c2))
			}
		}
		u.pending = utf8.AppendRune(u.pending[:0], r)
	}
	if n == 0 && u.err != nil {
		return 0, u.err
	}
	return n, nil
}

// charsetReader is called by the decoder when the XML declaration names an
// encoding other than UTF-8. UTF-16 input has been converted by then, so its
// label is accepted as it is.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "utf-8", "utf8", "utf-16", "utf-16le", "utf-16be", "utf16", "us-ascii", "ascii":
		return input, nil
	case "iso-8859-1", "latin1", "iso8859-1":
		return &latin1Reader{r: bufio.NewReader(input)}, nil
	}
	return nil, fmt.Errorf("unsupported encoding %q", label)
}

// latin1Reader converts ISO-8859-1, where every byte is the code point of
// the same number.
type latin1Reader struct {
	r *bufio.Reader
}

func (l *latin1Reader) Read(p []byte) (int, error) {
	if len(p) < 2 {
		return 0, io.ErrShortBuffer
	}
	n := 0
	for n+2 <= len(p) {
		b, err := l.r.ReadByte()
		if err != nil {
			if n > 0 {
				return n, nil
			}
			return 0, err
		}
		n += utf8.EncodeRune(p[n:], rune(b))
	}
	return n, nil
}
