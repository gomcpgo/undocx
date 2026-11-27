// Command undocx inspects Word documents.
//
//	undocx info [-json] file.docx
//	undocx markdown [-toc] [-placeholders] [-max BYTES] [-warnings] file.docx
//	undocx text [-toc] [-placeholders] [-warnings] file.docx
//	undocx blocks [-toc] file.docx
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/gomcpgo/undocx"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var err error
	switch os.Args[1] {
	case "info":
		err = info(ctx, os.Args[2:])
	case "markdown":
		err = convert(ctx, os.Args[2:], true)
	case "text":
		err = convert(ctx, os.Args[2:], false)
	case "blocks":
		err = blocks(ctx, os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		switch {
		case errors.Is(err, undocx.ErrEncrypted):
			os.Exit(3)
		case errors.Is(err, undocx.ErrNotDOCX), errors.Is(err, undocx.ErrDamaged):
			os.Exit(2)
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: undocx info [-json] file.docx")
	fmt.Fprintln(os.Stderr, "       undocx markdown [-toc] [-placeholders] [-max BYTES] [-warnings] file.docx")
	fmt.Fprintln(os.Stderr, "       undocx text [-toc] [-placeholders] [-warnings] file.docx")
	fmt.Fprintln(os.Stderr, "       undocx blocks [-toc] file.docx")
	os.Exit(64)
}

type infoOutput struct {
	undocx.Info
	Warnings []undocx.Warning `json:"warnings,omitempty"`
}

func info(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}

	doc, err := undocx.OpenFile(ctx, fs.Arg(0), undocx.Options{})
	if err != nil {
		return err
	}
	defer doc.Close()

	out := infoOutput{Info: doc.Info(), Warnings: doc.Warnings()}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	row := func(label, value string) {
		if value != "" {
			fmt.Printf("%-17s %s\n", label+":", value)
		}
	}
	date := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.Format(time.RFC3339)
	}
	number := func(n int) string {
		if n == 0 {
			return ""
		}
		return fmt.Sprint(n)
	}
	i := out.Info
	format := i.Format
	if i.Strict {
		format += " (Strict)"
	}
	row("Format", format)
	row("Title", i.Title)
	row("Author", i.Author)
	row("Subject", i.Subject)
	row("Keywords", i.Keywords)
	row("Description", i.Description)
	row("Category", i.Category)
	row("Last modified by", i.LastModifiedBy)
	row("Revision", number(i.Revision))
	row("Created", date(i.Created))
	row("Modified", date(i.Modified))
	row("Last printed", date(i.LastPrinted))
	row("Application", i.Application)
	row("App version", i.AppVersion)
	row("Template", i.Template)
	row("Company", i.Company)
	row("Pages", number(i.Pages))
	row("Words", number(i.Words))
	row("Characters", number(i.Characters))
	row("Paragraphs", number(i.Paragraphs))
	for _, w := range out.Warnings {
		fmt.Printf("warning [%s]: %s\n", w.Code, w.Message)
	}
	return nil
}

// convert writes the document to standard output as Markdown or plain text.
func convert(ctx context.Context, args []string, markdown bool) error {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	toc := fs.Bool("toc", false, "keep the generated table of contents")
	placeholders := fs.Bool("placeholders", false, "keep the prompts of content controls that are not filled in")
	maxBytes := fs.Int("max", 0, "stop after about this many bytes (Markdown only)")
	warnings := fs.Bool("warnings", false, "print warnings to standard error")
	fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}
	doc, err := undocx.OpenFile(ctx, fs.Arg(0), undocx.Options{})
	if err != nil {
		return err
	}
	defer doc.Close()
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	read := undocx.ReadOptions{TOC: *toc, Placeholders: *placeholders}
	if markdown {
		err = doc.WriteMarkdown(ctx, out, undocx.MarkdownOptions{ReadOptions: read, MaxBytes: *maxBytes})
	} else {
		err = doc.WriteText(ctx, out, read)
	}
	if *warnings {
		for _, w := range doc.Warnings() {
			fmt.Fprintf(os.Stderr, "warning [%s]: %s\n", w.Code, w.Message)
		}
	}
	return err
}

// blocks prints one JSON object per block: its kind, level, style, heading
// path and text. For a table the text is a line for each row, with a tab
// between cells.
func blocks(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("blocks", flag.ExitOnError)
	toc := fs.Bool("toc", false, "keep the generated table of contents")
	fs.Parse(args)
	if fs.NArg() != 1 {
		usage()
	}
	doc, err := undocx.OpenFile(ctx, fs.Arg(0), undocx.Options{})
	if err != nil {
		return err
	}
	defer doc.Close()
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	enc := json.NewEncoder(out)
	for b, err := range doc.Blocks(ctx, undocx.ReadOptions{TOC: *toc}) {
		if err != nil {
			return err
		}
		out := struct {
			Kind      string   `json:"kind"`
			Level     int      `json:"level,omitempty"`
			Label     string   `json:"label,omitempty"`
			Style     string   `json:"style,omitempty"`
			Path      []string `json:"path,omitempty"`
			Rows      int      `json:"rows,omitempty"`
			Columns   int      `json:"columns,omitempty"`
			Continued bool     `json:"continued,omitempty"`
			Text      string   `json:"text"`
		}{Kind: b.Kind.String(), Level: b.Level, Label: b.Label, Style: b.Style, Path: b.Path}
		if b.Kind == undocx.Table {
			out.Rows, out.Columns, out.Continued = len(b.Table.Rows), b.Table.Columns, b.Table.Continued
			out.Text = b.Text()
		} else {
			out.Text = undocx.Block{Inlines: b.Inlines}.Text()
		}
		enc.Encode(out)
	}
	return nil
}
