// Package undocx reads Word documents (.docx): their metadata today, and
// their text, structure and images as the package grows. It is written in
// pure Go with the standard library alone: no cgo and no external programs.
//
// Word files are treated as untrusted input. A .docx is a zip archive of XML
// parts, and both layers are read through the Limits in Options, which cap
// the number and size of the parts, how far they may expand, and how deeply
// their XML may nest. Nothing outside the file is ever opened: linked images,
// templates and other external targets are reported, never fetched. Malformed
// input produces an error or a Warning, never a panic.
//
// # Opening a document
//
// Open takes an io.ReaderAt, so a file, a byte slice or anything that can be
// read at an offset will do. Only the package structure and the document
// properties are read up front.
//
//	doc, err := undocx.OpenFile(ctx, "contract.docx", undocx.Options{})
//	if err != nil {
//		return err
//	}
//	defer doc.Close()
//
//	info := doc.Info()
//	fmt.Println(info.Title, info.Author, info.Modified)
//
// Open accepts .docx, .docm, .dotx and .dotm files, in the Transitional and
// the Strict conformance class. Info.Format says which kind it found; the
// file name plays no part.
//
// # Errors
//
// Errors that a caller can act on have a fixed identity:
//
//   - ErrNotDOCX: the file is something else. The error is a *FormatError
//     that names what it is when that is known: a spreadsheet, a PDF.
//   - ErrLegacyFormat: the file is a Word document in an older format, such
//     as a Word 97-2003 .doc, whatever its name. It is also a *FormatError
//     and also matches ErrNotDOCX.
//   - ErrEncrypted: the document is protected with a password to open.
//   - ErrDamaged: the file is a Word document that cannot be read.
//   - *LimitError: the document exceeded one of the Limits.
//
// The first four match with errors.Is and the last with errors.As. Problems
// that do not prevent reading are collected as warnings instead; see
// Document.Warnings.
//
// A Document is not safe for concurrent use.
package undocx
