// Command gen writes the fixture documents of package docxtest to a folder,
// for tools that read files: other converters, schema validators, the
// comparison harness.
//
//	go run ./internal/docxtest/gen -o ../corpus/files/fixtures -expected ../corpus/expected/fixtures
//
// With -expected it also writes, for each fixture, the text Word displays
// (name.txt) and the checks that go with it (name.json).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gomcpgo/undocx/internal/docxtest"
)

func main() {
	out := flag.String("o", "", "folder to write the .docx files to")
	expected := flag.String("expected", "", "folder to write the expected text and checks to")
	flag.Parse()
	if *out == "" {
		fmt.Fprintln(os.Stderr, "usage: gen -o dir [-expected dir]")
		os.Exit(2)
	}
	if err := run(*out, *expected); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type checks struct {
	About       string   `json:"about"`
	Must        []string `json:"must,omitempty"`
	MustNot     []string `json:"must_not,omitempty"`
	Once        []string `json:"once,omitempty"`
	Markdown    []string `json:"markdown,omitempty"`
	MarkdownNot []string `json:"markdown_not,omitempty"`
}

func run(out, expected string) error {
	for _, dir := range []string{out, expected} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	for _, f := range docxtest.Fixtures() {
		if err := os.WriteFile(filepath.Join(out, f.Name+".docx"), f.Doc().Bytes(), 0o644); err != nil {
			return err
		}
		if expected == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(expected, f.Name+".txt"), []byte(f.Text+"\n"), 0o644); err != nil {
			return err
		}
		data, err := json.MarshalIndent(checks{f.About, f.Must, f.MustNot, f.Once, f.Markdown, f.MarkdownNot}, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(expected, f.Name+".json"), append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("wrote %d fixtures to %s\n", len(docxtest.Fixtures()), out)
	return nil
}
