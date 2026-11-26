// Command undocx inspects Word documents.
//
//	undocx info [-json] file.docx
package main

import (
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
