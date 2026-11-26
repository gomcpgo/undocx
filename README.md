# undocx

A pure-Go package that reads Word documents (`.docx`) and returns their content as Markdown, plain text or a structured model, for language models and search indexes. It is the companion of [unpdf](https://github.com/gomcpgo/unpdf) and the reverse of [htmltodocx](https://github.com/gomcpgo/htmltodocx).

**Status: early. The package opens documents and reads their metadata; it does not extract text yet.**

What works today:

- Telling what a file is, whatever its name: a `.docx`, `.docm`, `.dotx` or `.dotm`, or something else, named for what it is (a Word 97-2003 `.doc`, a password-protected document, a spreadsheet, a PDF).
- Opening the package safely: limits on the number and size of parts, on compression ratio, and on XML depth and token size.
- Document properties: title, author, dates, revision, and the page and word counts the file records.
- Transitional and Strict documents alike.

```go
doc, err := undocx.OpenFile(ctx, "contract.docx", undocx.Options{})
switch {
case errors.Is(err, undocx.ErrLegacyFormat):
	// a .doc or .rtf: ask for it to be saved as .docx
case errors.Is(err, undocx.ErrEncrypted):
	// needs its password removed
case errors.Is(err, undocx.ErrNotDOCX):
	// some other kind of file; a *FormatError says which
case err != nil:
	return err
}
defer doc.Close()

info := doc.Info()
fmt.Println(info.Title, info.Author, info.Modified)
```

```
$ go run ./cmd/undocx info contract.docx
Format:           docx
Title:            Supply Agreement
Author:           Ann Lee
Modified:         2026-02-03T17:30:00Z
```

## Goals

- Pure Go, standard library only, no cgo and no external programs.
- The text Word shows, not just the text in the file: list labels as printed (`4.2`, `(b)`, `Article IV`), tracked changes accepted by default, hidden text left out, text boxes and moved text once.
- Headings, lists, tables, links, footnotes, comments, content controls and form fields as Markdown.
- Safe with untrusted files: limits on size, depth and compression ratio, typed errors, no panics, and nothing outside the package is ever fetched.

## Test documents

`internal/docxtest` builds packages from WordprocessingML written by hand from ECMA-376: 36 documents, one per feature, each with the text Word displays. No file saved by Word or any other program is part of this repository.

```
go test ./...
go test -fuzz FuzzOpen -fuzztime 1m .
go run ./internal/docxtest/gen -o /tmp/fixtures -expected /tmp/expected
```

The last command writes the fixture documents and the text expected from each, for use with other tools. Design decisions are recorded in [docs/design.md](docs/design.md).

## Clean room

This is a clean-room implementation, written from the ECMA-376 specification and Microsoft's published implementation notes. It contains no code from pandoc or any other copyleft converter.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
