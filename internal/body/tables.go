package body

import (
	"slices"

	"github.com/gomcpgo/undocx/internal/model"
	"github.com/gomcpgo/undocx/internal/wml"
)

const (
	// WarnMergedCell: cells merged into the cell above them hold text of
	// their own.
	WarnMergedCell = "merged-cell-text"

	// maxTableRows is the number of rows after which a table is handed on
	// in parts, so that a table of any length is read in bounded memory.
	maxTableRows = 1000
	// maxHeaderRows is the most rows at the top of a table that count as
	// its header. Each part of a long table starts with them again.
	maxHeaderRows = 8
	// sliver is the distance, in twentieths of a point, under which two
	// column edges are one edge: an eighth of an inch. Word leaves columns
	// a hair wide behind when cells are dragged or tables pasted together.
	// Nothing can be read in a column this narrow: the space Word keeps
	// free at the two sides of a cell is wider.
	sliver = 180
)

// table is a table being read. Its rows are kept until it ends, because
// what to make of it shows only then: a table with one cell of text in each
// row is a frame around paragraphs, not a table.
type table struct {
	depth int   // of the w:tbl element
	outer *cell // the cell the table stands in, or nil
	row   *row  // the open row
	cell  *cell // the open cell

	// widths is the widths of the grid columns, as far as the table
	// states them, and -1 where it does not.
	widths []int
	rows   []gridRow
	// repeated is how many of the rows are header rows put in front of a
	// later part of a long table.
	repeated int
	total    int       // rows so far, over all parts
	parts    int       // parts handed on as tables
	headers  []gridRow // the header rows at the top, as read
}

// row and cell are a row and a cell being read.
type row struct {
	depth int
	props wml.RowProps
	cells []cell
}

type cell struct {
	depth  int
	props  wml.CellProps
	blocks []model.Block
}

// gridRow is a row that has been read, with its cells on the grid of
// columns the file gives.
type gridRow struct {
	header bool
	cells  []gridCell
}

type gridCell struct {
	col, span int
	merge     wml.Merge // its part in a merge down rows
	blocks    []model.Block
}

// closeTables ends the cells, rows and tables whose elements lie deeper
// than depth, the innermost first.
func (w *walker) closeTables(tables *[]*table, depth int) {
	for n := len(*tables); n > 0; n = len(*tables) {
		t := (*tables)[n-1]
		if t.cell != nil && t.cell.depth > depth {
			w.endCell(t)
		}
		if t.row != nil && t.row.depth > depth {
			w.endRow(t)
		}
		if t.depth <= depth {
			return
		}
		*tables = (*tables)[:n-1]
		w.handOn(t)
	}
}

func (w *walker) endCell(t *table) {
	// A paragraph cannot run on into the next cell.
	w.flushCarry()
	t.row.cells = append(t.row.cells, *t.cell)
	t.cell = nil
	w.cell = t.outer
}

// endRow places the cells of a row on the grid: a cell starts in the column
// after the cell before it, or after the columns the row leaves empty
// (w:gridBefore), and covers w:gridSpan columns.
func (w *walker) endRow(t *table) {
	r := t.row
	t.row = nil
	if r.props.Hidden && !filled(r.cells) {
		// A hidden row whose text is hidden too is not shown.
		return
	}
	cells := make([]gridCell, 0, len(r.cells))
	col, across := r.props.Before, false
	for i := range r.cells {
		c := &r.cells[i]
		if c.props.Deleted {
			continue
		}
		span := max(c.props.Span, 1)
		if n := len(cells); c.props.HMerge == wml.MergeContinue && across && n > 0 {
			// The older way to merge across columns: the cell belongs to
			// the one before it.
			cells[n-1].span += span
			cells[n-1].blocks = append(cells[n-1].blocks, c.blocks...)
			col += span
			continue
		}
		across = c.props.HMerge != wml.MergeNone
		cells = append(cells, gridCell{col, span, c.props.VMerge, c.blocks})
		col += span
	}
	if len(cells) == 0 {
		return
	}
	header := r.props.Header && len(t.headers) == t.total && t.total < maxHeaderRows
	if header {
		// Only rows at the top of a table are header rows. They are kept
		// as read, because each part of the table works out its own
		// columns.
		t.headers = append(t.headers, gridRow{true, slices.Clone(cells)})
	}
	t.rows = append(t.rows, gridRow{header, cells})
	t.total++
	if len(t.rows)-t.repeated >= maxTableRows && t.outer == nil {
		w.handOn(t)
		for _, h := range t.headers {
			t.rows = append(t.rows, gridRow{true, slices.Clone(h.cells)})
		}
		t.repeated = len(t.rows)
	}
}

// filled reports whether any of the cells holds something.
func filled(cells []cell) bool {
	for i := range cells {
		if len(cells[i].blocks) > 0 {
			return true
		}
	}
	return false
}

// columns renumbers the columns of the rows so that they are the columns a
// reader sees, and returns how many there are.
//
// The grid of a file has more lines than the table has columns. Every place
// where a cell ever ended stays a grid line, and Word leaves columns a hair
// wide between cells that look aligned. Read as they stand, such a table is
// all merged cells, and rows that look alike start in different columns. So
// only the grid lines where a cell of these rows starts or ends count, and
// lines closer together than a sliver are one line, unless a cell lies
// between them.
func columns(rows []gridRow, widths []int) int {
	end := 0
	for _, r := range rows {
		if n := len(r.cells); n > 0 {
			end = max(end, r.cells[n-1].col+r.cells[n-1].span)
		}
	}
	// narrowest[k] is where the narrowest cell that starts at line k ends,
	// or 0 when no cell starts there; used[k] says that a cell starts or
	// ends at line k.
	used, narrowest := make([]bool, end+1), make([]int, end+1)
	for _, r := range rows {
		for _, c := range r.cells {
			e := c.col + c.span
			used[c.col], used[e] = true, true
			if narrowest[c.col] == 0 || e < narrowest[c.col] {
				narrowest[c.col] = e
			}
		}
	}
	// index[k] is the column that starts at line k.
	index := make([]int, end+1)
	col, pos := -1, 0 // the column being formed, and where line k lies
	start, limit := 0, 0
	for k := 0; k <= end; k++ {
		if used[k] {
			// The line joins the one before it when it is within a sliver
			// of where that column started, and no cell lies between.
			if col < 0 || pos-start >= sliver || (limit != 0 && k >= limit) {
				col++
				start, limit = pos, 0
			}
			if n := narrowest[k]; n != 0 && (limit == 0 || n < limit) {
				limit = n
			}
		}
		index[k] = max(col, 0)
		if k < len(widths) && widths[k] > 0 {
			pos += widths[k]
		} else {
			// A width that is not stated, or stated as nothing, is not
			// known, and what is not known is not a sliver.
			pos += sliver
		}
	}
	for _, r := range rows {
		for i := range r.cells {
			c := &r.cells[i]
			c.col, c.span = index[c.col], index[c.col+c.span]-index[c.col]
		}
	}
	return max(col, 0)
}

// vmerge is a cell that the cell under it may be merged into.
type vmerge struct {
	col, span int
	cell      *model.Cell
}

// handOn makes a block of the rows read since the last one, and hands it
// on: to the cell the table stands in, or to whoever takes the blocks
// outside tables.
//
// A cell marked as continuing a merge down rows is part of the cell above
// it when that one starts in the same column, covers as many columns and is
// merged itself. Otherwise it is a cell of its own: LibreOffice reads such a
// cell so, and its text is not lost.
func (w *walker) handOn(t *table) {
	rows, repeated := t.rows, t.repeated
	t.rows, t.repeated = nil, 0
	if len(rows) == repeated {
		return
	}
	cols := columns(rows, t.widths)
	out := make([]model.Row, 0, len(rows))
	var merges, next []vmerge // the merged cells of the row before, and of this one
	// content says that a cell holds something, and wide that some row
	// has two cells that do.
	content, wide := false, false
	for i, r := range rows {
		row := model.Row{Header: r.header, Cells: make([]model.Cell, 0, len(r.cells))}
		next = next[:0]
		k, held := 0, 0
		for _, c := range r.cells {
			if c.merge == wml.MergeContinue {
				for k < len(merges) && merges[k].col < c.col {
					k++
				}
				if k < len(merges) && merges[k].col == c.col && merges[k].span == c.span {
					merges[k].cell.RowSpan++
					next = append(next, merges[k])
					if len(c.blocks) > 0 && i >= repeated {
						w.mergedText++
					}
					continue
				}
			}
			row.Cells = append(row.Cells, model.Cell{Col: c.col, ColSpan: c.span, RowSpan: 1, Blocks: c.blocks})
			if c.merge != wml.MergeNone {
				// row.Cells has room for every cell of the row, so it
				// does not move and the pointer stays good.
				next = append(next, vmerge{c.col, c.span, &row.Cells[len(row.Cells)-1]})
			}
			if len(c.blocks) > 0 {
				held++
				content = content || i >= repeated
			}
		}
		merges, next = next, merges
		wide = wide || held > 1
		out = append(out, row)
	}
	switch {
	case !content:
		// A table with nothing in it has nothing to say.
	case !wide:
		// No row has text in two cells. The table is a frame around text,
		// or text set in one column with empty cells for space beside it,
		// as pages made from HTML are. Its paragraphs are paragraphs of
		// what it stands in.
		for _, r := range out[repeated:] {
			for _, c := range r.Cells {
				for _, b := range c.Blocks {
					if b.Kind == model.Table || len(b.Inlines) > 0 {
						w.emit(b)
					}
				}
			}
		}
	default:
		w.emit(model.Block{Kind: model.Table, Table: &model.TableData{Columns: cols, Rows: out, Continued: t.parts > 0}})
		t.parts++
	}
}
