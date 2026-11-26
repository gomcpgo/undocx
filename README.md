# undocx

A pure-Go package that reads Word documents (`.docx`) and returns their content as Markdown, plain text or a structured model, for language models and search indexes. It is the companion of [unpdf](https://github.com/gomcpgo/unpdf) and the reverse of [htmltodocx](https://github.com/gomcpgo/htmltodocx).

**Status: early. The package does not read documents yet.** What is here so far is the test foundation: a builder that makes Word documents in memory, and 36 documents built with it, one per feature, each with the text Word displays.

## Goals

- Pure Go, standard library only, no cgo and no external programs.
- The text Word shows, not just the text in the file: list labels as printed (`4.2`, `(b)`, `Article IV`), tracked changes accepted by default, hidden text left out, text boxes and moved text once.
- Headings, lists, tables, links, footnotes, comments, content controls and form fields as Markdown.
- Safe with untrusted files: limits on size, depth and compression ratio, typed errors, no panics, and nothing outside the package is ever fetched.

## Test documents

`internal/docxtest` builds packages from WordprocessingML written by hand from ECMA-376. No file saved by Word or any other program is part of this repository.

```
go test ./...
go run ./internal/docxtest/gen -o /tmp/fixtures -expected /tmp/expected
```

The second command writes the fixture documents and the text expected from each, for use with other tools.

## Clean room

This is a clean-room implementation, written from the ECMA-376 specification and Microsoft's published implementation notes. It contains no code from pandoc or any other copyleft converter.

## Licence

Apache-2.0. See [LICENSE](LICENSE).
