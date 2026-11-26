# Design notes

This file records how undocx is built and why. It doubles as the record that the work is independent: every decision here was made from the public specifications and from watching how documents and other programs behave, not from another converter's source code.

## Clean-room rules

1. **Sources are the specifications:** ECMA-376 (Office Open XML: Part 1 for WordprocessingML, Part 2 for packaging, Part 3 for markup compatibility) and Microsoft's published notes on how Word implements them ([MS-OI29500], [MS-DOCX], [MS-CFB], [MS-OFFCRYPTO]).
2. **Other converters are black boxes.** pandoc, LibreOffice and others are run as programs to compare output. Their source is never read. This applies above all to anything under a copyleft licence.
3. **Nothing other tools write is committed.** No document saved by Word, LibreOffice or pandoc, and no excerpt of one, is in this repository.
4. **Test documents are our own.** `internal/docxtest` builds every test document in memory from markup written by hand from the standard.
5. **No dependencies.** The standard library only.

## Layers

```
bytes (io.ReaderAt)
  │ internal/sniff: what kind of file is this?
  ▼
internal/opc: the zip archive as a package: parts, content types, relationships
  │ internal/xmlstream: tokens from a part, with limits, one namespace dialect,
  │                     and mc:AlternateContent resolved
  ▼
undocx: Open, Info, Warnings
```

- `opc` and `xmlstream` know nothing about Word, so readers for `.xlsx` and `.pptx` can use them unchanged.
- `internal/diag` holds what every layer reports with: `ErrDamaged`, `LimitError` and the warning log. The public package re-exports them.

## Decisions

### What the file is

- **The content decides, never the name.** `sniff` looks at the first bytes. A zip archive goes on to `opc`; anything else is turned away with a `FormatError` that names it, so a caller can tell a user "this is a spreadsheet" or "save this .doc as .docx".
- **A compound file needs its directory read.** A Word 97-2003 `.doc` and a password-protected `.docx` both start with the same eight bytes. They differ in the streams they hold: `WordDocument` or `EncryptedPackage`. `sniff/cfb.go` follows the allocation table to the directory and walks the root's children, with every loop bounded by a count, and gives up quietly on damage. Stream names compare without regard to case; one corpus file writes them in upper case.
- **A `.doc` with a password is reported as a `.doc`.** Its encryption is inside the Word stream, and the answer for the user is the same: it must be saved as `.docx`.
- **An encrypted package may not be a Word file.** The container does not say which Office program wrote it, so an encrypted spreadsheet also gives `ErrEncrypted`.
- **Unrecognised bytes still get a try as a zip.** A zip archive may have other data in front, and Go's reader copes with that.
- **`ErrLegacyFormat` also matches `ErrNotDOCX`.** A caller that only asks "is this a .docx?" gets the right answer; one that wants to say more checks for the legacy formats first.

### The package

- **The main part is found, not assumed.** The package relationships name it. Its content type then says whether this is a document, a template or a macro-enabled one, and its root element must be `w:document`. Failing a relationship, a part with the right content type is used, then `word/document.xml`, each with a warning.
- **A spreadsheet or presentation is recognised by its main part's content type**, and OpenDocument and EPUB files by their `mimetype` entry.
- **Part names compare in lower case, with percent escapes decoded and backslashes turned into slashes.** The standard makes part names case-insensitive, and real files differ in how they write the same name in the archive and in a relationship.
- **A part stored twice: the first copy wins, with a warning.** Two readers that pick different copies would show different documents, so the choice is fixed and reported.
- **A relationship target only ever names a part.** It is resolved as a path inside the package, steps above the root stay at the root, and nothing is read from disk or the network. External targets are kept as text for reporting.
- **Missing or malformed content types, relationships and properties are warnings.** The document is still read when it can be found.

### Limits

Defaults were set against a corpus of 965 real documents: the largest part is 18 MiB, the highest compression ratio of any part over 256 KiB is 74, the most entries is 910, and the deepest nesting is 33 elements, apart from one stress file.

- **Parts are checked when read, not when the package is opened.** A document with a huge embedded video should still give up its text, so an oversized part fails only if something asks for it.
- **The total counts each part once.** `MaxTotalBytes` adds up the parts actually inflated, and reading a part a second time is free. Converting a document twice must not run into a limit that converting it once did not.
- **The compression ratio is checked only above 8 MiB.** A small part of repetitive table markup compresses very well and costs nothing. The check uses the sizes the archive declares; Go's zip reader refuses to inflate a part past its declared size, so the declaration is a true upper bound.
- **Token size is guarded before the XML decoder sees the bytes.** `encoding/xml` reads a whole text node or tag into memory before returning it. A small filter in front of it counts the bytes between angle brackets and stops at the limit. The default is generous (32 MiB) because VML shapes legitimately carry large base64 attributes.
- **Depth is counted everywhere, including in content that is being skipped.** One corpus file nests tables 15,005 elements deep, and two other converters run for over a minute on it. The reader stops at the limit in under a millisecond. Reading it to the end with the limit raised takes 67 ms, so the limit is there for the code above the reader, not for the reader.

### XML

- **`encoding/xml` behind our own reader.** The wrapper holds the limits, the cancellation check and the clean-up below, so the tokenizer can be replaced if profiling ever asks for it.
- **No DTD processing.** The decoder does not expand entities that a document declares, so entity-expansion attacks do not apply. A part that uses its own entity is malformed, which matches the packaging standard: it forbids DTDs.
- **Strict becomes Transitional at the reader.** Strict documents use different namespace URIs for the same elements. The reader maps them on every element and attribute, so everything above it sees one dialect. The rule is mechanical (`http://purl.oclc.org/ooxml/AREA/REST` becomes `http://schemas.openxmlformats.org/AREA/2006/REST`), with two properties names spelled differently. It was checked against the corpus: each of the 41 Strict namespaces and relationship types found there maps to a Transitional one that also occurs there.
- **`mc:AlternateContent` is resolved at the reader.** The first `mc:Choice` whose required namespaces are all understood is passed on, otherwise the `mc:Fallback`, never both. The wrapper elements themselves are not passed on. The caller says which namespaces it understands, so the choice follows what the caller can actually use.
- **UTF-16 parts are converted.** The standard allows UTF-8 and UTF-16. UTF-16 is recognised by its byte order mark, or by the zero bytes around the first angle bracket.

### Metadata

- **`Info` reports what the file says.** Counts of pages and words are whatever the last program saved. Nothing is recounted.
- **Lenient values.** A revision that is not a number, or a date in no recognised form, is left at zero and is not an error. A time without a zone is taken as UTC.
- **Flags that need a pass over the document are not in `Info`.** Whether a document has tracked changes cannot be read from its properties. That will come with the body reader.

## Testing

- **Fixtures** (`internal/docxtest/fixtures.go`): one document per feature, with the text Word displays and checks on what a reader must and must not output. Every fixture ends with a paragraph after its last table, as Word writes it: LibreOffice hangs on a body that ends with a table.
- **Hostile archives** are built with `docxtest.Zip`, which takes entries as given: repeated names, leading slashes, backslashes, an encryption flag. `docxtest.CompoundFile` writes the container of the older formats, and was checked with two independent readers.
- **Fuzz targets:** `FuzzDetect` (sniffing and the compound-file directory), `FuzzReader` (the XML reader), `FuzzOpen` in `opc` and `FuzzOpen` on the public API.
- **Not in this repository:** a corpus of documents written by Word and other programs, a harness that compares against other converters, and a mutation sweep that damages every corpus file. They are run before each release, and their documents are never committed.
