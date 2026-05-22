package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpuguard/internal/api"
	"cpuguard/internal/model"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestPrepareActionRequiresThrottledSubject(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.subjects = []model.Subject{{ID: "s1", State: model.StateObserving}}
	m.subjectTable.SetRows(subjectRows(m.subjects))

	next, cmd := m.prepareAction(actionUnthrottle)
	if cmd != nil {
		t.Fatal("expected no command")
	}
	updated := next.(Model)
	if updated.pendingAction != actionNone {
		t.Fatalf("pending action = %q, want none", updated.pendingAction)
	}
}

func TestConfirmCancelClearsPendingAction(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.pendingAction = actionUnthrottle
	m.pendingSubject = &model.Subject{ID: "s1", TargetRef: "node#1000"}

	next, cmd := m.handleConfirm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	updated := next.(Model)
	if updated.pendingAction != actionNone || updated.pendingSubject != nil {
		t.Fatalf("pending action was not cleared: %#v", updated.pendingAction)
	}
}

func TestViewIsClippedToTerminalWidth(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 50
	m.height = 18
	m.resizeTables()
	m.subjects = []model.Subject{{
		ID:              "s1",
		State:           model.StateThrottled,
		LastCPUPct:      123.4,
		CurrentLimitPct: 61.7,
		Scope:           model.ScopeHostProcess,
		TargetRef:       "very-long-process-name-with-many-details#12345",
		RuleID:          "host-all-user-processes",
	}}
	m.subjectTable.SetRows(subjectRows(m.subjects))
	view := m.View()
	for _, line := range strings.Split(view, "\n") {
		if width := ansi.StringWidth(line); width > m.width {
			t.Fatalf("line width = %d, want <= %d: %q", width, m.width, line)
		}
	}
}

func TestHeaderStaysSingleLineAtNarrowWidth(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 40
	m.lastError = "very long daemon error that must not wrap the header"
	m.subjects = []model.Subject{
		{ID: "s1", State: model.StateThrottled},
		{ID: "s2", State: model.StateManualHold},
	}

	header := m.header()
	lines := strings.Split(header, "\n")
	if len(lines) != 1 {
		t.Fatalf("header line count = %d, want 1: %q", len(lines), header)
	}
	if width := ansi.StringWidth(header); width > m.width {
		t.Fatalf("header width = %d, want <= %d: %q", width, m.width, header)
	}
}

func TestHeaderLayoutKeepsRefreshControlVisibleWhenPossible(t *testing.T) {
	content, minusStart, minusEnd, plusStart, plusEnd := headerLayout(40, "CPUGUARD daemon with very long status", "refresh [-] 1s [+]")
	if strings.Contains(content, "\n") {
		t.Fatalf("header content wrapped: %q", content)
	}
	if !strings.Contains(content, "refresh [-] 1s [+]") {
		t.Fatalf("refresh control missing: %q", content)
	}
	if minusStart < 0 || minusEnd < minusStart || plusStart < 0 || plusEnd < plusStart {
		t.Fatalf("invalid bounds: minus=%d-%d plus=%d-%d", minusStart, minusEnd, plusStart, plusEnd)
	}
}

func TestTrendShowsReadableLabelsAndValues(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 120
	m.systemCPUHistory = []float64{10, 40, 80}
	m.throttledHistory = []int{0, 2, 1}

	trend := ansi.Strip(m.trend())
	if !strings.Contains(trend, "SYS CPU") || !strings.Contains(trend, "THROTTLED") {
		t.Fatalf("trend labels missing: %q", trend)
	}
	if !strings.Contains(trend, "now 80.0") || !strings.Contains(trend, "peak 80.0") {
		t.Fatalf("cpu values missing: %q", trend)
	}
	if !strings.Contains(trend, "now 1") || !strings.Contains(trend, "peak 2") {
		t.Fatalf("throttled values missing: %q", trend)
	}
}

func TestTrendIsClippedToWidth(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 60
	m.systemCPUHistory = []float64{10, 40, 80}
	m.throttledHistory = []int{0, 2, 1}

	trend := m.trend()
	if width := ansi.StringWidth(trend); width > m.width {
		t.Fatalf("trend width = %d, want <= %d: %q", width, m.width, trend)
	}
	if stripped := ansi.Strip(trend); !strings.Contains(stripped, "SYS CPU") || !strings.Contains(stripped, "THROTTLED") {
		t.Fatalf("compact trend lost a widget: %q", stripped)
	}
}

func TestSparklineUsesBlockGlyphs(t *testing.T) {
	line := sparkline([]float64{0, 1, 2, 3, 4}, 5)
	if strings.ContainsAny(line, ".^-_") {
		t.Fatalf("sparkline still uses rough ascii glyphs: %q", line)
	}
	if ansi.StringWidth(line) != 5 {
		t.Fatalf("sparkline width = %d, want 5: %q", ansi.StringWidth(line), line)
	}
}

func TestPercentSparklineUsesFixedPercentScale(t *testing.T) {
	low := percentSparkline([]float64{10}, 1)
	high := percentSparkline([]float64{90}, 1)
	if low == high {
		t.Fatalf("percent sparkline should distinguish low and high values: low=%q high=%q", low, high)
	}
	if low == "█" {
		t.Fatalf("low percent should not render as full block: %q", low)
	}
}

func TestMetricsSeparatesSystemCPUFromTrackedCPU(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 140
	temp := 66.5
	power := 18.2
	m.system = model.SystemMetrics{CPUPercent: 42.5, Load1: 1.25, CPUTempC: &temp, CPUPowerW: &power}
	m.subjects = []model.Subject{
		{ID: "s1", LastCPUPct: 10},
		{ID: "s2", LastCPUPct: 30},
	}

	metrics := ansi.Strip(m.metrics())
	for _, want := range []string{"sys cpu:", "42.5", "temp:", "66.5C", "power:", "18.2W", "load1:", "1.25", "tracked max:", "30.0", "tracked avg:", "20.0"} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("metrics missing %q: %q", want, metrics)
		}
	}
}

func TestMetricsShowsMissingTemperatureAndPowerAsDashes(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 120

	metrics := ansi.Strip(m.metrics())
	for _, want := range []string{"temp: --", "power: --"} {
		if !strings.Contains(metrics, want) {
			t.Fatalf("metrics missing %q: %q", want, metrics)
		}
	}
}

func TestRecordHistoryUsesSystemCPU(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.system = model.SystemMetrics{CPUPercent: 42.5}
	m.subjects = []model.Subject{{ID: "s1", LastCPUPct: 90}}

	m.recordHistory()
	if got := latestFloat(m.systemCPUHistory); got != 42.5 {
		t.Fatalf("system cpu history = %.1f, want 42.5", got)
	}
}

func TestMouseClickSelectsEventRowUnderCursor(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 90
	m.height = 24
	m.resizeTables()
	m.events = []model.Event{
		{ID: 1, Type: "first"},
		{ID: 2, Type: "second"},
		{ID: 3, Type: "third"},
	}
	m.eventTable.SetRows(eventRows(m.events))

	next, _ := m.handleMouse(tea.MouseMsg(tea.MouseEvent{
		X:      2,
		Y:      m.eventTop + 4,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}))
	updated := next.(Model)
	if updated.eventTable.Cursor() != 1 {
		t.Fatalf("event cursor = %d, want 1", updated.eventTable.Cursor())
	}
}

func TestMouseClickSelectsEventRowAfterScroll(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 90
	m.height = 24
	m.resizeTables()
	m.events = []model.Event{
		{ID: 1, Type: "first"},
		{ID: 2, Type: "second"},
		{ID: 3, Type: "third"},
		{ID: 4, Type: "fourth"},
		{ID: 5, Type: "fifth"},
		{ID: 6, Type: "sixth"},
		{ID: 7, Type: "seventh"},
		{ID: 8, Type: "eighth"},
	}
	m.eventTable.SetRows(eventRows(m.events))
	m.eventTable.ScrollDown(2)

	next, _ := m.handleMouse(tea.MouseMsg(tea.MouseEvent{
		X:      2,
		Y:      m.eventTop + 3,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}))
	updated := next.(Model)
	if updated.eventTable.Cursor() != 2 {
		t.Fatalf("event cursor = %d, want 2", updated.eventTable.Cursor())
	}
}

func TestSelectSubjectIDForEventsKeepsExistingSelection(t *testing.T) {
	subjects := []model.Subject{
		{ID: "low", State: model.StateObserving, LastCPUPct: 1},
		{ID: "high", State: model.StateObserving, LastCPUPct: 90},
	}
	if got := selectSubjectIDForEvents(subjects, "low", true); got != "low" {
		t.Fatalf("selected subject = %q, want low", got)
	}
}

func TestSelectSubjectIDForEventsFallsBackToTopSubject(t *testing.T) {
	subjects := []model.Subject{
		{ID: "low", State: model.StateObserving, LastCPUPct: 1},
		{ID: "high", State: model.StateObserving, LastCPUPct: 90},
	}
	if got := selectSubjectIDForEvents(subjects, "missing", true); got != "high" {
		t.Fatalf("selected subject = %q, want high", got)
	}
}

func TestSelectSubjectIDForEventsCanSkipAutoSelection(t *testing.T) {
	subjects := []model.Subject{
		{ID: "low", State: model.StateObserving, LastCPUPct: 1},
		{ID: "high", State: model.StateObserving, LastCPUPct: 90},
	}
	if got := selectSubjectIDForEvents(subjects, "", false); got != "" {
		t.Fatalf("selected subject = %q, want empty", got)
	}
}

func TestDefaultRefreshIntervalIsOneSecond(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	if m.refreshInterval != time.Second {
		t.Fatalf("refresh interval = %s, want 1s", m.refreshInterval)
	}
}

func TestDefaultSubjectSortIsCPUDescending(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	if m.subjectSortKey != subjectSortCPU || m.subjectSortDir != sortDesc {
		t.Fatalf("subject sort = %q/%v, want cpu/desc", m.subjectSortKey, m.subjectSortDir)
	}
}

func TestNextRefreshIntervalUsesBtopLikeSteps(t *testing.T) {
	if got := nextRefreshInterval(time.Second, 1); got != 2*time.Second {
		t.Fatalf("next interval = %s, want 2s", got)
	}
	if got := nextRefreshInterval(2*time.Second, -1); got != time.Second {
		t.Fatalf("previous interval = %s, want 1s", got)
	}
	if got := nextRefreshInterval(30*time.Second, 1); got != 30*time.Second {
		t.Fatalf("max interval = %s, want 30s", got)
	}
}

func TestColumnSortKeyAtX(t *testing.T) {
	columns := []table.Column{
		{Title: "STATE", Width: 12},
		{Title: "CPU ▼", Width: 7},
		{Title: "LIMIT", Width: 7},
	}
	key, ok := subjectSortKeyAtX(columns, 14)
	if !ok || key != subjectSortCPU {
		t.Fatalf("sort key = %q ok=%v, want cpu true", key, ok)
	}
}

func TestDataTableViewWithScrollbar(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "NAME", Width: 8}}, 4, true)
	tbl.SetRows([]table.Row{
		{"row1"},
		{"row2"},
		{"row3"},
		{"row4"},
		{"row5"},
		{"row6"},
	})
	tbl.SetCursor(4)
	rendered := tbl.View()
	if !strings.Contains(rendered, "█") {
		t.Fatalf("scrollbar thumb missing: %q", rendered)
	}
	if !strings.Contains(rendered, "░") {
		t.Fatalf("scrollbar track missing: %q", rendered)
	}
}

func TestDataTableCanKeepNoSelectionAcrossRowsUpdate(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "NAME", Width: 8}}, 4, true)
	tbl.SetRows([]table.Row{{"row1"}, {"row2"}})
	tbl.SetCursor(-1)
	tbl.SetRows([]table.Row{{"row1"}, {"row2"}, {"row3"}})
	if tbl.Cursor() != -1 {
		t.Fatalf("cursor = %d, want -1", tbl.Cursor())
	}
}

func TestDataTableMoveDownFromNoSelectionSelectsFirstVisibleRow(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "NAME", Width: 8}}, 4, true)
	tbl.SetRows([]table.Row{{"row1"}, {"row2"}, {"row3"}, {"row4"}, {"row5"}})
	tbl.SetCursor(-1)
	tbl.ScrollDown(1)
	tbl.MoveDown(1)
	if tbl.Cursor() != 1 {
		t.Fatalf("cursor = %d, want 1", tbl.Cursor())
	}
	if tbl.offset != 1 {
		t.Fatalf("offset = %d, want 1", tbl.offset)
	}
}

func TestDataTableScrollCanMoveSelectedRowOffscreen(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "NAME", Width: 8}}, 4, true)
	tbl.SetRows([]table.Row{{"row1"}, {"row2"}, {"row3"}, {"row4"}, {"row5"}, {"row6"}})
	tbl.SetCursor(0)
	tbl.ScrollDown(2)
	if tbl.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0", tbl.Cursor())
	}
	if tbl.offset != 2 {
		t.Fatalf("offset = %d, want 2", tbl.offset)
	}
	if strings.Contains(tbl.View(), ">") {
		t.Fatalf("selected offscreen row should not render as visible: %q", tbl.View())
	}
}

func TestDataTablePageDownScrollsWithoutMovingSelection(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "NAME", Width: 8}}, 4, true)
	tbl.SetRows([]table.Row{{"row1"}, {"row2"}, {"row3"}, {"row4"}, {"row5"}, {"row6"}})
	tbl.SetCursor(0)
	tbl, _ = tbl.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if tbl.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0", tbl.Cursor())
	}
	if tbl.offset == 0 {
		t.Fatal("expected page down to scroll viewport")
	}
}

func TestDataTableCanRestoreCursorWithoutChangingViewport(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "NAME", Width: 8}}, 4, true)
	tbl.SetRows([]table.Row{{"row1"}, {"row2"}, {"row3"}, {"row4"}, {"row5"}, {"row6"}})
	tbl.SetCursor(0)
	tbl.ScrollDown(2)
	tbl.SetCursorPreserveOffset(0)
	if tbl.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0", tbl.Cursor())
	}
	if tbl.offset != 2 {
		t.Fatalf("offset = %d, want 2", tbl.offset)
	}
}

func TestDataTableVirtualRowsOnlyRenderVisibleRows(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "NAME", Width: 10}}, 6, true)
	calls := 0
	tbl.SetVirtualRows(20000, func(index int) table.Row {
		calls++
		return table.Row{fmt.Sprintf("row%d", index)}
	})

	_ = tbl.View()

	if calls != tbl.visibleRows() {
		t.Fatalf("rowAt calls = %d, want visible rows %d", calls, tbl.visibleRows())
	}
}

func TestDataTableVirtualRowsUseOffsetIndexes(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "NAME", Width: 10}}, 4, true)
	var indexes []int
	tbl.SetVirtualRows(20000, func(index int) table.Row {
		indexes = append(indexes, index)
		return table.Row{fmt.Sprintf("row%d", index)}
	})
	tbl.ScrollDown(42)

	_ = tbl.View()

	want := []int{42, 43, 44}
	if !reflect.DeepEqual(indexes, want) {
		t.Fatalf("rowAt indexes = %+v, want %+v", indexes, want)
	}
}

func TestDataTableVirtualRowsCanBeCleared(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "NAME", Width: 8}}, 4, true)
	tbl.SetVirtualRows(20000, func(index int) table.Row {
		return table.Row{fmt.Sprintf("row%d", index)}
	})
	tbl.SetCursor(10)
	tbl.ScrollDown(20)

	tbl.SetVirtualRows(0, nil)

	if tbl.Cursor() != -1 {
		t.Fatalf("cursor = %d, want -1", tbl.Cursor())
	}
	if tbl.offset != 0 {
		t.Fatalf("offset = %d, want 0", tbl.offset)
	}
}

func TestDataTableVirtualRowsScrollToBottomUsesVirtualCount(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "NAME", Width: 12}}, 4, true)
	tbl.SetVirtualRows(20000, func(index int) table.Row {
		return table.Row{fmt.Sprintf("row%d", index)}
	})

	tbl.ScrollToBottom()

	if tbl.offset != 19997 {
		t.Fatalf("offset = %d, want 19997", tbl.offset)
	}
	if !strings.Contains(tbl.View(), "row19997") {
		t.Fatalf("bottom row missing from view: %q", tbl.View())
	}
}

func TestDataTableHeaderUsesContentPadding(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "CPU ▼", Width: 7}, {Title: "LIMIT", Width: 9}}, 3, true)
	header := strings.Split(tbl.View(), "\n")[0]
	stripped := ansi.Strip(header)
	if !strings.Contains(stripped, " CPU ▼") || !strings.Contains(stripped, " LIMIT") {
		t.Fatalf("header padding missing: %q", header)
	}
}

func TestDataTableRowsUseSameColumnWidthsAsHeader(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "CPU ▼", Width: 7}, {Title: "LIMIT", Width: 9}}, 3, false)
	tbl.SetRows([]table.Row{{"12.5", "--"}})
	lines := strings.Split(tbl.View(), "\n")
	headerLimit := displayIndex(lines[0], "LIMIT")
	rowLimit := displayIndex(lines[1], "--")
	if rowLimit != headerLimit {
		t.Fatalf("row/header column mismatch: header LIMIT at %d, row LIMIT at %d\nheader=%q\nrow=%q", headerLimit, rowLimit, lines[0], lines[1])
	}
}

func TestActiveSortHeaderKeepsRightPaddingHighlighted(t *testing.T) {
	cell := newDataTable([]table.Column{{Title: "CPU ▼", Width: 7}}, 2, false).renderHeaderCells()
	stripped := ansi.Strip(cell)
	if stripped != " CPU ▼ " {
		t.Fatalf("active sort header padding = %q, want %q", stripped, " CPU ▼ ")
	}
}

func TestDataTableHeaderAndRowsShareGutter(t *testing.T) {
	tbl := newDataTable([]table.Column{{Title: "CPU ▼", Width: 7}}, 3, false)
	tbl.SetRows([]table.Row{{"12.5"}})
	lines := strings.Split(tbl.View(), "\n")
	headerCPU := displayIndex(lines[0], "CPU")
	rowCPU := displayIndex(lines[1], "12.5")
	if headerCPU != rowCPU {
		t.Fatalf("header/data gutter mismatch: header CPU at %d, row CPU at %d\nheader=%q\nrow=%q", headerCPU, rowCPU, lines[0], lines[1])
	}
}

func TestSubjectHeaderClickTogglesCurrentSort(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 90
	m.height = 24
	m.resizeTables()
	m.subjects = []model.Subject{
		{ID: "low", State: model.StateObserving, LastCPUPct: 1},
		{ID: "high", State: model.StateObserving, LastCPUPct: 90},
	}
	m.subjectTable.SetRows(subjectRows(m.subjects))

	next, _ := m.handleSubjectHeaderClick(tea.MouseEvent{X: 15})
	updated := next.(Model)
	if updated.subjects[0].ID != "low" || updated.subjectSortKey != subjectSortCPU || updated.subjectSortDir != sortAsc {
		t.Fatalf("unexpected subject sort: key=%q subjects=%+v", updated.subjectSortKey, updated.subjects)
	}
}

func TestMouseClickSelectedSubjectClearsSelectionAndRefreshesAllEvents(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 90
	m.height = 24
	m.resizeTables()
	m.subjects = []model.Subject{{ID: "s1", TargetRef: "proc#1", State: model.StateObserving}}
	m.events = []model.Event{{ID: 1, SubjectID: "s1", Type: "throttle"}}
	m.subjectTable.SetRows(subjectRows(m.subjects))
	m.subjectTable.SetCursor(0)
	m.eventTable.SetRows(eventRows(m.events))

	next, cmd := m.handleMouse(tea.MouseMsg(tea.MouseEvent{
		X:      2,
		Y:      m.subjectTop + 3,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}))
	if cmd == nil {
		t.Fatal("expected refresh command")
	}
	updated := next.(Model)
	if updated.subjectTable.Cursor() != -1 {
		t.Fatalf("subject cursor = %d, want -1", updated.subjectTable.Cursor())
	}
	if !updated.subjectDeselected {
		t.Fatal("subjectDeselected = false, want true")
	}
	if len(updated.events) != 0 {
		t.Fatalf("events len = %d, want transient clear before refresh", len(updated.events))
	}
}

func TestMouseClickSelectsSubjectRowAfterScroll(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 90
	m.height = 24
	m.resizeTables()
	m.subjects = []model.Subject{
		{ID: "s1", TargetRef: "proc#1"},
		{ID: "s2", TargetRef: "proc#2"},
		{ID: "s3", TargetRef: "proc#3"},
		{ID: "s4", TargetRef: "proc#4"},
		{ID: "s5", TargetRef: "proc#5"},
		{ID: "s6", TargetRef: "proc#6"},
		{ID: "s7", TargetRef: "proc#7"},
		{ID: "s8", TargetRef: "proc#8"},
		{ID: "s9", TargetRef: "proc#9"},
	}
	m.subjectTable.SetRows(subjectRows(m.subjects))
	m.subjectTable.ScrollDown(2)

	next, _ := m.handleMouse(tea.MouseMsg(tea.MouseEvent{
		X:      2,
		Y:      m.subjectTop + 3,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}))
	updated := next.(Model)
	if updated.subjectTable.Cursor() != 2 {
		t.Fatalf("subject cursor = %d, want 2", updated.subjectTable.Cursor())
	}
}

func TestMouseClickSelectedEventClearsEventSelection(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 90
	m.height = 24
	m.resizeTables()
	m.events = []model.Event{{ID: 1, Type: "first"}}
	m.eventTable.SetRows(eventRows(m.events))
	m.eventTable.SetCursor(0)

	next, cmd := m.handleMouse(tea.MouseMsg(tea.MouseEvent{
		X:      2,
		Y:      m.eventTop + 3,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
	}))
	if cmd != nil {
		t.Fatal("expected no command")
	}
	updated := next.(Model)
	if updated.eventTable.Cursor() != -1 {
		t.Fatalf("event cursor = %d, want -1", updated.eventTable.Cursor())
	}
}

func TestNewModelStartsWithNoSubjectSelection(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	if !m.subjectDeselected {
		t.Fatal("subjectDeselected = false, want true")
	}

	next, cmd := m.Update(dataMsg{
		subjects: []model.Subject{{ID: "s1", TargetRef: "proc#1", LastCPUPct: 90}},
		at:       time.Now(),
	})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	updated := next.(Model)
	if updated.subjectTable.Cursor() != -1 {
		t.Fatalf("subject cursor = %d, want -1", updated.subjectTable.Cursor())
	}
}

func TestMouseWheelScrollsSubjectsWithoutSelecting(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 90
	m.height = 24
	m.resizeTables()
	m.subjects = []model.Subject{
		{ID: "s1", TargetRef: "proc#1"},
		{ID: "s2", TargetRef: "proc#2"},
		{ID: "s3", TargetRef: "proc#3"},
		{ID: "s4", TargetRef: "proc#4"},
		{ID: "s5", TargetRef: "proc#5"},
		{ID: "s6", TargetRef: "proc#6"},
		{ID: "s7", TargetRef: "proc#7"},
		{ID: "s8", TargetRef: "proc#8"},
		{ID: "s9", TargetRef: "proc#9"},
	}
	m.subjectTable.SetRows(subjectRows(m.subjects))
	m.subjectTable.SetCursor(-1)

	next, cmd := m.handleMouse(tea.MouseMsg(tea.MouseEvent{
		X:      2,
		Y:      m.subjectTop + 4,
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	}))
	if cmd != nil {
		t.Fatal("expected no command")
	}
	updated := next.(Model)
	if updated.subjectTable.Cursor() != -1 {
		t.Fatalf("subject cursor = %d, want -1", updated.subjectTable.Cursor())
	}
	if updated.subjectTable.offset == 0 {
		t.Fatal("expected subject table offset to scroll")
	}
}

func TestDataRefreshKeepsSubjectDeselected(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.subjectDeselected = true
	m.subjectTable.SetCursor(-1)

	next, cmd := m.Update(dataMsg{
		subjects: []model.Subject{{ID: "s1", TargetRef: "proc#1", LastCPUPct: 90}},
		events:   []model.Event{{ID: 1, SubjectID: "s1", Type: "throttle"}},
		at:       time.Now(),
	})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	updated := next.(Model)
	if updated.subjectTable.Cursor() != -1 {
		t.Fatalf("subject cursor = %d, want -1", updated.subjectTable.Cursor())
	}
	if len(updated.events) != 1 {
		t.Fatalf("events len = %d, want all events while subject is deselected", len(updated.events))
	}
}

func TestDataRefreshDiscardsStaleSubjectEventsWhenDeselected(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.subjectDeselected = true
	m.subjectTable.SetCursor(-1)

	next, cmd := m.Update(dataMsg{
		subjects:  []model.Subject{{ID: "s1", TargetRef: "proc#1", LastCPUPct: 90}},
		events:    []model.Event{{ID: 1, SubjectID: "s1", Type: "throttle"}},
		subjectID: "s1",
		at:        time.Now(),
	})
	if cmd == nil {
		t.Fatal("expected refresh command for all events")
	}
	updated := next.(Model)
	if len(updated.events) != 0 {
		t.Fatalf("events len = %d, want stale subject events discarded", len(updated.events))
	}
}

func TestDataRefreshUsesSubjectPageTotal(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 90
	m.height = 24
	m.resizeTables()

	next, cmd := m.Update(dataMsg{
		subjectPage: model.SubjectPage{
			Total:         20000,
			Offset:        100,
			Limit:         100,
			Items:         []model.Subject{{ID: "s100", TargetRef: "proc#100", LastCPUPct: 90}},
			SelectedIndex: -1,
		},
		summary: model.SubjectSummary{Total: 20000, MaxCPUPct: 90, AvgCPUPct: 10},
		at:      time.Now(),
	})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	updated := next.(Model)
	if updated.subjectTable.rowCount != 20000 {
		t.Fatalf("row count = %d, want 20000", updated.subjectTable.rowCount)
	}
	if updated.subjectPageOffset != 100 || len(updated.subjects) != 1 {
		t.Fatalf("unexpected page state: offset=%d len=%d", updated.subjectPageOffset, len(updated.subjects))
	}
}

func TestSubjectPageRefreshDoesNotRecordHistory(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.system = model.SystemMetrics{CPUPercent: 42}
	m.summary = model.SubjectSummary{Total: 100, Throttled: 2}
	m.recordHistory()

	next, cmd := m.Update(dataMsg{
		subjectPage: model.SubjectPage{
			Total:         20000,
			Offset:        100,
			Limit:         100,
			Items:         []model.Subject{{ID: "s100", TargetRef: "proc#100", LastCPUPct: 90}},
			SelectedIndex: -1,
		},
		updateSubjects: true,
		at:             time.Now(),
	})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	updated := next.(Model)
	if len(updated.systemCPUHistory) != 1 || len(updated.throttledHistory) != 1 {
		t.Fatalf("history changed on page refresh: cpu=%v throttled=%v", updated.systemCPUHistory, updated.throttledHistory)
	}
	if updated.system.CPUPercent != 42 || updated.summary.Total != 100 {
		t.Fatalf("status changed on page refresh: system=%+v summary=%+v", updated.system, updated.summary)
	}
}

func TestFullRefreshRecordsHistory(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))

	next, cmd := m.Update(dataMsg{
		subjectPage: model.SubjectPage{Total: 1, Limit: 100, Items: []model.Subject{{ID: "s1", TargetRef: "proc#1"}}},
		system:      model.SystemMetrics{CPUPercent: 55, SampledAt: time.Now()},
		summary:     model.SubjectSummary{Total: 1, Throttled: 1},
		at:          time.Now(),

		updateSubjects: true,
		updateStatus:   true,
		recordHistory:  true,
	})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	updated := next.(Model)
	if latestFloat(updated.systemCPUHistory) != 55 || latestInt(updated.throttledHistory) != 1 {
		t.Fatalf("history not recorded: cpu=%v throttled=%v", updated.systemCPUHistory, updated.throttledHistory)
	}
}

func TestDataRefreshDoesNotPullSelectedSubjectBackIntoView(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 90
	m.height = 24
	m.resizeTables()
	m.subjectDeselected = false
	m.subjects = []model.Subject{
		{ID: "s1", TargetRef: "proc#1"},
		{ID: "s2", TargetRef: "proc#2"},
		{ID: "s3", TargetRef: "proc#3"},
		{ID: "s4", TargetRef: "proc#4"},
		{ID: "s5", TargetRef: "proc#5"},
		{ID: "s6", TargetRef: "proc#6"},
		{ID: "s7", TargetRef: "proc#7"},
		{ID: "s8", TargetRef: "proc#8"},
		{ID: "s9", TargetRef: "proc#9"},
	}
	m.subjectTable.SetRows(subjectRows(m.subjects))
	m.subjectTable.SetCursor(0)
	m.subjectTable.ScrollDown(2)

	next, cmd := m.Update(dataMsg{subjects: m.subjects, subjectID: "s1", at: time.Now()})
	if cmd != nil {
		t.Fatal("expected no command")
	}
	updated := next.(Model)
	if got := updated.subjects[updated.subjectTable.Cursor()].ID; got != "s1" {
		t.Fatalf("selected subject = %q, want s1", got)
	}
	if updated.subjectTable.offset != 2 {
		t.Fatalf("subject offset = %d, want 2", updated.subjectTable.offset)
	}
}

func displayIndex(s string, substr string) int {
	stripped := ansi.Strip(s)
	idx := strings.Index(stripped, substr)
	if idx < 0 {
		return -1
	}
	return ansi.StringWidth(stripped[:idx])
}

func TestHeaderClickChangesRefreshInterval(t *testing.T) {
	m := New(api.NewClient("/tmp/missing.sock"))
	m.width = 100
	_, _, plusStart, _ := m.refreshControlBounds()

	next, _ := m.handleHeaderClick(tea.MouseEvent{X: plusStart})
	updated := next.(Model)
	if updated.refreshInterval != 2*time.Second {
		t.Fatalf("refresh interval = %s, want 2s", updated.refreshInterval)
	}
}
