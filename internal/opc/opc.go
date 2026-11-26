// Package opc reads an Open Packaging Conventions package: the zip archive
// that a .docx, .xlsx or .pptx is, with its content types and relationships
// (ECMA-376 Part 2). It knows nothing about Word.
//
// The archive is untrusted. Its parts are read only on request and through
// limits; nothing is ever written to disk, and a relationship is only ever
// resolved to the name of a part inside the package.
package opc

import (
	"archive/zip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/gomcpgo/undocx/internal/diag"
	"github.com/gomcpgo/undocx/internal/xmlstream"
)

// Errors that Open and the Package methods return.
var (
	// ErrNotZip means the input is not a zip archive at all.
	ErrNotZip = errors.New("opc: not a zip archive")
	// ErrNoPart means the package has no part of the name asked for.
	ErrNoPart = errors.New("opc: no such part")
	// ErrEncryptedPart means the part is stored with zip encryption.
	ErrEncryptedPart = errors.New("opc: the part is encrypted")
)

// Warning codes.
const (
	WarnPartName      = "part-name"
	WarnDuplicatePart = "duplicate-part"
	WarnContentTypes  = "content-types"
	WarnRelationships = "relationships"
)

// Limits caps what a package may make the reader do.
type Limits struct {
	// MaxParts caps the number of entries in the archive.
	MaxParts int
	// MaxPartBytes caps the uncompressed size of one part.
	MaxPartBytes int64
	// MaxTotalBytes caps the uncompressed bytes of all the parts that are
	// read. A part read twice counts once.
	MaxTotalBytes int64
	// MaxRatio caps how many times larger than its stored form a part may
	// be. It applies to parts above RatioFloor only: small parts of
	// repetitive XML compress very well and cost nothing.
	MaxRatio int
	// XML limits the XML of the package's own parts.
	XML xmlstream.Options
}

// RatioFloor is the uncompressed size below which a part's compression ratio
// is not checked.
const RatioFloor = 8 << 20

// Relationship is a typed link from a part, or from the package itself, to
// another part or to something outside the package.
type Relationship struct {
	ID string
	// Type is the relationship type, in its Transitional spelling.
	Type string
	// Target is the target as written.
	Target string
	// External reports that the target is outside the package. It is
	// never fetched.
	External bool
	// Part is the name of the target part, for use with Open. It is empty
	// for an external target and for one that cannot name a part.
	Part string
}

// Package is an open package.
type Package struct {
	zr        *zip.Reader
	parts     map[string]*zip.File
	lim       Limits
	log       *diag.Log
	defaults  map[string]string
	overrides map[string]string
	charged   map[*zip.File]int64
	total     int64
}

// Open reads the directory of the archive and its content types. No other
// part is read. Problems that do not prevent reading go to log.
func Open(ctx context.Context, r io.ReaderAt, size int64, lim Limits, log *diag.Log) (*Package, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil && !(errors.Is(err, zip.ErrInsecurePath) && zr != nil) {
		// Insecure names are no danger here, since no entry is ever
		// written to disk under its name.
		return nil, fmt.Errorf("%w: %v", ErrNotZip, err)
	}
	if len(zr.File) > lim.MaxParts {
		return nil, &diag.LimitError{What: "number of parts", Limit: int64(lim.MaxParts)}
	}
	p := &Package{
		zr:        zr,
		parts:     make(map[string]*zip.File, len(zr.File)),
		lim:       lim,
		log:       log,
		defaults:  map[string]string{},
		overrides: map[string]string{},
		charged:   map[*zip.File]int64{},
	}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") || strings.HasSuffix(f.Name, `\`) {
			continue // a directory entry
		}
		name, ok := PartName(f.Name)
		if !ok {
			log.Add(WarnPartName, "the entry %q is not a valid part name and was ignored", clip(f.Name))
			continue
		}
		if _, dup := p.parts[name]; dup {
			log.Add(WarnDuplicatePart, "the part %q is stored more than once; the first copy is used", clip(f.Name))
			continue
		}
		p.parts[name] = f
	}
	if err := p.readContentTypes(ctx); err != nil {
		if fatal(err) {
			return nil, err
		}
		log.Add(WarnContentTypes, "the content types could not be read: %v", err)
	}
	return p, nil
}

// fatal reports whether an error from reading one of the package's own parts
// must stop the work, as opposed to being worked around.
func fatal(err error) bool {
	var lim *diag.LimitError
	return errors.As(err, &lim) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// PartName returns the form of a part name that parts are looked up by:
// lower case, without a leading slash, with forward slashes and with percent
// escapes decoded. Part names compare without regard to case (ECMA-376 Part
// 2, on part name equivalence). It reports false for a name with an empty,
// "." or ".." segment, which no part may have.
func PartName(name string) (string, bool) {
	name = strings.ReplaceAll(name, `\`, "/")
	name = strings.TrimLeft(name, "/")
	name = strings.ToLower(unescape(name))
	if name == "" {
		return "", false
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return name, true
}

// unescape decodes %XX escapes, leaving anything malformed as it is.
func unescape(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if hi, ok1 := unhex(s[i+1]); ok1 {
				if lo, ok2 := unhex(s[i+2]); ok2 {
					sb.WriteByte(hi<<4 | lo)
					i += 2
					continue
				}
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// Has reports whether the package holds the part.
func (p *Package) Has(part string) bool {
	_, ok := p.parts[part]
	return ok
}

// Names returns the names of all parts, sorted.
func (p *Package) Names() []string {
	names := make([]string, 0, len(p.parts))
	for name := range p.parts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Size returns the uncompressed size of the part as the archive declares
// it, or -1 when there is no such part.
func (p *Package) Size(part string) int64 {
	f, ok := p.parts[part]
	if !ok {
		return -1
	}
	return int64(min(f.UncompressedSize64, 1<<62))
}

// Open returns the content of a part. The reader fails with a
// *diag.LimitError when the part or the package runs past a limit, and with
// an error matching diag.ErrDamaged when the stored data is corrupt.
func (p *Package) Open(part string) (io.ReadCloser, error) {
	f, ok := p.parts[part]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoPart, part)
	}
	if f.Flags&1 != 0 {
		return nil, fmt.Errorf("%w: %s", ErrEncryptedPart, part)
	}
	if f.UncompressedSize64 > uint64(p.lim.MaxPartBytes) {
		return nil, &diag.LimitError{What: "size of the part " + part, Limit: p.lim.MaxPartBytes}
	}
	if p.lim.MaxRatio > 0 && f.UncompressedSize64 > RatioFloor && f.UncompressedSize64/uint64(p.lim.MaxRatio) > f.CompressedSize64 {
		return nil, &diag.LimitError{What: "compression ratio of the part " + part, Limit: int64(p.lim.MaxRatio)}
	}
	rc, err := f.Open()
	if err != nil {
		return nil, diag.Damaged("part %s: %v", part, err)
	}
	return &partReader{p: p, f: f, rc: rc, name: part}, nil
}

// partReader counts what is read from a part against the limits.
type partReader struct {
	p    *Package
	f    *zip.File
	rc   io.ReadCloser
	name string
	n    int64
	err  error
}

func (r *partReader) Read(b []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.rc.Read(b)
	r.n += int64(n)
	if r.n > r.p.lim.MaxPartBytes {
		r.err = &diag.LimitError{What: "size of the part " + r.name, Limit: r.p.lim.MaxPartBytes}
		return 0, r.err
	}
	// Charge the package total for what this part had not been read to
	// before, so that reading a part again is free.
	if extra := r.n - r.p.charged[r.f]; extra > 0 {
		r.p.charged[r.f] = r.n
		r.p.total += extra
		if r.p.total > r.p.lim.MaxTotalBytes {
			r.err = &diag.LimitError{What: "total size of the parts read", Limit: r.p.lim.MaxTotalBytes}
			return 0, r.err
		}
	}
	if err != nil && err != io.EOF {
		r.err = diag.Damaged("part %s: %v", r.name, err)
		return n, r.err
	}
	return n, err
}

func (r *partReader) Close() error { return r.rc.Close() }

// ReadAll returns the whole content of a part that is expected to be small,
// failing with a *diag.LimitError when it is larger than max.
func (p *Package) ReadAll(part string, max int64) ([]byte, error) {
	if size := p.Size(part); size > max {
		return nil, &diag.LimitError{What: "size of the part " + part, Limit: max}
	}
	rc, err := p.Open(part)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, &diag.LimitError{What: "size of the part " + part, Limit: max}
	}
	return data, nil
}

// XML returns a token reader over a part, with the package's XML limits.
// understood says which namespaces an mc:Choice may require; see
// xmlstream.Options. Close the returned closer when done.
func (p *Package) XML(ctx context.Context, part string, understood func(string) bool) (*xmlstream.Reader, io.Closer, error) {
	rc, err := p.Open(part)
	if err != nil {
		return nil, nil, err
	}
	opt := p.lim.XML
	opt.Understood = understood
	return xmlstream.New(ctx, rc, opt), rc, nil
}

const contentTypesPart = "[content_types].xml"

// readContentTypes loads [Content_Types].xml: a content type for each file
// extension, and overrides for single parts.
func (p *Package) readContentTypes(ctx context.Context) error {
	if !p.Has(contentTypesPart) {
		return errors.New("the package has no [Content_Types].xml")
	}
	x, c, err := p.XML(ctx, contentTypesPart, nil)
	if err != nil {
		return err
	}
	defer c.Close()
	for {
		tok, err := x.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch el.Name.Local {
		case "Default":
			if ext := strings.ToLower(attr(el, "Extension")); ext != "" {
				if _, dup := p.defaults[ext]; !dup {
					p.defaults[ext] = attr(el, "ContentType")
				}
			}
		case "Override":
			if name, ok := PartName(attr(el, "PartName")); ok {
				if _, dup := p.overrides[name]; !dup {
					p.overrides[name] = attr(el, "ContentType")
				}
			}
		}
	}
}

// ContentType returns the content type of a part, or "" when the package
// does not give one.
func (p *Package) ContentType(part string) string {
	if ct, ok := p.overrides[part]; ok {
		return ct
	}
	if i := strings.LastIndexByte(part, '.'); i >= 0 && !strings.Contains(part[i:], "/") {
		return p.defaults[part[i+1:]]
	}
	return ""
}

// DeclaresType reports whether any content type the package declares, used
// by a part or not, satisfies match. It is for working out what kind of
// package this is when its relationships do not say.
func (p *Package) DeclaresType(match func(contentType string) bool) bool {
	for _, ct := range p.overrides {
		if match(ct) {
			return true
		}
	}
	for _, ct := range p.defaults {
		if match(ct) {
			return true
		}
	}
	return false
}

// RelsPart returns the name of the part that holds the relationships of
// source. An empty source stands for the package itself.
func RelsPart(source string) string {
	dir, file := path.Split(source)
	return dir + "_rels/" + file + ".rels"
}

// Relationships returns the relationships of a part, or of the package when
// source is empty. A part with no relationships part has none.
func (p *Package) Relationships(ctx context.Context, source string) ([]Relationship, error) {
	part := RelsPart(source)
	if !p.Has(part) {
		return nil, nil
	}
	x, c, err := p.XML(ctx, part, nil)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	var rels []Relationship
	for {
		tok, err := x.Token()
		if err == io.EOF {
			return rels, nil
		}
		if err != nil {
			return rels, err
		}
		el, ok := tok.(xml.StartElement)
		if !ok || el.Name.Local != "Relationship" {
			continue
		}
		r := Relationship{ID: attr(el, "Id"), Target: attr(el, "Target")}
		r.Type, _ = xmlstream.Transitional(attr(el, "Type"))
		r.External = strings.EqualFold(attr(el, "TargetMode"), "External")
		if !r.External {
			r.Part, _ = Resolve(source, r.Target)
		}
		rels = append(rels, r)
	}
}

// Resolve returns the name of the part that a relationship target written in
// the part source refers to. The target is a relative reference: it is
// resolved against the folder of source, or against the root when it starts
// with a slash. Steps above the root stay at the root, so the result is
// always a name inside the package; whether such a part exists is for the
// caller to check.
func Resolve(source, target string) (string, bool) {
	target = strings.ReplaceAll(target, `\`, "/")
	if i := strings.IndexAny(target, "#?"); i >= 0 {
		target = target[:i]
	}
	if target == "" {
		return "", false
	}
	if !strings.HasPrefix(target, "/") {
		target = "/" + path.Dir(source) + "/" + target
	}
	return PartName(path.Clean(target))
}

func attr(el xml.StartElement, local string) string {
	for _, a := range el.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// clip shortens a name taken from the file for use in a message.
func clip(s string) string {
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return strings.ToValidUTF8(s, "�")
}
