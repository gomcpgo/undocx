package docxtest

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

// TestFixturesAreWellFormed checks what every fixture must have whatever it
// exercises: a package that opens, parts that parse, and the same bytes on
// every build.
func TestFixturesAreWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range Fixtures() {
		t.Run(f.Name, func(t *testing.T) {
			if seen[f.Name] {
				t.Fatalf("duplicate fixture name")
			}
			seen[f.Name] = true
			if f.About == "" || f.Text == "" {
				t.Errorf("fixture needs About and Text")
			}
			data := f.Doc().Bytes()
			if !bytes.Equal(data, f.Doc().Bytes()) {
				t.Errorf("two builds differ")
			}
			zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("not a zip: %v", err)
			}
			names := map[string]bool{}
			for _, zf := range zr.File {
				if names[zf.Name] {
					t.Errorf("part %s stored twice", zf.Name)
				}
				names[zf.Name] = true
				if !strings.HasSuffix(zf.Name, ".xml") && !strings.HasSuffix(zf.Name, ".rels") {
					continue
				}
				rc, err := zf.Open()
				if err != nil {
					t.Fatalf("%s: %v", zf.Name, err)
				}
				dec := xml.NewDecoder(rc)
				for {
					if _, err := dec.Token(); err == io.EOF {
						break
					} else if err != nil {
						t.Errorf("%s: %v", zf.Name, err)
						break
					}
				}
				rc.Close()
			}
			for _, want := range []string{"[Content_Types].xml", "_rels/.rels"} {
				if !names[want] {
					t.Errorf("no %s", want)
				}
			}
		})
	}
}
