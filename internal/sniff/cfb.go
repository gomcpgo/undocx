package sniff

import (
	"encoding/binary"
	"io"
	"strings"
	"unicode/utf16"
)

// A compound file (the container of the Office 97-2003 formats, and of
// password-protected Office documents) is a small file system: sectors, an
// allocation table that chains them, and a directory of named streams. The
// layout is specified in [MS-CFB]. Only the directory is read here, to learn
// the names of the top-level streams, which say what the file is.

var oleMagic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

const (
	endOfChain = 0xFFFFFFFE
	freeSector = 0xFFFFFFFF
	noStream   = 0xFFFFFFFF

	maxFATSectors = 1 << 16 // enough for a 4 GB file with 512-byte sectors
	maxDirSectors = 1 << 12
	dirEntrySize  = 128
)

// compound classifies a compound file by its top-level streams.
func compound(r io.ReaderAt, size int64) string {
	names := topLevelNames(r, size)
	// Names in a compound file compare without regard to case.
	has := func(name string) bool {
		for _, n := range names {
			if strings.EqualFold(n, name) {
				return true
			}
		}
		return false
	}
	switch {
	case has("EncryptedPackage"), has("EncryptionInfo"):
		// [MS-OFFCRYPTO]: an encrypted package of any Office application.
		return Encrypted
	case has("WordDocument"):
		return Doc
	case has("Workbook"), has("Book"):
		return Xls
	case has("PowerPoint Document"):
		return Ppt
	}
	return OLE
}

// topLevelNames returns the names of the streams and storages directly under
// the root. It returns what it can find and never fails: a damaged file
// yields fewer names.
func topLevelNames(r io.ReaderAt, size int64) []string {
	var header [512]byte
	if n, _ := r.ReadAt(header[:], 0); n < len(header) {
		return nil
	}
	shift := binary.LittleEndian.Uint16(header[30:])
	if shift != 9 && shift != 12 {
		return nil
	}
	sectorSize := int64(1) << shift
	perSector := uint32(sectorSize / 4)

	sector := func(n uint32) []byte {
		off := (int64(n) + 1) << shift
		if n >= endOfChain-4 || off < 0 || off+sectorSize > size {
			return nil
		}
		buf := make([]byte, sectorSize)
		if got, _ := r.ReadAt(buf, off); int64(got) < sectorSize {
			return nil
		}
		return buf
	}

	// The sectors that hold the allocation table: the first 109 are listed
	// in the header, the rest in a chain of DIFAT sectors.
	var fat []uint32
	for i := 0; i < 109; i++ {
		if s := binary.LittleEndian.Uint32(header[76+4*i:]); s < endOfChain-4 {
			fat = append(fat, s)
		}
	}
	next := binary.LittleEndian.Uint32(header[68:])
	for steps := 0; next < endOfChain-4 && len(fat) < maxFATSectors && steps < maxFATSectors; steps++ {
		buf := sector(next)
		if buf == nil {
			break
		}
		for i := uint32(0); i < perSector-1; i++ {
			if s := binary.LittleEndian.Uint32(buf[4*i:]); s < endOfChain-4 {
				fat = append(fat, s)
			}
		}
		next = binary.LittleEndian.Uint32(buf[sectorSize-4:])
	}

	// following returns the sector after s in its chain.
	fatCache := map[uint32][]byte{}
	following := func(s uint32) uint32 {
		idx := s / perSector
		if int(idx) >= len(fat) {
			return endOfChain
		}
		buf, ok := fatCache[idx]
		if !ok {
			buf = sector(fat[idx])
			fatCache[idx] = buf
		}
		if buf == nil {
			return endOfChain
		}
		return binary.LittleEndian.Uint32(buf[4*(s%perSector):])
	}

	// Read the directory.
	type entry struct {
		name               string
		kind               byte
		left, right, child uint32
	}
	var entries []entry
	s := binary.LittleEndian.Uint32(header[48:])
	for steps := 0; s < endOfChain-4 && steps < maxDirSectors; steps++ {
		buf := sector(s)
		if buf == nil {
			break
		}
		for off := 0; off+dirEntrySize <= len(buf); off += dirEntrySize {
			e := buf[off : off+dirEntrySize]
			n := int(binary.LittleEndian.Uint16(e[64:]))
			var name string
			if n >= 2 && n <= 64 && n%2 == 0 {
				units := make([]uint16, n/2-1)
				for i := range units {
					units[i] = binary.LittleEndian.Uint16(e[2*i:])
				}
				name = string(utf16.Decode(units))
			}
			entries = append(entries, entry{name, e[66], binary.LittleEndian.Uint32(e[68:]),
				binary.LittleEndian.Uint32(e[72:]), binary.LittleEndian.Uint32(e[76:])})
		}
		s = following(s)
	}
	if len(entries) == 0 {
		return nil
	}

	// The children of the root form a binary tree linked by left and right.
	var names []string
	seen := make([]bool, len(entries))
	stack := []uint32{entries[0].child}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if i == noStream || int(i) >= len(entries) || seen[i] {
			continue
		}
		seen[i] = true
		e := entries[i]
		if e.kind == 1 || e.kind == 2 {
			names = append(names, e.name)
		}
		stack = append(stack, e.left, e.right)
	}
	if len(names) == 0 {
		// The tree is broken. Any entry is better than nothing.
		for _, e := range entries[1:] {
			if e.kind == 1 || e.kind == 2 {
				names = append(names, e.name)
			}
		}
	}
	return names
}
