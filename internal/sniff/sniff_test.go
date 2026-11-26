package sniff

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/gomcpgo/undocx/internal/docxtest"
)

func detect(data []byte) string {
	return Detect(bytes.NewReader(data), int64(len(data)))
}

func TestDetect(t *testing.T) {
	many := make([]string, 40) // a directory of several sectors
	for i := range many {
		many[i] = "Stream" + strings.Repeat("x", i%7)
	}
	many[37] = "WordDocument"

	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"empty", nil, Empty},
		{"zip", docxtest.New().Bytes(), Zip},
		{"empty zip", docxtest.Zip(), Zip},
		{"rtf", []byte(`{\rtf1\ansi Hello}`), RTF},
		{"pdf", []byte("%PDF-1.7\n"), PDF},
		{"html", []byte("<!DOCTYPE html><html><body>x</body></html>"), HTML},
		{"html after xml declaration", []byte(`<?xml version="1.0"?><html xmlns="http://www.w3.org/1999/xhtml"></html>`), HTML},
		{"web archive", []byte("MIME-Version: 1.0\r\nContent-Type: multipart/related\r\n"), MHT},
		{"word 2003 xml", []byte("\xef\xbb\xbf<?xml version=\"1.0\"?>\n<?mso-application progid=\"Word.Document\"?>\n<w:wordDocument xmlns:w=\"http://schemas.microsoft.com/office/word/2003/wordml\"/>"), Word2003XML},
		{"flat opc", []byte(`<?xml version="1.0"?><pkg:package xmlns:pkg="http://schemas.microsoft.com/office/2006/xmlPackage"/>`), FlatOPC},
		{"other xml", []byte(`<?xml version="1.0"?><root/>`), XML},
		{"text", []byte("Just some words.\n"), Unknown},
		{"binary", []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, Unknown},
		{"doc", docxtest.CompoundFile("1Table", "WordDocument", "\x05SummaryInformation"), Doc},
		{"encrypted", docxtest.CompoundFile("\x06DataSpaces", "EncryptionInfo", "EncryptedPackage"), Encrypted},
		{"encrypted, names in upper case", docxtest.CompoundFile("ENCRYPTIONINFO", "ENCRYPTEDPACKAGE"), Encrypted},
		{"xls", docxtest.CompoundFile("Workbook"), Xls},
		{"ppt", docxtest.CompoundFile("Current User", "PowerPoint Document"), Ppt},
		{"other compound file", docxtest.CompoundFile("__properties_version1.0"), OLE},
		{"compound file with no streams", docxtest.CompoundFile(), OLE},
		{"doc among many streams", docxtest.CompoundFile(many...), Doc},
		{"compound file header only", docxtest.CompoundFile("WordDocument")[:512], OLE},
		{"compound file cut short", docxtest.CompoundFile("WordDocument")[:700], OLE},
	}
	for _, tt := range tests {
		if got := detect(tt.data); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

// A directory chain that loops back on itself must not keep the reader busy.
func TestCompoundFileLoop(t *testing.T) {
	data := docxtest.CompoundFile("WordDocument")
	// Sector 0 is the allocation table; make the directory sector (1)
	// follow itself.
	binary.LittleEndian.PutUint32(data[512+4:], 1)
	if got := detect(data); got != Doc {
		t.Errorf("got %q, want %q", got, Doc)
	}
	// And a tree of siblings that points back at the root's child.
	data = docxtest.CompoundFile("A", "B")
	dir := data[2*512:]
	binary.LittleEndian.PutUint32(dir[128*2+72:], 1) // B.right = A
	binary.LittleEndian.PutUint32(dir[128*1+68:], 2) // A.left = B
	if got := detect(data); got != OLE {
		t.Errorf("got %q, want %q", got, OLE)
	}
}

func FuzzDetect(f *testing.F) {
	f.Add(docxtest.CompoundFile("WordDocument"))
	f.Add(docxtest.CompoundFile("EncryptedPackage", "EncryptionInfo"))
	f.Add(docxtest.New().Bytes())
	f.Add([]byte(`{\rtf1}`))
	f.Add([]byte(`<?xml version="1.0"?><w:wordDocument/>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		detect(data)
	})
}
