package undocx

import (
	"errors"

	"github.com/gomcpgo/undocx/internal/diag"
	"github.com/gomcpgo/undocx/internal/opc"
	"github.com/gomcpgo/undocx/internal/sniff"
)

// Errors that Open and the Document methods return. Test for them with
// errors.Is.
var (
	// ErrNotDOCX means the input is not a Word document in the .docx
	// family. The error is a *FormatError, which says what the input is
	// when that could be worked out.
	ErrNotDOCX = errors.New("undocx: not a Word document (.docx)")
	// ErrLegacyFormat means the input is a Word document in a format older
	// than .docx: a Word 97-2003 .doc, RTF, or one of the single-file XML
	// formats. It can be read after saving it as .docx. An error that
	// matches ErrLegacyFormat also matches ErrNotDOCX.
	ErrLegacyFormat = errors.New("undocx: a Word document in a legacy format")
	// ErrEncrypted means the document is protected with a password to
	// open, which this package does not decrypt.
	ErrEncrypted = errors.New("undocx: the document is password-protected")
	// ErrDamaged means the file is a Word document too damaged to read.
	ErrDamaged = diag.ErrDamaged
)

// LimitError means the document exceeded one of the Limits. What names the
// thing that was limited and Limit is the bound it ran into. Test for it
// with errors.As.
type LimitError = diag.LimitError

// Warning describes a problem that did not prevent reading the document.
// Code identifies the kind of problem and is stable across releases; Message
// describes this instance of it.
type Warning = diag.Warning

// Warning codes.
const (
	// WarnPartName: an entry of the archive has a name no part may have,
	// and was ignored.
	WarnPartName = opc.WarnPartName
	// WarnDuplicatePart: a part is stored more than once. The first copy
	// is used.
	WarnDuplicatePart = opc.WarnDuplicatePart
	// WarnContentTypes: the list of content types is missing or malformed.
	WarnContentTypes = opc.WarnContentTypes
	// WarnRelationships: a relationships part is missing or malformed.
	WarnRelationships = opc.WarnRelationships
	// WarnMainPart: the package does not say which part is the document,
	// and it was found by its usual name or its content type instead.
	WarnMainPart = "main-part"
	// WarnMetadata: the document properties could not be read in full.
	WarnMetadata = "metadata"
	// WarnLimit: something was cut short by a limit.
	WarnLimit = diag.WarnLimit
)

// Formats that a FormatError names.
const (
	FormatDOC         = sniff.Doc         // Word 97-2003 binary document
	FormatRTF         = sniff.RTF         // Rich Text Format
	FormatWord2003XML = sniff.Word2003XML // the single-file XML format of Word 2003
	FormatFlatOPC     = sniff.FlatOPC     // a .docx written out as one XML file
	FormatXLS         = sniff.Xls         // Excel 97-2003 workbook
	FormatPPT         = sniff.Ppt         // PowerPoint 97-2003 presentation
	FormatOLE         = sniff.OLE         // another Office 97-2003 style compound file
	FormatXLSX        = "xlsx"            // Excel workbook
	FormatPPTX        = "pptx"            // PowerPoint presentation
	FormatOpenDoc     = "opendocument"    // OpenDocument text, spreadsheet or presentation
	FormatEPUB        = "epub"
	FormatZip         = sniff.Zip // a zip archive that is not a Word document
	FormatPDF         = sniff.PDF
	FormatHTML        = sniff.HTML
	FormatMHT         = sniff.MHT // a web archive, which Word can save under a .doc name
	FormatXML         = sniff.XML
	FormatEmpty       = sniff.Empty // a file of no bytes
)

// FormatError reports that the input is not a .docx, and what it is instead.
// It matches ErrNotDOCX, and for the older Word formats ErrLegacyFormat too.
type FormatError struct {
	// Format is one of the Format constants, or empty when the input was
	// not recognised.
	Format string
}

var formatNames = map[string]string{
	FormatDOC:         "a Word 97-2003 document (.doc)",
	FormatRTF:         "a Rich Text Format document (.rtf)",
	FormatWord2003XML: "a Word 2003 XML document",
	FormatFlatOPC:     "a Word document saved as a single XML file",
	FormatXLS:         "an Excel 97-2003 workbook (.xls)",
	FormatPPT:         "a PowerPoint 97-2003 presentation (.ppt)",
	FormatOLE:         "an Office 97-2003 file that is not a Word document",
	FormatXLSX:        "an Excel workbook (.xlsx)",
	FormatPPTX:        "a PowerPoint presentation (.pptx)",
	FormatOpenDoc:     "an OpenDocument file",
	FormatEPUB:        "an EPUB book",
	FormatZip:         "a zip archive that holds no Word document",
	FormatPDF:         "a PDF",
	FormatHTML:        "an HTML page",
	FormatMHT:         "a web archive",
	FormatXML:         "an XML file",
	FormatEmpty:       "empty",
}

func (e *FormatError) Error() string {
	name, ok := formatNames[e.Format]
	switch {
	case !ok:
		return ErrNotDOCX.Error()
	case e.legacy():
		return ErrLegacyFormat.Error() + ": the file is " + name
	}
	return ErrNotDOCX.Error() + ": the file is " + name
}

// Is makes the error match ErrNotDOCX, and ErrLegacyFormat for the older
// Word formats.
func (e *FormatError) Is(target error) bool {
	return target == ErrNotDOCX || target == ErrLegacyFormat && e.legacy()
}

func (e *FormatError) legacy() bool {
	switch e.Format {
	case FormatDOC, FormatRTF, FormatWord2003XML, FormatFlatOPC:
		return true
	}
	return false
}
