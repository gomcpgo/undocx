package docxtest

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"unicode/utf16"
)

// Entry is one entry of a zip archive.
type Entry struct {
	// Name is stored as given: it may have a leading slash, backslashes
	// or anything else a test wants a reader to cope with.
	Name string
	Data []byte
	// Store keeps the data uncompressed.
	Store bool
	// Encrypted sets the flag that says the entry is encrypted, without
	// encrypting anything.
	Encrypted bool
}

// Zip writes a zip archive with the given entries, in order. Names may
// repeat.
func Zip(entries ...Entry) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.Name, Method: zip.Deflate}
		if e.Store {
			h.Method = zip.Store
		}
		if e.Encrypted {
			h.Flags |= 1
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			panic(err)
		}
		if _, err := w.Write(e.Data); err != nil {
			panic(err)
		}
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// CompoundFile returns a compound file, the container of the Office 97-2003
// formats and of password-protected Office documents, holding empty streams
// of the given names. It is laid out as [MS-CFB] version 3 describes: a
// header, one sector of allocation table, and the directory.
func CompoundFile(streams ...string) []byte {
	const (
		sectorSize = 512
		endOfChain = 0xFFFFFFFE
		fatSector  = 0xFFFFFFFD
		free       = 0xFFFFFFFF
	)
	dirSectors := (len(streams) + 1 + 3) / 4
	out := make([]byte, sectorSize*(2+dirSectors))
	le := binary.LittleEndian

	// Header.
	copy(out, []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1})
	le.PutUint16(out[24:], 0x003E) // minor version
	le.PutUint16(out[26:], 3)      // major version
	le.PutUint16(out[28:], 0xFFFE) // little-endian
	le.PutUint16(out[30:], 9)      // sector size 512
	le.PutUint16(out[32:], 6)      // mini sector size 64
	le.PutUint32(out[44:], 1)      // one allocation-table sector
	le.PutUint32(out[48:], 1)      // the directory starts in sector 1
	le.PutUint32(out[56:], 4096)   // mini stream cutoff
	le.PutUint32(out[60:], endOfChain)
	le.PutUint32(out[68:], endOfChain)
	for i := 0; i < 109; i++ {
		le.PutUint32(out[76+4*i:], free)
	}
	le.PutUint32(out[76:], 0) // the allocation table is sector 0

	// Allocation table: sector 0 is itself, the directory is a chain.
	fat := out[sectorSize : 2*sectorSize]
	for i := 0; i < sectorSize/4; i++ {
		le.PutUint32(fat[4*i:], free)
	}
	le.PutUint32(fat[0:], fatSector)
	for i := 1; i <= dirSectors; i++ {
		next := uint32(i + 1)
		if i == dirSectors {
			next = endOfChain
		}
		le.PutUint32(fat[4*i:], next)
	}

	// Directory: the root, then the streams as a chain of right siblings.
	dir := out[2*sectorSize:]
	entry := func(i int, name string, kind byte, right, child uint32) {
		e := dir[128*i : 128*(i+1)]
		units := utf16.Encode([]rune(name))
		for j, u := range units {
			le.PutUint16(e[2*j:], u)
		}
		le.PutUint16(e[64:], uint16(2*(len(units)+1)))
		e[66] = kind
		e[67] = 1 // black
		le.PutUint32(e[68:], free)
		le.PutUint32(e[72:], right)
		le.PutUint32(e[76:], child)
		le.PutUint32(e[116:], endOfChain)
	}
	child := uint32(free)
	if len(streams) > 0 {
		child = 1
	}
	entry(0, "Root Entry", 5, free, child)
	for i, name := range streams {
		right := uint32(i + 2)
		if i == len(streams)-1 {
			right = free
		}
		entry(i+1, name, 2, right, free)
	}
	for i := len(streams) + 1; i < 4*dirSectors; i++ {
		entry(i, "", 0, free, free)
		le.PutUint16(dir[128*i+64:], 0)
	}
	return out
}
