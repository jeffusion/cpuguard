package store

import (
	"path/filepath"
	"testing"
	"time"

	"cpuguard/internal/model"
)

func TestPruneEventsByMaxEvents(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	for i := 0; i < 5; i++ {
		if err := db.AddEvent(model.Event{SubjectID: "s", RuleID: "r", Type: "test", CreatedAt: time.Now().Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.PruneEvents(2, 0); err != nil {
		t.Fatal(err)
	}
	events, err := db.ListEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
}

func TestPruneEventsByMaxAge(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.AddEvent(model.Event{SubjectID: "old", RuleID: "r", Type: "test", CreatedAt: time.Now().Add(-48 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddEvent(model.Event{SubjectID: "new", RuleID: "r", Type: "test", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.PruneEvents(0, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	events, err := db.ListEvents(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].SubjectID != "new" {
		t.Fatalf("unexpected events after prune: %+v", events)
	}
}

func TestListEventsForSubject(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.AddEvent(model.Event{SubjectID: "s1", RuleID: "r", Type: "one", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddEvent(model.Event{SubjectID: "s2", RuleID: "r", Type: "two", CreatedAt: time.Now().Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddEvent(model.Event{SubjectID: "s1", RuleID: "r", Type: "three", CreatedAt: time.Now().Add(2 * time.Second)}); err != nil {
		t.Fatal(err)
	}

	events, err := db.ListEventsForSubject("s1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	for _, event := range events {
		if event.SubjectID != "s1" {
			t.Fatalf("unexpected subject event: %+v", event)
		}
	}
}

func TestQueryEventsFiltersBySubjectTypeAndSince(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Now().Add(-time.Hour)
	events := []model.Event{
		{SubjectID: "s1", RuleID: "r", Type: "error", CreatedAt: base},
		{SubjectID: "s1", RuleID: "r", Type: "throttled", CreatedAt: base.Add(10 * time.Minute)},
		{SubjectID: "s2", RuleID: "r", Type: "throttled", CreatedAt: base.Add(20 * time.Minute)},
		{SubjectID: "s1", RuleID: "r", Type: "throttled", CreatedAt: base.Add(30 * time.Minute)},
	}
	for _, event := range events {
		if err := db.AddEvent(event); err != nil {
			t.Fatal(err)
		}
	}

	got, err := db.QueryEvents(model.EventQuery{
		SubjectID: "s1",
		Type:      "throttled",
		Since:     base.Add(15 * time.Minute),
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("events len = %d, want 1: %+v", len(got), got)
	}
	if got[0].SubjectID != "s1" || got[0].Type != "throttled" {
		t.Fatalf("unexpected event: %+v", got[0])
	}
}

func TestQueryEventsSinceIncludesLaterNanosecondsInSameSecond(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	base := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
	if err := db.AddEvent(model.Event{SubjectID: "s1", RuleID: "r", Type: "test", CreatedAt: base.Add(100 * time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	events, err := db.QueryEvents(model.EventQuery{Since: base, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
}

func TestDeleteStaleObservingSubjects(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	subjects := []model.Subject{
		{ID: "old-observing", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "old", DisplayName: "old", State: model.StateObserving, UpdatedAt: now.Add(-time.Hour)},
		{ID: "new-observing", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "new", DisplayName: "new", State: model.StateObserving, UpdatedAt: now},
		{ID: "old-throttled", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "throttled", DisplayName: "throttled", State: model.StateThrottled, UpdatedAt: now.Add(-time.Hour)},
	}
	if err := db.UpsertSubjects(subjects); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteStaleObservingSubjects(now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}

	remaining, err := db.ListSubjects()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, subject := range remaining {
		got[subject.ID] = true
	}
	if got["old-observing"] {
		t.Fatal("old observing subject was not deleted")
	}
	if !got["new-observing"] || !got["old-throttled"] {
		t.Fatalf("unexpected remaining subjects: %+v", got)
	}
}

func TestQuerySubjectsPaginatesAndSortsByCPU(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	subjects := []model.Subject{
		{ID: "low", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "low", DisplayName: "low", State: model.StateObserving, LastCPUPct: 10, UpdatedAt: now},
		{ID: "mid", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "mid", DisplayName: "mid", State: model.StateObserving, LastCPUPct: 50, UpdatedAt: now},
		{ID: "high", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "high", DisplayName: "high", State: model.StateObserving, LastCPUPct: 90, UpdatedAt: now},
	}
	if err := db.UpsertSubjects(subjects); err != nil {
		t.Fatal(err)
	}

	page, err := db.QuerySubjects(model.SubjectQuery{Offset: 1, Limit: 1, Sort: "cpu", Dir: "desc"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Items) != 1 || page.Items[0].ID != "mid" {
		t.Fatalf("unexpected page: %+v", page)
	}
}

func TestQuerySubjectsReturnsSelectedIndex(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	subjects := []model.Subject{
		{ID: "a", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "a", DisplayName: "a", State: model.StateObserving, LastCPUPct: 10, UpdatedAt: now},
		{ID: "b", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "b", DisplayName: "b", State: model.StateObserving, LastCPUPct: 20, UpdatedAt: now},
		{ID: "c", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "c", DisplayName: "c", State: model.StateObserving, LastCPUPct: 30, UpdatedAt: now},
	}
	if err := db.UpsertSubjects(subjects); err != nil {
		t.Fatal(err)
	}

	page, err := db.QuerySubjects(model.SubjectQuery{Offset: 0, Limit: 1, Sort: "cpu", Dir: "desc", SelectedID: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if page.SelectedIndex != 1 || page.Selected == nil || page.Selected.ID != "b" {
		t.Fatalf("unexpected selected subject: %+v", page)
	}
}

func TestSubjectSummary(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	subjects := []model.Subject{
		{ID: "observing", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "observing", DisplayName: "observing", State: model.StateObserving, LastCPUPct: 10, UpdatedAt: now},
		{ID: "throttled", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "throttled", DisplayName: "throttled", State: model.StateThrottled, LastCPUPct: 40, UpdatedAt: now},
		{ID: "held", RuleID: "r", Scope: model.ScopeHostProcess, TargetRef: "held", DisplayName: "held", State: model.StateManualHold, LastCPUPct: 70, UpdatedAt: now},
	}
	if err := db.UpsertSubjects(subjects); err != nil {
		t.Fatal(err)
	}

	summary, err := db.SubjectSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 3 || summary.Throttled != 1 || summary.Held != 1 || summary.MaxCPUPct != 70 || summary.AvgCPUPct != 40 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}
