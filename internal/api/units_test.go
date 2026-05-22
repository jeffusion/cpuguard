package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"cpuguard/internal/model"
)

func TestConvertSubjectsToTotalCPUPercent(t *testing.T) {
	subjects := []model.Subject{{
		ID:              "s1",
		LastCPUPct:      200,
		CurrentLimitPct: 100,
	}}

	converted := convertSubjectsToTotalCPUPercent(subjects, 8)
	if converted[0].LastCPUPct != 25 {
		t.Fatalf("last cpu = %.1f, want 25.0", converted[0].LastCPUPct)
	}
	if converted[0].CurrentLimitPct != 12.5 {
		t.Fatalf("limit = %.1f, want 12.5", converted[0].CurrentLimitPct)
	}
	if subjects[0].LastCPUPct != 200 {
		t.Fatalf("input subject was mutated: %.1f", subjects[0].LastCPUPct)
	}
}

func TestConvertSubjectPageToTotalCPUPercent(t *testing.T) {
	selected := model.Subject{ID: "s1", LastCPUPct: 400, CurrentLimitPct: 200}
	page := model.SubjectPage{
		Total:    1,
		Items:    []model.Subject{selected},
		Selected: &selected,
	}

	converted := convertSubjectPageToTotalCPUPercent(page, 8)
	if converted.Items[0].LastCPUPct != 50 || converted.Items[0].CurrentLimitPct != 25 {
		t.Fatalf("converted item = %+v", converted.Items[0])
	}
	if converted.Selected == nil || converted.Selected.LastCPUPct != 50 || converted.Selected.CurrentLimitPct != 25 {
		t.Fatalf("converted selected = %+v", converted.Selected)
	}
}

func TestConvertSubjectSummaryToTotalCPUPercent(t *testing.T) {
	converted := convertSubjectSummaryToTotalCPUPercent(model.SubjectSummary{MaxCPUPct: 720, AvgCPUPct: 360}, 8)
	if converted.MaxCPUPct != 90 || converted.AvgCPUPct != 45 {
		t.Fatalf("converted summary = %+v", converted)
	}
}

func TestConvertEventsToTotalCPUPercent(t *testing.T) {
	events := []model.Event{{
		CPUPct:   720,
		LimitPct: 360,
	}}

	converted := convertEventsToTotalCPUPercent(events, 8)
	if converted[0].CPUPct != 90 {
		t.Fatalf("event cpu = %.1f, want 90.0", converted[0].CPUPct)
	}
	if converted[0].LimitPct != 45 {
		t.Fatalf("event limit = %.1f, want 45.0", converted[0].LimitPct)
	}
}

func TestParseEventQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/events?subject_id=s1&type=error&limit=25&since=2026-05-21T10:00:00Z", nil)
	query, err := parseEventQuery(req)
	if err != nil {
		t.Fatal(err)
	}
	if query.SubjectID != "s1" || query.Type != "error" || query.Limit != 25 {
		t.Fatalf("unexpected query: %+v", query)
	}
	if !query.Since.Equal(time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("since = %s", query.Since)
	}
}

func TestParseSubjectQuery(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/subjects?offset=20&limit=50&sort=target&dir=asc&selected_id=s1", nil)
	query := parseSubjectQuery(req)
	if query.Offset != 20 || query.Limit != 50 || query.Sort != "target" || query.Dir != "asc" || query.SelectedID != "s1" {
		t.Fatalf("unexpected query: %+v", query)
	}
}

func TestParseSubjectQueryDefaultsUnsafeValues(t *testing.T) {
	req := httptest.NewRequest("GET", "/v1/subjects?offset=-1&limit=99999&sort=bad&dir=sideways", nil)
	query := parseSubjectQuery(req)
	if query.Offset != 0 || query.Limit != 1000 || query.Sort != "cpu" || query.Dir != "desc" {
		t.Fatalf("unexpected query: %+v", query)
	}
}
