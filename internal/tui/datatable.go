package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	tableCellPadding = 1
	tableGutterWidth = 2
)

type dataTable struct {
	columns  []table.Column
	rowAt    func(int) table.Row
	rowCount int
	cursor   int
	offset   int
	width    int
	height   int
	focused  bool
}

func newDataTable(columns []table.Column, height int, focused bool) dataTable {
	t := dataTable{
		columns: columns,
		cursor:  -1,
		height:  height,
		focused: focused,
	}
	t.clampOffset()
	return t
}

func (t dataTable) Update(msg tea.Msg) (dataTable, tea.Cmd) {
	if !t.focused {
		return t, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return t, nil
	}
	switch key.String() {
	case "up", "k":
		t.MoveUp(1)
	case "down", "j":
		t.MoveDown(1)
	case "pgup", "b":
		t.ScrollUp(t.visibleRows())
	case "pgdown", "f", " ":
		t.ScrollDown(t.visibleRows())
	case "home", "g":
		if t.cursor < 0 {
			t.ScrollToTop()
		} else {
			t.SetCursor(0)
		}
	case "end", "G":
		if t.cursor < 0 {
			t.ScrollToBottom()
		} else {
			t.SetCursor(t.rowCount - 1)
		}
	}
	return t, nil
}

func (t dataTable) View() string {
	if t.height < 1 {
		return ""
	}
	lines := make([]string, 0, t.height)
	lines = append(lines, t.renderHeader())
	visible := t.visibleRows()
	for row := 0; row < visible; row++ {
		idx := t.offset + row
		if idx >= 0 && idx < t.rowCount {
			lines = append(lines, t.renderRow(t.rowAt(idx), idx == t.cursor, row, visible))
			continue
		}
		lines = append(lines, t.renderEmptyRow(row, visible))
	}
	return strings.Join(lines, "\n")
}

func (t *dataTable) SetWidth(width int) {
	if width < 1 {
		width = 1
	}
	t.width = width
}

func (t *dataTable) SetHeight(height int) {
	if height < 1 {
		height = 1
	}
	t.height = height
	t.clampOffset()
}

func (t *dataTable) SetColumns(columns []table.Column) {
	t.columns = columns
}

func (t *dataTable) SetRows(rows []table.Row) {
	t.SetVirtualRows(len(rows), func(index int) table.Row {
		return rows[index]
	})
}

func (t *dataTable) SetVirtualRows(count int, rowAt func(int) table.Row) {
	if count < 0 {
		count = 0
	}
	t.rowCount = count
	t.rowAt = rowAt
	if t.rowAt == nil {
		t.rowAt = func(int) table.Row { return nil }
	}
	if t.cursor > t.rowCount-1 {
		t.cursor = t.rowCount - 1
	}
	if t.rowCount == 0 {
		t.cursor = -1
	}
	t.clampOffset()
}

func (t *dataTable) SetCursor(cursor int) {
	if t.rowCount == 0 {
		t.cursor = -1
		t.offset = 0
		return
	}
	if cursor < 0 {
		t.cursor = -1
		t.clampOffset()
		return
	}
	if cursor > t.rowCount-1 {
		cursor = t.rowCount - 1
	}
	t.cursor = cursor
	t.ensureCursorVisible()
}

func (t *dataTable) SetCursorPreserveOffset(cursor int) {
	if t.rowCount == 0 {
		t.cursor = -1
		t.offset = 0
		return
	}
	if cursor < 0 {
		t.cursor = -1
		t.clampOffset()
		return
	}
	if cursor > t.rowCount-1 {
		cursor = t.rowCount - 1
	}
	t.cursor = cursor
	t.clampOffset()
}

func (t *dataTable) Cursor() int {
	return t.cursor
}

func (t *dataTable) Columns() []table.Column {
	return t.columns
}

func (t *dataTable) Height() int {
	return t.height
}

func (t *dataTable) Width() int {
	return t.width
}

func (t *dataTable) Focus() {
	t.focused = true
}

func (t *dataTable) Blur() {
	t.focused = false
}

func (t *dataTable) MoveUp(n int) {
	if t.cursor < 0 {
		t.SetCursor(min(t.rowCount-1, t.offset+t.visibleRows()-1))
		return
	}
	t.SetCursor(t.cursor - n)
}

func (t *dataTable) MoveDown(n int) {
	if t.cursor < 0 {
		t.SetCursor(t.offset)
		return
	}
	t.SetCursor(t.cursor + n)
}

func (t *dataTable) ScrollUp(n int) {
	t.offset -= n
	t.clampOffset()
}

func (t *dataTable) ScrollDown(n int) {
	t.offset += n
	t.clampOffset()
}

func (t *dataTable) ScrollToTop() {
	t.offset = 0
	t.clampOffset()
}

func (t *dataTable) ScrollToBottom() {
	t.offset = t.rowCount - t.visibleRows()
	t.clampOffset()
}

func (t dataTable) visibleRows() int {
	if t.height <= 1 {
		return 0
	}
	return t.height - 1
}

func (t *dataTable) clampOffset() {
	visible := t.visibleRows()
	if visible <= 0 || t.rowCount == 0 {
		t.offset = 0
		return
	}
	maxOffset := t.rowCount - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if t.offset > maxOffset {
		t.offset = maxOffset
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

func (t *dataTable) ensureCursorVisible() {
	visible := t.visibleRows()
	if visible <= 0 || t.rowCount == 0 {
		t.offset = 0
		return
	}
	if t.cursor < 0 {
		t.clampOffset()
		return
	}
	if t.cursor > t.rowCount-1 {
		t.cursor = t.rowCount - 1
	}
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+visible {
		t.offset = t.cursor - visible + 1
	}
	t.clampOffset()
}

func (t dataTable) renderHeader() string {
	content := t.renderHeaderCells()
	return tableHeaderBarStyle.Render(t.fit(tableHeaderStyle.Render(strings.Repeat(" ", tableGutterWidth)) + content + "  "))
}

func (t dataTable) renderRow(row table.Row, selected bool, visibleRow int, visibleRows int) string {
	style := tableCellStyle
	scrollbarTrackStyle := tableScrollbarTrackStyle
	scrollbarThumbStyle := tableScrollbarThumbStyle
	if selected {
		style = tableSelectedCellStyle
		scrollbarTrackStyle = tableSelectedScrollbarTrackStyle
		scrollbarThumbStyle = tableSelectedScrollbarThumbStyle
	}
	content := t.renderCells(row, style)
	prefix := "  "
	if selected {
		prefix = "> "
	}
	line := style.Render(prefix) + content + style.Render(" ") + t.scrollbar(visibleRow, visibleRows, scrollbarTrackStyle, scrollbarThumbStyle)
	return t.fit(line)
}

func (t dataTable) renderEmptyRow(visibleRow int, visibleRows int) string {
	style := tableCellStyle
	scrollbarTrackStyle := tableScrollbarTrackStyle
	scrollbarThumbStyle := tableScrollbarThumbStyle
	return t.fit(style.Render("  "+strings.Repeat(" ", t.contentWidth())+" ") + t.scrollbar(visibleRow, visibleRows, scrollbarTrackStyle, scrollbarThumbStyle))
}

func (t dataTable) renderCells(row table.Row, style lipgloss.Style) string {
	cells := make([]string, 0, len(t.columns))
	for i, column := range t.columns {
		if column.Width <= 0 {
			continue
		}
		value := ""
		if i < len(row) {
			value = row[i]
		}
		cell := paddedCell(value, column.Width)
		cell = style.Render(cell)
		cells = append(cells, cell)
	}
	return strings.Join(cells, "")
}

func (t dataTable) renderHeaderCells() string {
	cells := make([]string, 0, len(t.columns))
	for _, column := range t.columns {
		if column.Width <= 0 {
			continue
		}
		cell := paddedCell(column.Title, column.Width)
		if isActiveSortTitle(column.Title) {
			activeWidth := min(column.Width, ansi.StringWidth(strings.TrimRight(cell, " "))+tableCellPadding)
			if activeWidth < column.Width {
				cell = tableActiveHeaderStyle.Render(ansi.Truncate(cell, activeWidth, "")) +
					tableHeaderStyle.Render(strings.Repeat(" ", column.Width-activeWidth))
			} else {
				cell = tableActiveHeaderStyle.Render(cell)
			}
		} else {
			cell = tableHeaderStyle.Render(cell)
		}
		cells = append(cells, cell)
	}
	return strings.Join(cells, "")
}

func (t dataTable) scrollbar(visibleRow int, visibleRows int, trackStyle lipgloss.Style, thumbStyle lipgloss.Style) string {
	if t.rowCount <= visibleRows || visibleRows <= 0 {
		return trackStyle.Render(" ")
	}
	thumbSize := visibleRows * visibleRows / t.rowCount
	if thumbSize < 1 {
		thumbSize = 1
	}
	if thumbSize > visibleRows {
		thumbSize = visibleRows
	}
	maxThumbStart := visibleRows - thumbSize
	thumbStart := 0
	if t.rowCount > 1 && maxThumbStart > 0 {
		thumbStart = t.offset * maxThumbStart / max(1, t.rowCount-visibleRows)
	}
	if visibleRow >= thumbStart && visibleRow < thumbStart+thumbSize {
		return thumbStyle.Render("█")
	}
	return trackStyle.Render("░")
}

func (t dataTable) contentWidth() int {
	width := 0
	for _, column := range t.columns {
		if column.Width > 0 {
			width += column.Width
		}
	}
	return width
}

func (t dataTable) fit(line string) string {
	if t.width <= 0 {
		return line
	}
	return ansi.Truncate(line, t.width, "")
}

func headerCells(columns []table.Column) table.Row {
	row := make(table.Row, 0, len(columns))
	for _, column := range columns {
		row = append(row, column.Title)
	}
	return row
}

func isActiveSortTitle(title string) bool {
	return strings.HasSuffix(title, " ▲") || strings.HasSuffix(title, " ▼")
}

func paddedCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if width <= tableCellPadding*2 {
		return ansi.Truncate(value, width, "")
	}
	innerWidth := width - tableCellPadding*2
	value = ansi.Truncate(value, innerWidth, "")
	cell := strings.Repeat(" ", tableCellPadding) + value
	cell += strings.Repeat(" ", max(0, width-ansi.StringWidth(cell)))
	return cell
}

var (
	tableHeaderBarStyle              = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Background(lipgloss.Color("235"))
	tableHeaderStyle                 = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Bold(true)
	tableActiveHeaderStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("16")).Background(lipgloss.Color("80")).Bold(true)
	tableCellStyle                   = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	tableSelectedCellStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("24"))
	tableScrollbarTrackStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
	tableScrollbarThumbStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("80")).Bold(true)
	tableSelectedScrollbarTrackStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("67")).Background(lipgloss.Color("24"))
	tableSelectedScrollbarThumbStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("24")).Bold(true)
)
