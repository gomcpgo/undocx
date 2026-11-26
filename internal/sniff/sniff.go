// Package sniff works out what kind of file something is from its first
// bytes, so that a file that is not a Word document can be named for what it
// is: a legacy .doc, a password-protected document, a spreadsheet.
package sniff

import (
	"bytes"
	"io"
)

// Formats that Detect reports.
const (
	Unknown     = ""            // not recognised; it may still be a zip with data in front
	Empty       = "empty"       // no bytes at all
	Zip         = "zip"         // a zip archive, which a .docx is
	Doc         = "doc"         // Word 97-2003 binary document
	Xls         = "xls"         // Excel 97-2003 workbook
	Ppt         = "ppt"         // PowerPoint 97-2003 presentation
	OLE         = "ole"         // some other compound file
	Encrypted   = "encrypted"   // a password-protected Office document
	RTF         = "rtf"         // Rich Text Format
	Word2003XML = "word2003xml" // the single-file XML format of Word 2003
	FlatOPC     = "flatopc"     // a package written out as one XML file
	XML         = "xml"         // some other XML
	HTML        = "html"        // HTML, which Word also saves under a .doc name
	MHT         = "mht"         // a web archive, likewise
	PDF         = "pdf"
)

const headSize = 4096

// Detect reports the format of the file. It reads the first few kilobytes,
// and for a compound file the directory, wherever that is.
func Detect(r io.ReaderAt, size int64) string {
	if size <= 0 {
		return Empty
	}
	head := make([]byte, min(size, headSize))
	n, _ := r.ReadAt(head, 0)
	head = head[:n]
	switch {
	case len(head) == 0:
		return Empty
	case bytes.HasPrefix(head, []byte("PK\x03\x04")), bytes.HasPrefix(head, []byte("PK\x05\x06")):
		return Zip
	case bytes.HasPrefix(head, oleMagic):
		return compound(r, size)
	case bytes.HasPrefix(head, []byte(`{\rtf`)):
		return RTF
	case bytes.HasPrefix(head, []byte("%PDF-")):
		return PDF
	}
	text := bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	text = bytes.TrimLeft(text, " \t\r\n")
	lower := bytes.ToLower(text)
	switch {
	case bytes.HasPrefix(lower, []byte("mime-version:")):
		return MHT
	case bytes.HasPrefix(lower, []byte("<!doctype html")), bytes.HasPrefix(lower, []byte("<html")):
		return HTML
	case bytes.HasPrefix(text, []byte("<?xml")), bytes.HasPrefix(text, []byte("<")):
		switch {
		case bytes.Contains(text, []byte("schemas.microsoft.com/office/2006/xmlPackage")):
			return FlatOPC
		case bytes.Contains(text, []byte("schemas.microsoft.com/office/word/2003/wordml")), bytes.Contains(text, []byte("<w:wordDocument")):
			return Word2003XML
		case bytes.Contains(lower, []byte("<html")):
			return HTML
		}
		return XML
	}
	return Unknown
}
