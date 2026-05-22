package tui

import (
	"testing"
	"time"

	"cpuguard/internal/model"
	"github.com/charmbracelet/bubbles/table"
)

func TestSortSubjectsDefaultsToCPUDescending(t *testing.T) {
	subjects := []model.Subject{
		{ID: "observing-high", State: model.StateObserving, LastCPUPct: 200},
		{ID: "throttled-low", State: model.StateThrottled, LastCPUPct: 20},
		{ID: "hold", State: model.StateManualHold, LastCPUPct: 50},
	}
	sorted := sortSubjects(subjects)
	if sorted[0].ID != "observing-high" {
		t.Fatalf("first subject = %s, want observing-high", sorted[0].ID)
	}
	if sorted[1].ID != "hold" {
		t.Fatalf("second subject = %s, want hold", sorted[1].ID)
	}
}

func TestEventRowsFormatsPercentFields(t *testing.T) {
	events := []model.Event{{
		SubjectID: "host/rule/12345",
		Type:      "throttled",
		Message:   "host process tree throttled",
		CPUPct:    22.5,
		LimitPct:  11.3,
		CreatedAt: time.Date(2026, 5, 21, 1, 2, 3, 0, time.Local),
	}}
	rows := eventRows(events)
	if len(rows) != 1 {
		t.Fatalf("rows len = %d, want 1", len(rows))
	}
	if rows[0][1] != "throttled" || rows[0][3] != "22.5" || rows[0][4] != "11.3" {
		t.Fatalf("unexpected row: %#v", rows[0])
	}
}

func TestFormatPercent(t *testing.T) {
	if got := formatPercent(25); got != "25.0" {
		t.Fatalf("formatPercent(25) = %q, want 25.0", got)
	}
	if got := formatPercent(0); got != "0.0" {
		t.Fatalf("formatPercent(0) = %q, want 0.0", got)
	}
}

func TestSubjectRowsShowsBlankLimitForUnthrottledSubjects(t *testing.T) {
	rows := subjectRows([]model.Subject{{
		ID:              "s1",
		State:           model.StateObserving,
		LastCPUPct:      25,
		CurrentLimitPct: 12.5,
	}})
	if rows[0][2] != "--" {
		t.Fatalf("limit = %q, want --", rows[0][2])
	}
}

func TestSubjectRowsShowsLimitForThrottledSubjects(t *testing.T) {
	rows := subjectRows([]model.Subject{{
		ID:              "s1",
		State:           model.StateThrottled,
		LastCPUPct:      25,
		CurrentLimitPct: 12.5,
	}})
	if rows[0][2] != "12.5" {
		t.Fatalf("limit = %q, want 12.5", rows[0][2])
	}
}

func TestSortSubjectsByCPUDescending(t *testing.T) {
	subjects := []model.Subject{
		{ID: "low", LastCPUPct: 1},
		{ID: "high", LastCPUPct: 90},
	}
	sorted := sortSubjectsBy(subjects, subjectSortCPU, sortDesc)
	if sorted[0].ID != "high" {
		t.Fatalf("first subject = %s, want high", sorted[0].ID)
	}
}

func TestSortEventsByTypeAscending(t *testing.T) {
	events := []model.Event{
		{ID: 1, Type: "released"},
		{ID: 2, Type: "error"},
	}
	sorted := sortEventsBy(events, eventSortType, sortAsc)
	if sorted[0].Type != "error" {
		t.Fatalf("first event type = %s, want error", sorted[0].Type)
	}
}

func TestSubjectColumnsShowSortIndicator(t *testing.T) {
	columns := subjectColumnsForSort(100, subjectSortCPU, sortDesc)
	if columns[1].Title != "CPU ▼" {
		t.Fatalf("cpu title = %q, want CPU ▼", columns[1].Title)
	}
}

func TestSubjectColumnsFillTableWidth(t *testing.T) {
	columns := subjectColumns(100)
	if got, want := totalColumnWidth(columns), tableContentWidth(100); got != want {
		t.Fatalf("subject column width = %d, want %d", got, want)
	}
}

func TestSubjectColumnsGiveRuleUsefulWidth(t *testing.T) {
	columns := subjectColumns(100)
	targetWidth := columns[4].Width
	ruleWidth := columns[5].Width
	if ruleWidth < 20 {
		t.Fatalf("rule width = %d, want >= 20", ruleWidth)
	}
	if targetWidth > 56 {
		t.Fatalf("target width = %d, want <= 56", targetWidth)
	}
}

func TestEventColumnsShowSortIndicator(t *testing.T) {
	columns := eventColumnsForSort(100, eventSortTime, sortDesc)
	if columns[0].Title != "TIME ▼" {
		t.Fatalf("time title = %q, want TIME ▼", columns[0].Title)
	}
}

func TestEventColumnsFillTableWidth(t *testing.T) {
	columns := eventColumns(100)
	if got, want := totalColumnWidth(columns), tableContentWidth(100); got != want {
		t.Fatalf("event column width = %d, want %d", got, want)
	}
}

func totalColumnWidth(columns []table.Column) int {
	total := 0
	for _, column := range columns {
		total += column.Width
	}
	return total
}
