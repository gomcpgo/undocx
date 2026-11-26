package undocx

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/gomcpgo/undocx/internal/diag"
	"github.com/gomcpgo/undocx/internal/opc"
	"github.com/gomcpgo/undocx/internal/sniff"
	"github.com/gomcpgo/undocx/internal/wml"
	"github.com/gomcpgo/undocx/internal/xmlstream"
)

// Limits caps the resources a document may consume. Word files are untrusted
// input, and a small file can ask for a great deal of work: a zip archive
// can expand a thousandfold, and XML can nest without end. A zero field
// selects its default, which no ordinary document comes near.
type Limits struct {
	// MaxParts caps the number of entries in the zip archive. The default
	// is 16,384.
	MaxParts int
	// MaxPartBytes caps the uncompressed size of one part. The default is
	// 256 MiB.
	MaxPartBytes int64
	// MaxTotalBytes caps the uncompressed size of all the parts that are
	// read, taken together. Parts that are not read, such as images nobody
	// asked for, do not count, and a part read twice counts once. The
	// default is 1 GiB.
	MaxTotalBytes int64
	// MaxCompressionRatio caps how many times larger than its stored form
	// a part may be. It is checked for parts above 8 MiB only, because
	// small parts of repetitive XML compress very well and cost little.
	// The default is 250.
	MaxCompressionRatio int
	// MaxDepth caps how deeply XML elements may nest. The default is 256.
	MaxDepth int
	// MaxTokenBytes caps the size of one piece of XML: a run of text, or a
	// tag with its attributes. The default is 32 MiB.
	MaxTokenBytes int
	// MaxWarnings caps how many warnings are kept. The default is 200.
	MaxWarnings int
}

// DefaultLimits returns the limits that a zero Limits selects.
func DefaultLimits() Limits {
	return Limits{}.withDefaults()
}

func (l Limits) withDefaults() Limits {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def64 := func(v *int64, d int64) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&l.MaxParts, 16384)
	def64(&l.MaxPartBytes, 256<<20)
	def64(&l.MaxTotalBytes, 1<<30)
	def(&l.MaxCompressionRatio, 250)
	def(&l.MaxDepth, 256)
	def(&l.MaxTokenBytes, 32<<20)
	def(&l.MaxWarnings, 200)
	return l
}

// Options configures Open.
type Options struct {
	// Limits caps resource use. The zero value selects the defaults.
	Limits Limits
}

// Document is an open Word document. It is not safe for concurrent use by
// multiple goroutines.
type Document struct {
	pkg    *opc.Package
	closer io.Closer
	log    *diag.Log
	lim    Limits
	main   string             // the name of the main document part
	rels   []opc.Relationship // the relationships of the package
	info   Info
	styles *wml.Styles       // nil until the body is first read
	lists  *wml.Numbering    // the list definitions, read with the styles
	links  map[string]string // external hyperlink targets by relationship ID
}

// OpenFile opens the document at path. Close the Document to release the
// file. The name of the file is not used to decide what it is.
func OpenFile(ctx context.Context, path string, opts Options) (*Document, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err == nil && st.IsDir() {
		err = &os.PathError{Op: "open", Path: path, Err: errors.New("is a directory")}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	doc, err := Open(ctx, f, st.Size(), opts)
	if err != nil {
		f.Close()
		return nil, err
	}
	doc.closer = f
	return doc, nil
}

// Open reads a document from r, which must stay readable until the Document
// is closed. Only the structure of the package and the document properties
// are read up front.
//
// A file that is not a .docx gives a *FormatError, a password-protected one
// ErrEncrypted, and one that cannot be read ErrDamaged. Missing or malformed
// parts that the document can be read without are reported through Warnings.
func Open(ctx context.Context, r io.ReaderAt, size int64, opts Options) (*Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lim := opts.Limits.withDefaults()
	log := diag.NewLog(lim.MaxWarnings)

	format := sniff.Detect(r, size)
	switch format {
	case sniff.Encrypted:
		return nil, ErrEncrypted
	case sniff.Zip, sniff.Unknown:
		// Unknown may still be a zip archive with other data in front.
	default:
		return nil, &FormatError{Format: format}
	}

	pkg, err := opc.Open(ctx, r, size, opc.Limits{
		MaxParts:      lim.MaxParts,
		MaxPartBytes:  lim.MaxPartBytes,
		MaxTotalBytes: lim.MaxTotalBytes,
		MaxRatio:      lim.MaxCompressionRatio,
		XML:           xmlstream.Options{MaxDepth: lim.MaxDepth, MaxTokenBytes: lim.MaxTokenBytes},
	}, log)
	if err != nil {
		if errors.Is(err, opc.ErrNotZip) {
			if format == sniff.Zip {
				// It starts like a zip archive and has no usable
				// directory: cut short, most likely.
				return nil, diag.Damaged("the zip archive cannot be read: %v", errors.Unwrap(err))
			}
			return nil, &FormatError{}
		}
		return nil, err
	}

	d := &Document{pkg: pkg, log: log, lim: lim}
	if err := d.findMain(ctx); err != nil {
		return nil, err
	}
	d.readProperties(ctx)
	return d, nil
}

// Close releases the resources held by the Document. It closes the
// underlying file only when the Document was opened with OpenFile.
func (d *Document) Close() error {
	if d.closer == nil {
		return nil
	}
	err := d.closer.Close()
	d.closer = nil
	return err
}

// Warnings returns the problems noticed so far. The list grows as more of
// the document is read.
func (d *Document) Warnings() []Warning {
	return d.log.List()
}

const (
	nsWML = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"

	relOfficeDocument = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument"
	defaultMainPart   = "word/document.xml"
)

// The content types of the main part say which kind of Word file this is.
var mainTypes = map[string]string{
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml": "docx",
	"application/vnd.ms-word.document.macroenabled.main+xml":                           "docm",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.template.main+xml": "dotx",
	"application/vnd.ms-word.template.macroenabledtemplate.main+xml":                   "dotm",
}

// findMain locates the main document part and checks that it is one: its
// content type and its root element both have to say so. A package that is
// some other kind of file gives a *FormatError.
func (d *Document) findMain(ctx context.Context) error {
	// The package relationships name the main part (ECMA-376 Part 1,
	// 11.3.10); its name is not fixed.
	var err error
	d.rels, err = d.pkg.Relationships(ctx, "")
	if err != nil {
		if stop(err) {
			return err
		}
		d.log.Add(WarnRelationships, "the package relationships could not be read: %v", err)
	}
	var other string // the content type of a main part that is not a Word document
	for _, r := range d.rels {
		if r.Type != relOfficeDocument || r.External || !d.pkg.Has(r.Part) {
			continue
		}
		ct := strings.ToLower(d.pkg.ContentType(r.Part))
		if format, ok := mainTypes[ct]; ok {
			d.main, d.info.Format = r.Part, format
			break
		}
		if foreign(ct) != "" {
			other = ct
			continue
		}
		// No usable content type. The root element decides below.
		if d.main == "" {
			d.main = r.Part
		}
	}

	if d.main == "" && other == "" {
		// The relationships are missing or do not lead anywhere. Look
		// for a part with the content type of a main part, then for the
		// usual name.
		for _, name := range d.pkg.Names() {
			if format, ok := mainTypes[strings.ToLower(d.pkg.ContentType(name))]; ok {
				d.main, d.info.Format = name, format
				break
			}
		}
		if d.main == "" && d.pkg.Has(defaultMainPart) && foreign(strings.ToLower(d.pkg.ContentType(defaultMainPart))) == "" {
			d.main = defaultMainPart
		}
		if d.main != "" {
			d.log.Add(WarnMainPart, "the package does not name its main part; using %s", d.main)
		}
	}
	if d.main == "" {
		return &FormatError{Format: d.packageKind(other)}
	}

	// The root element must be w:document, in either conformance class.
	x, c, err := d.pkg.XML(ctx, d.main, nil)
	if err != nil {
		if errors.Is(err, opc.ErrEncryptedPart) {
			return ErrEncrypted
		}
		return err
	}
	defer c.Close()
	for {
		tok, err := x.Token()
		if err == io.EOF {
			return diag.Damaged("the main part %s is empty", d.main)
		}
		if err != nil {
			return err
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if el.Name.Local != "document" || el.Name.Space != nsWML {
			if d.info.Format == "" {
				return &FormatError{Format: d.packageKind(other)}
			}
			return diag.Damaged("the main part %s is not a Word document: its root element is %s", d.main, el.Name.Local)
		}
		break
	}
	d.info.Strict = x.Strict()
	if d.info.Format == "" {
		d.info.Format = "docx"
		d.log.Add(WarnContentTypes, "the main part %s has no content type; reading it as a .docx", d.main)
	}
	return nil
}

// stop reports whether an error met while reading an optional part must end
// the work: a limit, or the caller giving up. Anything else becomes a
// warning.
func stop(err error) bool {
	var lim *LimitError
	return errors.As(err, &lim) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// foreign names the kind of file a main part's content type belongs to, when
// it is not a Word document. ct must be in lower case.
func foreign(ct string) string {
	switch {
	case strings.Contains(ct, "spreadsheetml"), strings.Contains(ct, "ms-excel"):
		return FormatXLSX
	case strings.Contains(ct, "presentationml"), strings.Contains(ct, "ms-powerpoint"):
		return FormatPPTX
	}
	return ""
}

// packageKind works out what a zip archive is, once it is clear that it is
// not a Word document. mainType is the content type of its main part, if it
// has one.
func (d *Document) packageKind(mainType string) string {
	if kind := foreign(mainType); kind != "" {
		return kind
	}
	for _, kind := range []string{FormatXLSX, FormatPPTX} {
		if d.pkg.DeclaresType(func(ct string) bool { return foreign(strings.ToLower(ct)) == kind }) {
			return kind
		}
	}
	// OpenDocument and EPUB files start with an entry that names their type.
	if d.pkg.Has("mimetype") {
		if data, err := d.pkg.ReadAll("mimetype", 256); err == nil {
			switch mt := strings.TrimSpace(string(data)); {
			case strings.HasPrefix(mt, "application/vnd.oasis.opendocument"):
				return FormatOpenDoc
			case mt == "application/epub+zip":
				return FormatEPUB
			}
		}
	}
	return FormatZip
}
