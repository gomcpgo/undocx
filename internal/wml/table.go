package wml

import (
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/gomcpgo/undocx/internal/xmlstream"
)

// MaxSpan is the most grid columns one cell may span, and the most a row
// may leave empty before or after its cells. Word allows 63 columns in a
// table; a larger value in a file is capped, so that a cell cannot claim a
// width that the output would then have to fill.
const MaxSpan = 63

// RowProps is what a w:trPr element states, of the properties the reader
// uses.
type RowProps struct {
	// Deleted reports that the row is a tracked deletion: with the change
	// accepted, it is gone.
	Deleted bool
	// Header reports that the row is repeated at the top of each page
	// (w:tblHeader), which is how Word marks a header row.
	Header bool
	// Hidden reports that the row's end mark is hidden (w:hidden). A row
	// whose text is hidden too is not shown at all.
	Hidden bool
	// Before and After are the grid columns the row leaves empty before
	// its first cell and after its last (w:gridBefore, w:gridAfter).
	Before, After int
}

// Merge says what part a cell plays in a merged group.
type Merge int8

const (
	// MergeNone: the cell is not merged.
	MergeNone Merge = iota
	// MergeRestart: the cell begins a merged group.
	MergeRestart
	// MergeContinue: the cell is part of the group begun before it.
	MergeContinue
)

// CellProps is what a w:tcPr element states, of the properties the reader
// uses.
type CellProps struct {
	// Span is the number of grid columns the cell covers (w:gridSpan), at
	// least 1.
	Span int
	// VMerge is the cell's part in a group merged down rows (w:vMerge).
	VMerge Merge
	// HMerge is its part in a group merged across columns the older way
	// (w:hMerge), which Word still reads.
	HMerge Merge
	// Deleted reports that the cell is a tracked deletion (w:cellDel).
	Deleted bool
}

// ReadRowProps reads a w:trPr element whose start tag has just been read.
func ReadRowProps(x *xmlstream.Reader) (RowProps, error) {
	var p RowProps
	// Only the children are looked at: the earlier properties that a
	// tracked change keeps (w:trPrChange) are not the properties.
	err := children(x, func(el xml.StartElement) error {
		switch el.Name.Local {
		case "del":
			p.Deleted = true
		case "tblHeader":
			p.Header = Bool(el) == On
		case "hidden":
			p.Hidden = Bool(el) == On
		case "gridBefore":
			p.Before = span(el, 0)
		case "gridAfter":
			p.After = span(el, 0)
		}
		return x.Skip()
	})
	return p, err
}

// ReadCellProps reads a w:tcPr element whose start tag has just been read.
func ReadCellProps(x *xmlstream.Reader) (CellProps, error) {
	p := CellProps{Span: 1}
	err := children(x, func(el xml.StartElement) error {
		switch el.Name.Local {
		case "gridSpan":
			p.Span = span(el, 1)
		case "vMerge":
			p.VMerge = merge(Val(el))
		case "hMerge":
			p.HMerge = merge(Val(el))
		case "cellDel":
			p.Deleted = true
		case "cellMerge":
			// A merge that is a tracked change: w:vMerge says what the cell
			// is once the change is accepted.
			switch Attr(el, "vMerge") {
			case "rest":
				p.VMerge = MergeRestart
			case "cont":
				p.VMerge = MergeContinue
			}
		}
		return x.Skip()
	})
	return p, err
}

// merge reads the value of w:vMerge or w:hMerge. Left out, it means the
// cell continues a group (ECMA-376 Part 1, 17.4.85).
func merge(val string) Merge {
	if val == "restart" {
		return MergeRestart
	}
	return MergeContinue
}

// span reads a count of grid columns, between least and MaxSpan.
func span(el xml.StartElement, least int) int {
	n, ok := number(Val(el))
	if !ok {
		return least
	}
	return min(max(n, least), MaxSpan)
}

// MaxGridColumns is the most columns of a w:tblGrid whose widths are kept.
const MaxGridColumns = 1024

// ReadGrid reads a w:tblGrid element whose start tag has just been read,
// and returns the widths of the grid columns in twentieths of a point. A
// width that is missing or not understood is -1.
func ReadGrid(x *xmlstream.Reader) ([]int, error) {
	var widths []int
	// Only the children: the earlier grid that a tracked change keeps
	// (w:tblGridChange) is not the grid.
	err := children(x, func(el xml.StartElement) error {
		if el.Name.Local == "gridCol" && len(widths) < MaxGridColumns {
			widths = append(widths, twips(Attr(el, "w")))
		}
		return x.Skip()
	})
	return widths, err
}

// twips reads a measure in twentieths of a point: a whole number, or a
// number with a unit, which the standard also allows ("12pt", "0.5in").
// It returns -1 for anything else.
func twips(s string) int {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 {
			return -1
		}
		return n
	}
	if len(s) < 3 {
		return -1
	}
	per := 0.0
	switch s[len(s)-2:] {
	case "pt":
		per = 20
	case "in":
		per = 1440
	case "cm":
		per = 1440 / 2.54
	case "mm":
		per = 1440 / 25.4
	case "pc", "pi":
		per = 240
	default:
		return -1
	}
	f, err := strconv.ParseFloat(s[:len(s)-2], 64)
	if err != nil || f < 0 || f > 1e6 {
		return -1
	}
	return int(f * per)
}
