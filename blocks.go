package undocx

import (
	"context"
	"errors"
	"io"
	"iter"
	"strings"

	"github.com/gomcpgo/undocx/internal/body"
	"github.com/gomcpgo/undocx/internal/model"
	"github.com/gomcpgo/undocx/internal/wml"
)

// Block is one paragraph-level piece of a document: a paragraph, a heading,
// a list item, the title or a table. Its Text and Markdown methods render it
// alone, which is what a caller needs to cut a document into chunks on block
// boundaries.
type Block = model.Block

// TableData is the content of a Table block: its rows, with each cell placed
// on the grid of columns. To cut a long table into chunks, make a Block of
// kind Table from some of its rows and render that.
type TableData = model.TableData

// Row is one row of a table.
type Row = model.Row

// Cell is one cell of a table. Its content is blocks again: paragraphs, list
// items and tables nested in it.
type Cell = model.Cell

// BlockKind says what part a block plays in the document.
type BlockKind = model.BlockKind

// Inline is a piece of a block's content: a run of text with one set of
// formatting, or a line break.
type Inline = model.Inline

// InlineKind says what an Inline is.
type InlineKind = model.InlineKind

// LabelKind says how Markdown writes the label of a list item.
type LabelKind = model.LabelKind

const (
	// Paragraph is body text, and anything not recognised as something
	// else.
	Paragraph = model.Paragraph
	// Heading is a section heading. Block.Level is its rank, from 1.
	Heading = model.Heading
	// Title is the title of the document: a paragraph in the Title style.
	Title = model.Title
	// ListItem is a paragraph of a numbered or bulleted list. Block.Level
	// is its level in the list, from 1, and Block.Label its number or
	// bullet.
	ListItem = model.ListItem
	// Table is a table. Block.Table holds its rows and cells.
	Table = model.Table

	// LabelText is a label Markdown has no list syntax for, such as "4.2",
	// "(b)" or "Article VII". It is written as text at the start of the
	// paragraph.
	LabelText = model.LabelText
	// LabelBullet is a bullet. Markdown writes the item with "-".
	LabelBullet = model.LabelBullet
	// LabelNumber is a plain number followed by a full stop or a
	// parenthesis, which Markdown writes as an item of an ordered list.
	LabelNumber = model.LabelNumber

	// Text is a run of text.
	Text = model.Text
	// Break is a line break inside a paragraph.
	Break = model.Break
)

// More warning codes, for problems met while reading the text.
const (
	// WarnStyles: the styles part is missing or malformed, or paragraphs
	// name styles that are not defined. Headings that rely on those
	// styles are read as paragraphs.
	WarnStyles = "styles"
	// WarnDamagedBody: the document is malformed or cut short part of the
	// way through. The text before the damage was read.
	WarnDamagedBody = "damaged-body"
	// WarnSymbol: characters of a symbol font have no Unicode equivalent
	// and appear as U+FFFD.
	WarnSymbol = body.WarnSymbol
	// WarnLink: links that are not web, mail or telephone addresses were
	// left as plain text.
	WarnLink = body.WarnLink
	// WarnEquation: equations were written as plain text.
	WarnEquation = body.WarnEquation
	// WarnPlaceholders: content controls that nobody has filled in were
	// left out. ReadOptions.Placeholders keeps them.
	WarnPlaceholders = body.WarnPlaceholder
	// WarnEmbedded: the document embeds content in another format, which
	// was not read.
	WarnEmbedded = body.WarnAltChunk
	// WarnNumbering: the numbering part is missing or malformed, or
	// paragraphs refer to numbering it does not define. Those paragraphs
	// are read without their labels.
	WarnNumbering = body.WarnNumbering
	// WarnNumberFormat: list labels use a number format that is not
	// supported, and their numbers were written in decimal.
	WarnNumberFormat = body.WarnNumberFormat
	// WarnMergedCell: table cells that are merged into the cell above them
	// hold text of their own. Word shows only the text of the first cell
	// of a merged group, so the rest was left out.
	WarnMergedCell = body.WarnMergedCell
)

// ReadOptions says what of a document to read. The zero value reads the
// document as it looks in Word with every tracked change accepted: inserted
// text is kept, deleted text is gone, moved text appears once in its new
// place, and hidden text is left out.
type ReadOptions struct {
	// TOC keeps a generated table of contents. It is left out by default,
	// because it repeats the headings.
	TOC bool
	// Placeholders keeps the prompt text of content controls that nobody
	// has filled in, such as "Click here to enter text". It is left out by
	// default, so that a form is not read as if the prompts were answers,
	// and a warning says how many controls were left out. A template can
	// consist of little else: read it with Placeholders set to see what it
	// asks for.
	Placeholders bool
}

// MarkdownOptions configures Markdown and WriteMarkdown.
type MarkdownOptions struct {
	ReadOptions
	// MaxBytes stops the output once it has reached about this size, at
	// the end of a block, and adds a comment saying that it was cut. Zero
	// means no limit.
	MaxBytes int
}

const (
	relStyles    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles"
	relNumbering = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering"
	relHyperlink = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink"
)

// prepare loads what reading the body needs: the styles, the list
// definitions and the targets of the links. It does so once.
func (d *Document) prepare(ctx context.Context) error {
	if d.styles != nil {
		return nil
	}
	rels, err := d.pkg.Relationships(ctx, d.main)
	if err != nil {
		if stop(err) {
			return err
		}
		d.log.Add(WarnRelationships, "the relationships of %s could not be read in full: %v", d.main, err)
	}
	links := map[string]string{}
	stylesPart, numberingPart := "", ""
	for _, r := range rels {
		switch {
		case r.Type == relHyperlink && r.External:
			links[r.ID] = r.Target
		case r.Type == relStyles && !r.External && stylesPart == "" && d.pkg.Has(r.Part):
			stylesPart = r.Part
		case r.Type == relNumbering && !r.External && numberingPart == "" && d.pkg.Has(r.Part):
			numberingPart = r.Part
		}
	}
	styles := wml.NoStyles()
	if stylesPart != "" {
		x, c, err := d.pkg.XML(ctx, stylesPart, nil)
		if err == nil {
			styles, err = wml.ReadStyles(x)
			c.Close()
		}
		if err != nil {
			if stop(err) {
				return err
			}
			d.log.Add(WarnStyles, "the styles could not be read in full: %v", err)
		}
	}
	numbering := wml.NoNumbering()
	if numberingPart != "" {
		x, c, err := d.pkg.XML(ctx, numberingPart, nil)
		if err == nil {
			numbering, err = wml.ReadNumbering(x)
			c.Close()
		}
		if err != nil {
			if stop(err) {
				return err
			}
			d.log.Add(WarnNumbering, "the numbering could not be read in full, and some list labels may be missing: %v", err)
		}
	}
	d.styles, d.lists, d.links = styles, numbering, links
	return nil
}

// Blocks returns an iterator over the blocks of the document, in reading
// order. The document is read as the iteration goes, a paragraph or a table
// at a time, so memory does not grow with its length and stopping early costs
// nothing for what was not visited.
//
// A table is one block, with its rows and cells in Block.Table. A table
// whose rows have one cell each is a frame around text, not a table, and its
// paragraphs come as blocks of their own. A table of several thousand rows
// comes as several Table blocks; see TableData.Continued.
//
// The iterator yields a non-nil error, and stops, when the context is
// cancelled, when a limit is exceeded, or when the document is damaged from
// its very start. Damage further in ends the iteration without an error and
// is reported through Warnings, so that what could be read is not lost.
func (d *Document) Blocks(ctx context.Context, opts ReadOptions) iter.Seq2[Block, error] {
	return func(yield func(Block, error) bool) {
		if err := d.prepare(ctx); err != nil {
			yield(Block{}, err)
			return
		}
		x, c, err := d.pkg.XML(ctx, d.main, body.Understood)
		if err != nil {
			yield(Block{}, err)
			return
		}
		defer c.Close()
		stopped := false
		n, err := body.Read(x, body.Config{Styles: d.styles, Numbering: d.lists, Links: d.links, Log: d.log, TOC: opts.TOC, Placeholders: opts.Placeholders}, func(b model.Block) bool {
			if !yield(b, nil) {
				stopped = true
			}
			return !stopped
		})
		if err == nil || stopped {
			return
		}
		if errors.Is(err, ErrDamaged) && n > 0 {
			d.log.Add(WarnDamagedBody, "the document is damaged after %d blocks, and the rest was not read: %v", n, err)
			return
		}
		yield(Block{}, err)
	}
}

// WriteMarkdown writes the document to w as Markdown, a block at a time.
// See Block.Markdown for how a block is written. List items are nested, and
// the items of a list are written on consecutive lines. A table is a pipe
// table, or an HTML table when it has merged cells, a table inside a cell or
// more than one header row.
func (d *Document) WriteMarkdown(ctx context.Context, w io.Writer, opts MarkdownOptions) error {
	written := 0
	var mw model.MarkdownWriter
	for b, err := range d.Blocks(ctx, opts.ReadOptions) {
		if err != nil {
			return err
		}
		md := mw.Block(b)
		if md == "" {
			continue
		}
		if written > 0 {
			md = md[1:] // the newline that ends the block before is written already
		}
		if opts.MaxBytes > 0 && written+len(md)+1 > opts.MaxBytes {
			if written == 0 {
				// Not even the first block fits: cut it.
				md = strings.ToValidUTF8(strings.TrimLeft(md, "\n")[:min(len(md), opts.MaxBytes)], "")
				if _, err := io.WriteString(w, md+"\n"); err != nil {
					return err
				}
			}
			_, err := io.WriteString(w, "\n<!-- The document continues. This output was cut short at the size asked for. -->\n")
			return err
		}
		n, err := io.WriteString(w, md+"\n")
		written += n
		if err != nil {
			return err
		}
	}
	return nil
}

// Markdown returns the document as Markdown: headings, paragraphs, lists,
// tables, inline formatting and links. On an error it returns what was
// converted before it.
func (d *Document) Markdown(ctx context.Context, opts MarkdownOptions) (string, error) {
	var sb strings.Builder
	err := d.WriteMarkdown(ctx, &sb, opts)
	return sb.String(), err
}

// WriteText writes the document to w as plain text, one block per line. A
// line break inside a paragraph is a line end too. A list item starts with
// its label, and is indented by two spaces for each level below the first.
// A table is one line for each row, with a tab between cells.
func (d *Document) WriteText(ctx context.Context, w io.Writer, opts ReadOptions) error {
	for b, err := range d.Blocks(ctx, opts) {
		if err != nil {
			return err
		}
		line := b.Text()
		if b.Kind == ListItem && b.Level > 1 {
			pad := strings.Repeat("  ", b.Level-1)
			line = pad + strings.ReplaceAll(line, "\n", "\n"+pad)
		}
		if _, err := io.WriteString(w, line+"\n"); err != nil {
			return err
		}
	}
	return nil
}

// Text returns the document as plain text. On an error it returns what was
// read before it.
func (d *Document) Text(ctx context.Context, opts ReadOptions) (string, error) {
	var sb strings.Builder
	err := d.WriteText(ctx, &sb, opts)
	return sb.String(), err
}
