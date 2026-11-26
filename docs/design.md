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
internal/wml: the styles, read whole
internal/body: the main part, read as a stream into blocks
  ▼
internal/model: blocks and inline runs, and their Markdown and plain text
  ▼
undocx: Open, Info, Blocks, Markdown, Text, Warnings
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

### Reading the body

- **One pass, a paragraph at a time.** The main part is read as a stream and each block is handed on when it is complete. What carries over between paragraphs is small: the open fields, the headings the text stands under, and a paragraph waiting to be joined to the next. Converting 50,000 paragraphs takes ten times as long as 5,000, and the largest corpus document (18 MiB of XML) peaks at 13 MB of memory.
- **The block loop does not call itself.** Table rows and cells, content controls and custom XML wrappers are passed through, and every paragraph found is a block, however deep. Only a text box nests a call, and the XML depth limit bounds that.
- **Text comes only from `w:t` in a run. Every wrapper is passed through.** `w:ins`, `w:moveTo`, `w:smartTag`, `w:customXml`, `w:dir`, `w:bdo`, `w:sdt`, `w:fldSimple` and anything a later version of Word adds are descended into without being named. A reader that lists the wrappers it knows loses text in the ones it forgot; this one cannot.
- **Tables are flattened for now.** Their paragraphs come out in reading order. Structure comes with the tables milestone.

### Tracked changes

The document is read as it looks with every change accepted.

- **Deleted and moved-away text is read past, not skipped.** `w:del` and `w:moveFrom` contribute no text, but a field may begin or end inside them, and skipping the element would leave the count of open fields wrong and hide everything after it.
- **Moved text appears once,** where it was moved to. `w:moveFrom` holds ordinary `w:t` runs, which is why simple readers print it twice.
- **A deleted paragraph mark joins two paragraphs.** The joined paragraph takes the style of the second, whose mark survives. A deleted table row is left out.
- **The old formatting inside `w:rPrChange` and `w:pPrChange` is never read as the formatting.**

### Fields

- **A stack of open fields, kept across paragraphs.** Between `begin` and `separate` is the instruction; text there is not document text, and that includes the results of fields nested in the instruction. Between `separate` and `end` is the result that the last program to save the file left, which is what Word shows.
- **A table of contents is a field** whose result repeats the headings. It is left out unless asked for.
- **`HYPERLINK` fields and `w:hyperlink` elements become links** when the address is a web, mail or telephone address. Anything else, such as a script or a file path, stays as plain text and is counted in a warning, because the Markdown may be shown by a program that follows links.
- **Nesting is capped at 64.** Fields begun beyond that are counted, so their ends do not close the wrong field.

### Styles and formatting

- **Headings by outline level, then by name.** The level set on the paragraph wins, then the one set on its style or a style it is based on, then a style named `heading 1` to `heading 9`. The style ID is never used: it is translated, and anyone can give a body style the ID `Heading1`.
- **Toggle properties follow the standard.** Direct formatting states bold outright. A paragraph style and a character style that both say bold cancel out, which is why Strong inside a bold heading is not bold in Word. Within a chain of `basedOn`, the nearest style that speaks wins.
- **Right-to-left text takes bold and italic from `w:bCs` and `w:iCs`.**
- **Code is a monospace font or a style named for code.** The list of fonts is by name; theme fonts are not resolved.
- **Text formatted as all capitals stays as typed.** Word shows it in capitals, but the stored text is what copying it out of Word gives, and it keeps the case of names. pandoc capitalises it when the formatting is direct; agreeing with it there lowered agreement elsewhere, and the gain was not worth the rule.
- **Hidden text is left out**, whether hidden directly or through a style.
- **Unfilled content controls are left out, with a warning that counts them.** Their content is a prompt such as "Click here to enter text". `ReadOptions.Placeholders` keeps them, which a template needs: in the corpus, some templates are nothing but prompts.

### Symbols

- **Symbol-font text is a glyph position, not a character.** `a` in Symbol is alpha. Both `w:sym` and ordinary runs set in a symbol font are mapped, from the position itself or from its private-use twin at F000 plus the position.
- **Symbol uses the Unicode Consortium's table** (see `NOTICE`). **Wingdings uses a short table of our own** for the glyphs documents use most: bullets, arrows, check marks. It was written from knowledge of the font and has not been checked against a published table. Everything else in Wingdings, and all of Webdings and Wingdings 2 and 3, comes out as U+FFFD with a warning that counts them.

### Text clean-up

- **A soft hyphen is dropped; a non-breaking hyphen is a hyphen; a non-breaking space is a space.** A search for "well-known" should find the text whichever hyphen the author typed.
- **Tabs and line ends typed into the XML are spaces.** The tabs and line breaks of the document have elements of their own.

### Markdown

- **Marks hug the text.** `**bold **text` is not bold in Markdown, so white space at the edge of a formatted run is written outside its marks. Runs with the same formatting are joined first, so the way Word split a sentence does not show.
- **Escaping is minimal.** A character is escaped only where Markdown would read it as markup: an underscore at the edge of a word but not inside `snake_case`, `#` or `1.` at the start of a line but not elsewhere. Escaping every punctuation mark is always safe for a renderer and worse for a reader and for search.
- **A line break is a backslash at the end of the line,** which is visible in the source, unlike two trailing spaces.
- **Headings do not mark bold,** since a heading is bold already.

### Damage

- **Damage part of the way through ends the reading without an error.** The blocks before it are kept, including the paragraph that was cut, and a warning says where it stopped. Damage before any text is `ErrDamaged`. A limit or a cancelled context is always an error.

## Testing

- **Fixtures** (`internal/docxtest/fixtures.go`): one document per feature, with the text Word displays and checks on what a reader must and must not output. Every fixture ends with a paragraph after its last table, as Word writes it: LibreOffice hangs on a body that ends with a table.
- **Hostile archives** are built with `docxtest.Zip`, which takes entries as given: repeated names, leading slashes, backslashes, an encryption flag. `docxtest.CompoundFile` writes the container of the older formats, and was checked with two independent readers.
- **`TestFixtures` runs every fixture through `Text` and `Markdown`** and applies its checks. Fixtures whose features are not built yet are listed with the reason; one of them passing is a failure too, so the list cannot go stale.
- **Fuzz targets:** `FuzzDetect` (sniffing and the compound-file directory), `FuzzReader` (the XML reader), `FuzzOpen` in `opc` and `FuzzOpen` on the public API, and `FuzzBody` (a main part and a styles part of arbitrary bytes inside a sound package).
- **Not in this repository:** a corpus of documents written by Word and other programs, a harness that compares against other converters, and a mutation sweep that damages every corpus file. They are run before each release, and their documents are never committed.
