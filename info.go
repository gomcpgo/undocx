package undocx

import (
	"context"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
	"time"
)

// Info is a document's metadata: what kind of file it is, and the properties
// stored with it. The properties are whatever the last program to save the
// file wrote. Nothing is counted or checked against the content, so Words and
// Pages can be stale or absent.
type Info struct {
	// Format is "docx" for a document, "docm" for a macro-enabled one,
	// "dotx" for a template and "dotm" for a macro-enabled template. It
	// comes from the content of the file, not from its name.
	Format string
	// Strict reports that the document uses the Strict conformance class
	// of the standard, which names its elements differently. It is read
	// the same way.
	Strict bool

	Title       string
	Author      string
	Subject     string
	Keywords    string
	Description string
	Category    string
	// LastModifiedBy is the name of the person who saved the file last.
	LastModifiedBy string
	// Revision is the number of times the document has been saved, as
	// Word counts it. It is 0 when the file does not say.
	Revision int
	// Created, Modified and LastPrinted are zero when the file does not
	// say.
	Created     time.Time
	Modified    time.Time
	LastPrinted time.Time

	// Application is the program that saved the file, such as "Microsoft
	// Office Word", and AppVersion its version.
	Application string
	AppVersion  string
	// Template is the name of the template the document is attached to.
	Template string
	Company  string
	// Pages, Words, Characters and Paragraphs are the counts the saving
	// program recorded. They are 0 when the file does not say.
	Pages      int
	Words      int
	Characters int
	Paragraphs int
}

// Info returns the document's metadata.
func (d *Document) Info() Info {
	return d.info
}

const (
	// maxPropertyBytes caps the length of one property value kept.
	maxPropertyBytes = 64 << 10

	defaultCorePart = "docprops/core.xml"
	defaultAppPart  = "docprops/app.xml"
)

// readProperties fills d.info from the core and the extended properties
// parts. Neither is required, and a problem with either is a warning.
func (d *Document) readProperties(ctx context.Context) {
	core, app := "", ""
	for _, r := range d.rels {
		if r.External || !d.pkg.Has(r.Part) {
			continue
		}
		switch {
		case strings.HasSuffix(r.Type, "/metadata/core-properties") && core == "":
			core = r.Part
		case strings.HasSuffix(r.Type, "/extended-properties") && app == "":
			app = r.Part
		}
	}
	if core == "" && d.pkg.Has(defaultCorePart) {
		core = defaultCorePart
	}
	if app == "" && d.pkg.Has(defaultAppPart) {
		app = defaultAppPart
	}

	i := &d.info
	if core != "" {
		d.properties(ctx, core, func(name, value string) {
			switch name {
			case "title":
				i.Title = value
			case "creator":
				i.Author = value
			case "subject":
				i.Subject = value
			case "keywords":
				i.Keywords = value
			case "description":
				i.Description = value
			case "category":
				i.Category = value
			case "lastModifiedBy":
				i.LastModifiedBy = value
			case "revision":
				i.Revision = count(value)
			case "created":
				i.Created = parseTime(value)
			case "modified":
				i.Modified = parseTime(value)
			case "lastPrinted":
				i.LastPrinted = parseTime(value)
			}
		})
	}
	if app != "" {
		d.properties(ctx, app, func(name, value string) {
			switch name {
			case "Application":
				i.Application = value
			case "AppVersion":
				i.AppVersion = value
			case "Template":
				i.Template = value
			case "Company":
				i.Company = value
			case "Pages":
				i.Pages = count(value)
			case "Words":
				i.Words = count(value)
			case "Characters":
				i.Characters = count(value)
			case "Paragraphs":
				i.Paragraphs = count(value)
			}
		})
	}
}

// properties reads a part whose root element holds one child element per
// property, and calls set with the local name and the text of each. The
// first value of a property wins.
func (d *Document) properties(ctx context.Context, part string, set func(name, value string)) {
	x, c, err := d.pkg.XML(ctx, part, nil)
	if err != nil {
		d.log.Add(WarnMetadata, "the properties in %s could not be read: %v", part, err)
		return
	}
	defer c.Close()
	seen := map[string]bool{}
	var name string
	var value strings.Builder
	for {
		tok, err := x.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			d.log.Add(WarnMetadata, "the properties in %s could not be read in full: %v", part, err)
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if x.Depth() == 2 {
				name = t.Name.Local
				value.Reset()
			}
		case xml.CharData:
			// Text of the property and of anything nested in it, such
			// as the entries of a list-valued property.
			if name != "" && value.Len() < maxPropertyBytes {
				value.Write(t[:min(len(t), maxPropertyBytes-value.Len())])
			}
		case xml.EndElement:
			if x.Depth() == 1 && name != "" {
				if !seen[name] {
					seen[name] = true
					set(name, strings.TrimSpace(strings.ToValidUTF8(value.String(), "�")))
				}
				name = ""
			}
		}
	}
}

// count parses a non-negative whole number, giving 0 for anything else.
func count(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// The forms of date and time that the W3C date and time format allows, and
// that programs are seen to write.
var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04",
	"2006-01-02",
	"2006-01",
	"2006",
}

// parseTime parses a property date, giving the zero time for anything it
// does not recognise. A time without a zone is taken as UTC.
func parseTime(s string) time.Time {
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
