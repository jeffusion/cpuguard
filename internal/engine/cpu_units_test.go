package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cpuguard/internal/config"
	"cpuguard/internal/model"
	"cpuguard/internal/store"
	"cpuguard/internal/system"
)

func TestTotalCPUPercentToCorePercent(t *testing.T) {
	e := &Engine{cpuCount: 8}

	if got := e.totalCPUPercentToCorePercent(90); got != 720 {
		t.Fatalf("90%% total CPU converted to %.1f core-percent, want 720.0", got)
	}
	if got := e.totalCPUPercentToCorePercent(70); got != 560 {
		t.Fatalf("70%% total CPU converted to %.1f core-percent, want 560.0", got)
	}
	if got := e.totalCPUPercentToCorePercent(20); got != 160 {
		t.Fatalf("20%% total CPU converted to %.1f core-percent, want 160.0", got)
	}
}

func TestTotalCPUThresholdDoesNotTreatCorePercentAsTotalPercent(t *testing.T) {
	e := &Engine{cpuCount: 8}
	now := time.Now()
	window := 5 * time.Second
	sampleInterval := time.Second

	var moderate Window
	for i := 0; i < 5; i++ {
		moderate.Add(Sample{At: now.Add(time.Duration(i-4) * sampleInterval), CPUPct: 200}, window)
	}
	if moderate.MeetsAbove(e.totalCPUPercentToCorePercent(90), window, 1.0, sampleInterval, now) {
		t.Fatal("200 core-percent on an 8-core host should not meet a 90% total CPU threshold")
	}

	var saturated Window
	for i := 0; i < 5; i++ {
		saturated.Add(Sample{At: now.Add(time.Duration(i-4) * sampleInterval), CPUPct: 800}, window)
	}
	if !saturated.MeetsAbove(e.totalCPUPercentToCorePercent(90), window, 1.0, sampleInterval, now) {
		t.Fatal("800 core-percent on an 8-core host should meet a 90% total CPU threshold")
	}
}

func TestReloadInvalidConfigKeepsExistingConfig(t *testing.T) {
	e := &Engine{cfg: &config.Config{}}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("rules: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := e.Reload(context.Background(), path); err == nil {
		t.Fatal("expected reload to fail")
	}
	if e.cfg == nil {
		t.Fatal("existing config was cleared")
	}
}

func TestPruneStaleThrottledSubjectReleasesLimit(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	subject := model.Subject{
		ID:          "host/rule/1234",
		RuleID:      "rule",
		Scope:       model.ScopeHostProcess,
		TargetRef:   "cpuguard-burn#1234",
		DisplayName: "cpuguard-burn#1234",
		State:       model.StateThrottled,
		LastCPUPct:  95,
		UpdatedAt:   now.Add(-time.Minute),
	}
	if err := db.UpsertSubject(subject); err != nil {
		t.Fatal(err)
	}

	e := &Engine{
		cfg:      &config.Config{},
		store:    db,
		subjects: map[string]*trackedSubject{subject.ID: &trackedSubject{Subject: subject}},
	}
	e.cfg.Global.SampleInterval.Duration = time.Second
	e.pruneStaleObservingSubjects(now)

	limits, err := e.ListLimits()
	if err != nil {
		t.Fatal(err)
	}
	if len(limits) != 0 {
		t.Fatalf("limits len = %d, want 0: %+v", len(limits), limits)
	}
	events, err := db.QueryEvents(model.EventQuery{Type: "stale_release", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("stale_release events len = %d, want 1", len(events))
	}
}

func TestThrottledHostReleasesAfterRecoveryWindow(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now()
	sampleInterval := time.Second
	rule := model.Rule{
		ID:    "host-cpuguard-burn",
		Scope: model.ScopeHostProcess,
		Threshold: model.Threshold{
			CPUPercentTotalGT: 80,
			Window:            20 * time.Second,
			Ratio:             0.8,
		},
		Action: model.Action{
			Factor:      0.5,
			MinLimitPct: 20,
		},
		Recovery: model.Recovery{
			CPUPercentTotalLT: 30,
			Window:            30 * time.Second,
			Ratio:             0.8,
		},
		Cooldown: 45 * time.Second,
	}
	subject := model.Subject{
		ID:              "host/host-cpuguard-burn/5001",
		RuleID:          rule.ID,
		Scope:           model.ScopeHostProcess,
		TargetRef:       "cpuguard-burn#5001",
		DisplayName:     "cpuguard-burn#5001",
		State:           model.StateThrottled,
		LastCPUPct:      320,
		CurrentLimitPct: 160,
		UpdatedAt:       now,
	}
	tracked := &trackedSubject{Subject: subject}
	for i := 0; i < 30; i++ {
		tracked.Window.Add(Sample{At: now.Add(time.Duration(i-29) * sampleInterval), CPUPct: 40}, rule.Recovery.Window)
	}

	e := &Engine{
		cfg: &config.Config{Global: config.GlobalConfig{
			SampleInterval: config.Duration{Duration: sampleInterval},
		}},
		store:    db,
		cgroups:  &system.CgroupManager{},
		cpuCount: 4,
		subjects: map[string]*trackedSubject{subject.ID: tracked},
	}
	discovered := model.HostSubject{
		Rule:        rule,
		Root:        model.HostProcess{PID: 5001, Name: "cpuguard-burn"},
		Descendants: []int{5001},
	}

	e.evaluateHostLocked(now, discovered, tracked, 40)

	if tracked.Subject.State != model.StateObserving {
		t.Fatalf("state = %s, want %s", tracked.Subject.State, model.StateObserving)
	}
	if tracked.Subject.CurrentLimitPct != 0 {
		t.Fatalf("current limit = %.1f, want 0", tracked.Subject.CurrentLimitPct)
	}
	events, err := db.QueryEvents(model.EventQuery{Type: "released", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("released events len = %d, want 1", len(events))
	}
}
