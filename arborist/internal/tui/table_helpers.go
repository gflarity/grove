package tui

import (
	"github.com/charmbracelet/bubbles/table"
)

// ColumnSpec defines a titled column with a proportional weight.
type ColumnSpec struct {
	Title  string
	Weight int
}

// Column layouts for each table type. Single source of truth.
var (
	resourceColumnSpecs = []ColumnSpec{
		{Title: "NAMESPACE", Weight: 2},
		{Title: "TYPE", Weight: 3},
		{Title: "NAME", Weight: 5},
		{Title: "TOPOLOGY", Weight: 3},
		{Title: "READY", Weight: 2},
		{Title: "SCHEDULED", Weight: 2}, // Title overridden to "PHASE" for pod views
	}

	eventColumnSpecs = []ColumnSpec{
		{Title: "TYPE", Weight: 2},
		{Title: "KIND", Weight: 3},
		{Title: "REASON", Weight: 3},
		{Title: "AGE", Weight: 1},
		{Title: "FROM", Weight: 3},
		{Title: "MESSAGE", Weight: 8},
	}

	topologyDomainColumnSpecs = []ColumnSpec{
		{Title: "DOMAIN", Weight: 2},
		{Title: "KEY", Weight: 5},
		{Title: "VALUES", Weight: 1},
	}

	topologyDrillValueColumnSpecs = []ColumnSpec{
		{Title: "VALUE", Weight: 1},
	}

	topologyPodColumnSpecs = []ColumnSpec{
		{Title: "NAMESPACE", Weight: 2},
		{Title: "NODE", Weight: 2},
		{Title: "NAME", Weight: 4},
		{Title: "TOPOLOGY", Weight: 3},
		{Title: "PHASE", Weight: 1},
	}

	containerColumnSpecs = []ColumnSpec{
		{Title: "NAME", Weight: 3},
		{Title: "IMAGE", Weight: 5},
		{Title: "STATE", Weight: 2},
		{Title: "READY", Weight: 1},
		{Title: "RESTARTS", Weight: 1},
	}
)

// computeWeightedColumns takes a column spec and available width, and returns
// []table.Column with widths distributed proportionally by weight.
// The last column absorbs any rounding remainder so the table fills exactly.
func computeWeightedColumns(specs []ColumnSpec, availableWidth int) []table.Column {
	if len(specs) == 0 {
		return nil
	}

	totalWeight := 0
	for _, s := range specs {
		totalWeight += s.Weight
	}
	if totalWeight == 0 {
		totalWeight = len(specs) // fallback: equal weights
	}

	unit := availableWidth / totalWeight
	columns := make([]table.Column, len(specs))
	usedWidth := 0
	for i, s := range specs {
		w := unit * s.Weight
		columns[i] = table.Column{Title: s.Title, Width: w}
		usedWidth += w
	}
	// Last column absorbs rounding remainder
	columns[len(columns)-1].Width += availableWidth - usedWidth

	return columns
}

// tableContentWidth returns the usable content width for a table given terminal
// width, frame border overhead, and per-cell padding (2 chars per column for
// the Padding(0,1) style).
func tableContentWidth(termWidth, numColumns int) int {
	const frameBorders = 2 // 1 char on each side for the rounded border
	cellPadding := numColumns * 2
	w := termWidth - frameBorders - cellPadding
	if w < 80 {
		w = 80
	}
	return w
}

// saveCursorState saves the name at nameCol and the current cursor index for later restoration.
func saveCursorState(t *table.Model, nameCol int) (string, int) {
	prevName := ""
	if row := t.SelectedRow(); len(row) > nameCol {
		prevName = row[nameCol]
	}
	return prevName, t.Cursor()
}

// restoreCursorState tries to find the row with prevName at nameCol, otherwise clamps
// the cursor to valid range.
func restoreCursorState(t *table.Model, rows []table.Row, prevName string, prevCursor, nameCol int) {
	if len(rows) == 0 {
		return
	}
	if prevName != "" {
		for i, row := range rows {
			if len(row) > nameCol && row[nameCol] == prevName {
				t.SetCursor(i)
				return
			}
		}
	}
	if prevCursor >= len(rows) {
		t.SetCursor(len(rows) - 1)
	} else if prevCursor >= 0 {
		t.SetCursor(prevCursor)
	} else {
		t.SetCursor(0)
	}
}

// tableRebuildConfig holds the parameters for the rebuildTable helper.
type tableRebuildConfig struct {
	table   *table.Model
	specs   []ColumnSpec
	nameCol int
	width   int
}

// rebuildTable performs the standard table rebuild: save cursor → clear rows →
// set columns → build rows → set rows → restore cursor. The only variation
// between callers is which table, specs, name column, and how rows are built.
func rebuildTable(cfg tableRebuildConfig, buildRows func() []table.Row) {
	prevName, prevCursor := saveCursorState(cfg.table, cfg.nameCol)
	cfg.table.SetRows([]table.Row{})
	w := tableContentWidth(cfg.width, len(cfg.specs))
	cfg.table.SetColumns(computeWeightedColumns(cfg.specs, w))
	rows := buildRows()
	cfg.table.SetRows(rows)
	restoreCursorState(cfg.table, rows, prevName, prevCursor, cfg.nameCol)
}

// createTableModel creates a table.Model with the given column spec and focus state.
// This is the shared factory used by all table types in the TUI.
func createTableModel(specs []ColumnSpec, focused bool) table.Model {
	columns := computeWeightedColumns(specs, 80) // placeholder widths until first resize
	t := table.New(
		table.WithColumns(columns),
		table.WithRows([]table.Row{}),
		table.WithFocused(focused),
		table.WithHeight(10),
	)
	t.SetStyles(ArboristTableStyles())
	return t
}

// resizeTable sets width, height, and styles on a table.
func resizeTable(t *table.Model, width, height int) {
	t.SetWidth(width)
	t.SetHeight(height)
	t.SetStyles(ArboristTableStylesWithWidth(width))
}
