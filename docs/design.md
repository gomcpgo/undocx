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
internal/wml: the styles and the list definitions, read whole
internal/body: the main part, read as a stream into blocks, with the list
               counters kept as it goes (internal/numfmt writes the numbers)
               and each table kept until it ends
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

- **One pass, a paragraph or a table at a time.** The main part is read as a stream and each block is handed on when it is complete. What carries over between paragraphs is small: the open fields, the headings the text stands under, a paragraph waiting to be joined to the next, and the rows of a table that has not ended. Converting 50,000 paragraphs takes ten times as long as 5,000, and the largest corpus document (18 MiB of XML) peaks at 14 MB of memory.
- **The block loop does not call itself.** Content controls and custom XML wrappers are passed through, and the open tables are kept on a stack of the loop's own, so a table in a cell in a table costs no call. Only a text box nests a call, and the XML depth limit bounds that. The blocks themselves do nest (a cell holds blocks, and a table is a block), so the writers call themselves once for each level of tables; they write into one buffer, so the depth costs no copying.
- **A heading opens a section when its block is handed on, not when its paragraph is read.** Whether a heading in a table cell is a heading of the document depends on what the table turns out to be, and that shows only at the table's end.
- **Text comes only from `w:t` in a run. Every wrapper is passed through.** `w:ins`, `w:moveTo`, `w:smartTag`, `w:customXml`, `w:dir`, `w:bdo`, `w:sdt`, `w:fldSimple` and anything a later version of Word adds are descended into without being named. A reader that lists the wrappers it knows loses text in the ones it forgot; this one cannot.

### Lists and numbering

A list label is worked out the way Word works it out, so that "see clause 4.2" in the text still points somewhere. The standard gives the data but not the counting; the rules below come from the standard, Microsoft's notes on Word ([MS-OI29500] 2.1.1690 and the 17.9 notes, [MS-DOCX] 2.4), and documents built for the purpose and run through LibreOffice. Where those disagree, the choice and the reason are given.

- **Counters belong to the list definition** (`w:abstractNum`), not to the numbering instance (`w:num`) a paragraph names. Two instances of one definition count on from each other. LibreOffice numbers them so, and it is why Word's "continue numbering" works across instances.
- **A `w:startOverride` applies once,** the first time a paragraph uses its instance. After that the instance counts on, and other instances of the same definition count on from the restarted value. LibreOffice agrees.
- **A level above the item that has no number yet takes its start value, and counts as used.** A list that opens at its second level reads `1.1`, and the next first-level item is `2`. LibreOffice agrees.
- **Every paragraph is counted, including an empty one.** Word shows the label of an empty numbered paragraph. The empty paragraph itself is still left out of the output.
- **`w:lvlRestart` follows Microsoft's note:** a level restarts when the level it names or any higher one is used, and never with the value 0. Left out, a level restarts when any higher level is used. LibreOffice ignores the element; the fixture `list-restart-levels` records the difference.
- **An override that replaces a level (`w:lvlOverride/w:lvl`) changes its format, not its count.** Word ignores the `w:start` and `w:lvlRestart` inside it (Microsoft's notes on 17.9.10 and 17.9.25), and LibreOffice agrees.
- **A `w:start` left out is 0,** as the standard says. LibreOffice agrees.
- **The level of a paragraph numbered through its style** comes from the `w:pStyle` that ties a level of the list to that style, as the standard says (17.9.23), then from the style's own `w:ilvl`. A level stated on the paragraph wins over both. LibreOffice ignores the tie; Word writes the same level in both places, so documents rarely tell them apart.
- **A pattern that refers to a deeper level shows nothing at all** (Microsoft's note on 17.9.11). A level that refers to a bullet level writes nothing for it: `%1.%2.%3` over a bullet at the second level reads `1..1`. A document written to test this shows the second as Word's result.
- **`w:isLgl` writes every number of the label in decimal, its own level's included** (Microsoft's note on 17.9.4). A level whose format is `none` still writes nothing.
- **A paragraph at a level the list does not define has no label.** LibreOffice makes up a decimal label; what Word does is not known.
- **`w15:restartNumberingAfterBreak`** ([MS-DOCX] 2.2.10) restarts a list after each section break.
- **A paragraph whose mark is a tracked deletion takes no number,** since with the change accepted it is part of the next paragraph.

Number formats:

- **Every format the corpus uses is written,** and the common ones besides: decimal with and without a zero, Roman, letters (`27` is `AA`, as Word repeats the letter), ordinals and English words, Chicago symbols, hexadecimal, enclosed and full-width digits, Chinese and Japanese counting, the CJK financial digits, kana and Hangul sequences, Hebrew and Arabic letters, and the zero-padded custom formats of Word 2010 (`001, 002, 003, ...`).
- **Word's limits are kept:** words stop after 999,999, Chicago symbols after 116, hexadecimal after 65,535, everything after 2,147,483,647. Past them the number is empty.
- **Formats that repeat a symbol stop at 32 repeats** and fall back to decimal, so that a list starting at a huge number cannot make a huge label. This limit is ours.
- **`chineseCounting` counts up to 99 and writes larger numbers digit by digit** (`一○○`), and `chineseCountingThousand` counts throughout (`一百`). The names Word gives them in [MS-DOCX] 2.4 show this.
- **A format not supported is written in decimal, with a warning that counts the labels.** These are the Thai, Hindi, Vietnamese and Korean counting formats, Russian letters, and the custom Turkish, Bulgarian and Greek sequences of Word 2010, whose rules are not clear enough from the published notes to write them.
- **A bullet glyph in a symbol font is mapped like any symbol;** one with no equivalent, and a picture bullet, become `•`.

How a label is written:

- **The label is a field of the block, not part of its inlines.** `Text` and `Markdown` write it in front of the content, and a heading path includes it (`1.2 Payment`), so a citation can name the clause.
- **A space always follows the label,** whatever the level's `w:suff` says. A label glued to its text (`1.Definitions`) breaks searching for either.
- **Markdown uses its own list syntax only for a bullet, or for a decimal number of the item's own level followed by `.` or `)`.** Any other label is written as text at the start of the paragraph, escaped where Markdown would read it as markup (`01\.`). The choice is made per item, not per list: Word's default numbered list has `a.` and `i.` at its lower levels, and deciding per list would turn every such list into paragraphs.
- **An item with a text label stays inside the Markdown item above it.** It is indented to that item's content, so `4.1` written as text sits inside item `4.`, and is not a code block or the end of the list. It nests nothing itself.
- **Items of one list are on consecutive lines.** A blank line separates them only where Markdown needs one: before an item that is not a bullet and does not start at 1, since only those may begin straight after a line of text.
- **Markdown renumbers an ordered list from its first item,** so where a list restarts straight after another (`3.` then `1.`), an empty HTML comment keeps the two apart. Without it the second list would read `4.`.
- **Plain text indents a list item by two spaces for each level below the first,** which is the only sign of nesting a bullet has.

### Tables

A table is one block, with its rows and cells. The rules below come from the standard, from documents built for the purpose and read with LibreOffice and pandoc, and from the tables of the corpus. Where LibreOffice and pandoc disagree, the choice and the reason are given. What Word does in those places is not known.

What a table is:

- **A table is kept until it ends.** Three things show only then: whether it is a table at all, which cells are merged, and whether Markdown can write it.
- **A table with text in only one cell of each row is not a table.** It is a frame around a notice, or a page laid out with empty cells for space, as documents made from HTML are. Its paragraphs are handed on as paragraphs of whatever the table stands in, and a heading among them opens a section. The cost: a form with labels in one column and blanks in the other reads as its labels alone.
- **A table with no text is left out.** An empty row of a table that has text is kept: it is a row someone is meant to fill in.
- **A heading in a cell of a real table keeps its kind and opens no section.** It is written in bold. The blocks in a cell have no heading path of their own.
- **An empty numbered paragraph in a cell is kept,** as its label alone. It is often all the cell holds: the number of the row. Outside a table such a paragraph still only counts.
- **A long table is handed on in parts of 1,000 rows,** so that a table of any length is read in bounded memory: 200,000 rows take 14 MB. Each part is a block of its own, marked as a continuation, and starts with the table's header rows again, as each page of the table does in Word. A cell is merged only with cells of its own part. No table in the corpus is this long (the longest has 948 rows).

Where a cell stands:

- **A cell starts in the column after the cell before it,** or after the columns its row leaves empty (`w:gridBefore`), and covers `w:gridSpan` columns. The number of columns comes from the rows; `w:tblGrid` is read only for its widths.
- **The columns written are the ones a reader sees, not the grid lines of the file.** Every place where a cell ever ended stays a grid line, so the grid of a plain five-column table may have eleven lines, with every cell spanning two. Only the lines where a cell starts or ends count as column edges. Edges closer together than an eighth of an inch are one edge, unless a cell lies between them: Word leaves columns a hair wide behind when cells are dragged, and rows of one corpus document start 11 twentieths of a point apart. Read as they stand, such tables are all merged cells, and pandoc writes them with columns of width 0%. A width the file does not state is taken as wide.
- **An indent that every row has is not a column.**
- **A span is capped at 63 columns,** the most Word allows in a table, so that a cell cannot claim a width the output would have to fill.
- **Rows that the file shifts sideways by a real distance stay shifted.** One corpus document has many tables whose rows each start a third of an inch further right. They are written as the file has them.

Merged cells:

- **A cell marked as continuing a merge down rows is part of the cell above it when that one starts in the same column, covers as many columns and is merged itself.** The comparison is made on the columns a reader sees, so rows a hair apart still merge.
- **Otherwise it is a cell of its own.** LibreOffice reads a continuation that nothing began as a cell with its text. pandoc merges it into the cell above, whatever that is, and drops its text. The first loses nothing.
- **Text in a merged-away cell is left out, with a warning that counts the cells.** LibreOffice and pandoc both leave it out, and no corpus document has any. Whether Word shows it has not been checked.
- **The older merge across columns (`w:hMerge`) joins the cells and keeps the text of each.** LibreOffice does the same; pandoc ignores the element.
- **A deleted row and a deleted cell (`w:cellDel`) are gone; a merge that is a tracked change (`w:cellMerge`) is made.** A hidden row (`w:hidden`) whose text is hidden too is left out.

Header rows:

- **A header row is a row marked `w:tblHeader` at the top of the table,** at most eight of them. That mark is what Word's "repeat as header row" sets, and it is a statement about the row. The "header row" option of a table style (`w:tblLook`) is not used: it says how the style paints the first row, not what the row is, and it is set on half the tables of the corpus (640 of 1,334). pandoc does treat most first rows as headers; in the corpus the two agree only where `w:tblHeader` is set.

Around a table:

- **A paragraph cannot run on into a table, or from one cell into the next.** A paragraph whose mark is deleted, or a drop cap, is handed on at the end of its cell.
- **A content control around a cell that nobody has filled in leaves the cell in place, empty.** Left out with the cell, it would move the cells after it one column to the left. Around rows or a whole table it is left out with them.
- **Rows and cells with no table around them are passed through,** and their paragraphs are paragraphs.
- **Damage inside a table keeps the rows read before it.**

As Markdown:

- **A pipe table where Markdown can write one,** and an HTML table where it cannot: a merged cell, a table in a cell, or more than one header row.
- **In a pipe table the first row is the header,** because the syntax needs one. A later part of a long table that has no header rows gets an empty header, since its first row is a row of data.
- **A cell is one line.** Its paragraphs are joined with `<br>`, a list item starts with its label, and every `|` is escaped, which the syntax asks for even in a code span. In a link address it is written `%7C`.
- **Rows are filled out with empty cells to the table's width,** up to 64 columns. In a wider table, which Word cannot make, only the header is, so that one very long row cannot make every other row long.
- **In an HTML table nothing is Markdown.** A Markdown reader takes the lines of an HTML block as they stand, so bold, links and line breaks are tags, and `<`, `>` and `&` are character references. There is no empty line in it, which would end the block.
- **A column a row leaves empty gets an empty cell in HTML; a column covered by a cell merged from above does not.**

As plain text:

- **A line for each row and a tab between cells,** so that a table can be read as tab-separated values. A cell is in its column whatever is merged above or beside it: a cell that covers three columns is followed by three tabs. Tabs at the end of a line are dropped, and a row with no text has no line.
- **The paragraphs of a cell are joined with a space,** since a line end would end the row.
- **A table in a cell follows the row it stands in.**

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
- **Fuzz targets:** `FuzzDetect` (sniffing and the compound-file directory), `FuzzReader` (the XML reader), `FuzzOpen` in `opc` and `FuzzOpen` on the public API, `FuzzBody` (a main part, a styles part and a numbering part of arbitrary bytes inside a sound package), and `FuzzFormat` (the number formats).
- **`TestTables` and `TestTableGrid` state the table rules,** one small document for each, with both outputs written out. `TestTableTime` guards the costs: a long table, cells that claim to be wide, one very wide row over many short ones, and deep nesting.
- **Not in this repository:** a corpus of documents written by Word and other programs, a harness that compares against other converters, a check of list labels against LibreOffice's, a check of table grids against pandoc's HTML output, and a mutation sweep that damages every corpus file. They are run before each release, and their documents are never committed.
