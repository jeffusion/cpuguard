package engine

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"time"

	"cpuguard/internal/config"
	"cpuguard/internal/model"
	"cpuguard/internal/store"
	"cpuguard/internal/system"
)

type Engine struct {
	cfg           *config.Config
	store         *store.SQLiteStore
	docker        *system.DockerClient
	cgroups       *system.CgroupManager
	hostSampler   *system.HostCPUSampler
	dockerSampler *system.DockerCPUSampler
	systemSampler *system.SystemMetricsSampler
	cpuCount      int

	mu       sync.RWMutex
	subjects map[string]*trackedSubject
}

type trackedSubject struct {
	Subject model.Subject
	Window  Window
	Host    *model.HostSubject
	Docker  *model.DockerSubject
}

func New(cfg *config.Config, db *store.SQLiteStore) *Engine {
	cpuCount := runtime.NumCPU()
	engine := &Engine{
		cfg:           cfg,
		store:         db,
		docker:        system.NewDockerClient(),
		cgroups:       &system.CgroupManager{},
		hostSampler:   system.NewHostCPUSampler(cpuCount),
		dockerSampler: system.NewDockerCPUSampler(),
		systemSampler: system.NewSystemMetricsSampler(cpuCount),
		cpuCount:      cpuCount,
		subjects:      map[string]*trackedSubject{},
	}
	engine.restoreSubjects()
	return engine
}

func (e *Engine) restoreSubjects() {
	subjects, err := e.store.ListSubjects()
	if err != nil {
		log.Printf("restore subjects: %v", err)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, subject := range subjects {
		subj := subject
		e.subjects[subject.ID] = &trackedSubject{Subject: subj}
	}
}

func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(e.cfg.Global.SampleInterval.Duration)
	defer ticker.Stop()
	e.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.tick(ctx)
		}
	}
}

func (e *Engine) tick(ctx context.Context) {
	rules := e.cfg.RulesAsModel()
	now := time.Now()
	for _, rule := range rules {
		switch rule.Scope {
		case model.ScopeHostProcess:
			e.tickHostRule(now, rule)
		case model.ScopeDockerContainer:
			e.tickDockerRule(ctx, now, rule)
		}
	}
	e.pruneStaleObservingSubjects(now)
}

func (e *Engine) tickHostRule(now time.Time, rule model.Rule) {
	subjects, err := system.DiscoverHostSubjects(rule)
	if err != nil {
		log.Printf("discover host subjects for rule %s: %v", rule.ID, err)
		return
	}
	updates := make([]model.Subject, 0, len(subjects))
	for _, discovered := range subjects {
		cpuPct, err := e.hostSampler.Sample(discovered)
		if err != nil {
			log.Printf("sample host subject %s: %v", discovered.SubjectID(), err)
			continue
		}
		updates = append(updates, e.observeHost(now, discovered, cpuPct))
	}
	if err := e.store.UpsertSubjects(updates); err != nil {
		log.Printf("store host subjects for rule %s: %v", rule.ID, err)
	}
}

func (e *Engine) tickDockerRule(ctx context.Context, now time.Time, rule model.Rule) {
	subjects, err := system.DiscoverDockerSubjects(ctx, e.docker, rule)
	if err != nil {
		log.Printf("discover docker subjects for rule %s: %v", rule.ID, err)
		return
	}
	updates := make([]model.Subject, 0, len(subjects))
	for _, discovered := range subjects {
		cpuPct, err := e.dockerSampler.Sample(discovered)
		if err != nil {
			log.Printf("sample docker subject %s: %v", discovered.SubjectID(), err)
			continue
		}
		updates = append(updates, e.observeDocker(ctx, now, discovered, cpuPct))
	}
	if err := e.store.UpsertSubjects(updates); err != nil {
		log.Printf("store docker subjects for rule %s: %v", rule.ID, err)
	}
}

func (e *Engine) observeHost(now time.Time, discovered model.HostSubject, cpuPct float64) model.Subject {
	e.mu.Lock()
	defer e.mu.Unlock()
	tracked := e.ensureSubjectLocked(discovered.SubjectID(), func() model.Subject {
		return model.Subject{
			ID:          discovered.SubjectID(),
			RuleID:      discovered.Rule.ID,
			Scope:       model.ScopeHostProcess,
			TargetRef:   discovered.TargetReference(),
			DisplayName: discovered.TargetReference(),
			State:       model.StateObserving,
		}
	})
	tracked.Window.Add(Sample{At: now, CPUPct: cpuPct}, maxDuration(discovered.Rule.Threshold.Window, discovered.Rule.Recovery.Window))
	hostCopy := discovered
	tracked.Host = &hostCopy
	tracked.Subject.LastCPUPct = cpuPct
	tracked.Subject.UpdatedAt = now
	e.evaluateHostLocked(now, discovered, tracked, cpuPct)
	return tracked.Subject
}

func (e *Engine) observeDocker(ctx context.Context, now time.Time, discovered model.DockerSubject, cpuPct float64) model.Subject {
	e.mu.Lock()
	defer e.mu.Unlock()
	tracked := e.ensureSubjectLocked(discovered.SubjectID(), func() model.Subject {
		return model.Subject{
			ID:                discovered.SubjectID(),
			RuleID:            discovered.Rule.ID,
			Scope:             model.ScopeDockerContainer,
			TargetRef:         discovered.TargetReference(),
			DisplayName:       discovered.TargetReference(),
			State:             model.StateObserving,
			DockerContainerID: discovered.Container.ID,
		}
	})
	tracked.Subject.DockerContainerID = discovered.Container.ID
	if tracked.Subject.State == model.StateObserving && tracked.Subject.DockerOriginalCPUPeriod == 0 && tracked.Subject.DockerOriginalCPUQuota == 0 {
		tracked.Subject.DockerOriginalCPUPeriod = discovered.Container.CPUPeriod
		tracked.Subject.DockerOriginalCPUQuota = discovered.Container.CPUQuota
	}
	tracked.Window.Add(Sample{At: now, CPUPct: cpuPct}, maxDuration(discovered.Rule.Threshold.Window, discovered.Rule.Recovery.Window))
	dockerCopy := discovered
	tracked.Docker = &dockerCopy
	tracked.Subject.LastCPUPct = cpuPct
	tracked.Subject.UpdatedAt = now
	e.evaluateDockerLocked(ctx, now, discovered, tracked, cpuPct)
	return tracked.Subject
}

func (e *Engine) evaluateHostLocked(now time.Time, discovered model.HostSubject, tracked *trackedSubject, cpuPct float64) {
	rule := discovered.Rule
	switch tracked.Subject.State {
	case model.StateObserving:
		if now.Before(tracked.Subject.CooldownUntil) {
			return
		}
		if tracked.Window.MeetsAbove(e.totalCPUPercentToCorePercent(rule.Threshold.CPUPercentTotalGT), rule.Threshold.Window, rule.Threshold.Ratio, e.cfg.Global.SampleInterval.Duration, now) {
			limit := CalculateLimit(cpuPct, rule.Action.Factor, e.totalCPUPercentToCorePercent(rule.Action.MinLimitPct))
			next, err := e.cgroups.ThrottleHost(discovered, &tracked.Subject, limit)
			if err != nil {
				e.recordEventLocked(tracked.Subject, "error", fmt.Sprintf("host throttle failed: %v", err), cpuPct, limit)
				return
			}
			next.LastCPUPct = cpuPct
			next.CooldownUntil = now.Add(rule.Cooldown)
			next.UpdatedAt = now
			tracked.Subject = mergeSubject(tracked.Subject, next)
			e.recordEventLocked(tracked.Subject, "throttled", "host process tree throttled", cpuPct, limit)
		}
	case model.StateThrottled:
		if tracked.Window.MeetsBelow(e.totalCPUPercentToCorePercent(rule.Recovery.CPUPercentTotalLT), rule.Recovery.Window, rule.Recovery.Ratio, e.cfg.Global.SampleInterval.Duration, now) {
			if err := e.cgroups.ReleaseHost(tracked.Subject); err != nil {
				e.recordEventLocked(tracked.Subject, "error", fmt.Sprintf("host release failed: %v", err), cpuPct, 0)
				return
			}
			tracked.Subject.State = model.StateObserving
			tracked.Subject.CurrentLimitPct = 0
			tracked.Subject.CooldownUntil = now.Add(rule.Cooldown)
			tracked.Subject.UpdatedAt = now
			e.recordEventLocked(tracked.Subject, "released", "host process tree released", cpuPct, 0)
		}
	}
}

func (e *Engine) evaluateDockerLocked(ctx context.Context, now time.Time, discovered model.DockerSubject, tracked *trackedSubject, cpuPct float64) {
	rule := discovered.Rule
	switch tracked.Subject.State {
	case model.StateObserving:
		if now.Before(tracked.Subject.CooldownUntil) {
			return
		}
		if tracked.Window.MeetsAbove(e.totalCPUPercentToCorePercent(rule.Threshold.CPUPercentTotalGT), rule.Threshold.Window, rule.Threshold.Ratio, e.cfg.Global.SampleInterval.Duration, now) {
			limit := CalculateLimit(cpuPct, rule.Action.Factor, e.totalCPUPercentToCorePercent(rule.Action.MinLimitPct))
			if err := e.docker.UpdateContainerCPU(ctx, discovered.Container.ID, LimitToCPUQuota(limit), 100000); err != nil {
				e.recordEventLocked(tracked.Subject, "error", fmt.Sprintf("docker throttle failed: %v", err), cpuPct, limit)
				return
			}
			tracked.Subject.State = model.StateThrottled
			tracked.Subject.CurrentLimitPct = limit
			tracked.Subject.CooldownUntil = now.Add(rule.Cooldown)
			tracked.Subject.UpdatedAt = now
			tracked.Subject.DockerContainerID = discovered.Container.ID
			if tracked.Subject.DockerOriginalCPUPeriod == 0 && tracked.Subject.DockerOriginalCPUQuota == 0 {
				tracked.Subject.DockerOriginalCPUPeriod = discovered.Container.CPUPeriod
				tracked.Subject.DockerOriginalCPUQuota = discovered.Container.CPUQuota
			}
			e.recordEventLocked(tracked.Subject, "throttled", "docker container throttled", cpuPct, limit)
		}
	case model.StateThrottled:
		if tracked.Window.MeetsBelow(e.totalCPUPercentToCorePercent(rule.Recovery.CPUPercentTotalLT), rule.Recovery.Window, rule.Recovery.Ratio, e.cfg.Global.SampleInterval.Duration, now) {
			period := tracked.Subject.DockerOriginalCPUPeriod
			quota := tracked.Subject.DockerOriginalCPUQuota
			if err := e.docker.UpdateContainerCPU(ctx, discovered.Container.ID, quota, period); err != nil {
				e.recordEventLocked(tracked.Subject, "error", fmt.Sprintf("docker release failed: %v", err), cpuPct, 0)
				return
			}
			tracked.Subject.State = model.StateObserving
			tracked.Subject.CurrentLimitPct = 0
			tracked.Subject.CooldownUntil = now.Add(rule.Cooldown)
			tracked.Subject.UpdatedAt = now
			e.recordEventLocked(tracked.Subject, "released", "docker container released", cpuPct, 0)
		}
	}
}

func mergeSubject(current, update model.Subject) model.Subject {
	current.State = update.State
	current.CurrentLimitPct = update.CurrentLimitPct
	current.OriginalCgroupPath = update.OriginalCgroupPath
	current.ManagedCgroupPath = update.ManagedCgroupPath
	current.HostOriginalCgroups = update.HostOriginalCgroups
	current.TargetRef = update.TargetRef
	current.DisplayName = update.DisplayName
	return current
}

func maxDuration(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

func (e *Engine) ensureSubjectLocked(id string, build func() model.Subject) *trackedSubject {
	if tracked, ok := e.subjects[id]; ok {
		return tracked
	}
	subject := build()
	tracked := &trackedSubject{Subject: subject}
	e.subjects[id] = tracked
	return tracked
}

func (e *Engine) recordEventLocked(subject model.Subject, eventType, message string, cpuPct, limitPct float64) {
	retention := e.cfg.Global.EventRetention
	maxEvents := 0
	maxAge := time.Duration(0)
	if retention != nil {
		maxEvents = retention.MaxEvents
		maxAge = retention.MaxAge.Duration
	}
	if err := e.store.AddEventAndPrune(model.Event{
		SubjectID: subject.ID,
		RuleID:    subject.RuleID,
		Type:      eventType,
		Message:   message,
		CPUPct:    cpuPct,
		LimitPct:  limitPct,
		CreatedAt: time.Now(),
	}, maxEvents, maxAge); err != nil {
		log.Printf("store event: %v", err)
	}
}

func (e *Engine) ListSubjects() ([]model.Subject, error) {
	return e.store.ListSubjects()
}

func (e *Engine) QuerySubjects(query model.SubjectQuery) (model.SubjectPage, error) {
	return e.store.QuerySubjects(query)
}

func (e *Engine) SubjectSummary() (model.SubjectSummary, error) {
	return e.store.SubjectSummary()
}

func (e *Engine) ListLimits() ([]model.Subject, error) {
	subjects, err := e.store.ListSubjects()
	if err != nil {
		return nil, err
	}
	out := make([]model.Subject, 0, len(subjects))
	for _, subject := range subjects {
		if subject.State == model.StateThrottled || subject.State == model.StateManualHold {
			out = append(out, subject)
		}
	}
	return out, nil
}

func (e *Engine) SystemMetrics() model.SystemMetrics {
	e.mu.Lock()
	sampler := e.systemSampler
	if sampler == nil {
		sampler = system.NewSystemMetricsSampler(e.cpuCount)
		e.systemSampler = sampler
	}
	e.mu.Unlock()
	return sampler.Sample()
}

func (e *Engine) ListEvents(limit int) ([]model.Event, error) {
	return e.store.ListEvents(limit)
}

func (e *Engine) ListEventsForSubject(subjectID string, limit int) ([]model.Event, error) {
	return e.store.ListEventsForSubject(subjectID, limit)
}

func (e *Engine) QueryEvents(query model.EventQuery) ([]model.Event, error) {
	return e.store.QueryEvents(query)
}

func (e *Engine) ProtectedPolicy() model.ProtectedPolicy {
	return system.ProtectedPolicy()
}

func (e *Engine) Rules() []model.Rule {
	return e.cfg.RulesAsModel()
}

func (e *Engine) CPUCount() int {
	return e.cpuCount
}

func (e *Engine) pruneStaleObservingSubjects(now time.Time) {
	maxAge := e.cfg.Global.SampleInterval.Duration * 3
	if maxAge < 30*time.Second {
		maxAge = 30 * time.Second
	}
	cutoff := now.Add(-maxAge)
	e.mu.Lock()
	for id, tracked := range e.subjects {
		if tracked.Subject.UpdatedAt.IsZero() || !tracked.Subject.UpdatedAt.Before(cutoff) {
			continue
		}
		switch tracked.Subject.State {
		case model.StateObserving:
			delete(e.subjects, id)
		case model.StateThrottled, model.StateManualHold:
			if err := e.releaseSubjectLocked(context.Background(), tracked); err != nil {
				e.recordEventLocked(tracked.Subject, "error", fmt.Sprintf("stale release failed: %v", err), tracked.Subject.LastCPUPct, 0)
				continue
			}
			tracked.Subject.CooldownUntil = now.Add(e.cfg.Global.SampleInterval.Duration)
			tracked.Subject.UpdatedAt = now
			e.recordEventLocked(tracked.Subject, "stale_release", "released stale subject after it disappeared from discovery", tracked.Subject.LastCPUPct, 0)
			if err := e.store.UpsertSubject(tracked.Subject); err != nil {
				log.Printf("store stale released subject: %v", err)
			}
		}
	}
	e.mu.Unlock()
	if err := e.store.DeleteStaleObservingSubjects(cutoff); err != nil {
		log.Printf("delete stale observing subjects: %v", err)
	}
}

func (e *Engine) Reload(ctx context.Context, path string) error {
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	matched, err := discoverSubjectIDsForRules(ctx, e.docker, cfg.RulesAsModel())
	if err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for id, tracked := range e.subjects {
		if tracked.Subject.State != model.StateThrottled && tracked.Subject.State != model.StateManualHold {
			continue
		}
		if matched[id] {
			continue
		}
		if err := e.releaseSubjectLocked(ctx, tracked); err != nil {
			return err
		}
		tracked.Subject.UpdatedAt = time.Now()
		e.recordEventLocked(tracked.Subject, "reload_release", "released because subject no longer matches reloaded config", tracked.Subject.LastCPUPct, 0)
		if err := e.store.UpsertSubject(tracked.Subject); err != nil {
			return err
		}
	}
	e.cfg = cfg
	return nil
}

func discoverSubjectIDsForRules(ctx context.Context, docker *system.DockerClient, rules []model.Rule) (map[string]bool, error) {
	matched := map[string]bool{}
	for _, rule := range rules {
		switch rule.Scope {
		case model.ScopeHostProcess:
			subjects, err := system.DiscoverHostSubjects(rule)
			if err != nil {
				return nil, err
			}
			for _, subject := range subjects {
				matched[subject.SubjectID()] = true
			}
		case model.ScopeDockerContainer:
			subjects, err := system.DiscoverDockerSubjects(ctx, docker, rule)
			if err != nil {
				return nil, err
			}
			for _, subject := range subjects {
				matched[subject.SubjectID()] = true
			}
		}
	}
	return matched, nil
}

func (e *Engine) Unthrottle(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	tracked, ok := e.subjects[id]
	if !ok {
		return fmt.Errorf("subject not found: %s", id)
	}
	if tracked.Subject.State == model.StateObserving {
		return nil
	}
	if err := e.releaseSubjectLocked(ctx, tracked); err != nil {
		return err
	}
	tracked.Subject.State = model.StateObserving
	tracked.Subject.CurrentLimitPct = 0
	tracked.Subject.UpdatedAt = time.Now()
	e.recordEventLocked(tracked.Subject, "manual_release", "manual release via API", tracked.Subject.LastCPUPct, 0)
	return e.store.UpsertSubject(tracked.Subject)
}

func (e *Engine) releaseSubjectLocked(ctx context.Context, tracked *trackedSubject) error {
	switch tracked.Subject.Scope {
	case model.ScopeHostProcess:
		if err := e.cgroups.ReleaseHost(tracked.Subject); err != nil {
			return err
		}
	case model.ScopeDockerContainer:
		period := tracked.Subject.DockerOriginalCPUPeriod
		quota := tracked.Subject.DockerOriginalCPUQuota
		if err := e.docker.UpdateContainerCPU(ctx, tracked.Subject.DockerContainerID, quota, period); err != nil {
			return err
		}
	}
	tracked.Subject.State = model.StateObserving
	tracked.Subject.CurrentLimitPct = 0
	return nil
}

func (e *Engine) Hold(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	tracked, ok := e.subjects[id]
	if !ok {
		return fmt.Errorf("subject not found: %s", id)
	}
	if tracked.Subject.State == model.StateThrottled {
		tracked.Subject.State = model.StateManualHold
		tracked.Subject.UpdatedAt = time.Now()
		e.recordEventLocked(tracked.Subject, "manual_hold", "manual hold via API", tracked.Subject.LastCPUPct, tracked.Subject.CurrentLimitPct)
		return e.store.UpsertSubject(tracked.Subject)
	}
	return fmt.Errorf("subject %s is not throttled", id)
}

func (e *Engine) ThrottleNow(ctx context.Context, id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	tracked, ok := e.subjects[id]
	if !ok {
		return fmt.Errorf("subject not found: %s", id)
	}
	if tracked.Subject.State != model.StateObserving {
		return nil
	}
	rule, err := e.findRule(tracked.Subject.RuleID)
	if err != nil {
		return err
	}
	limit := CalculateLimit(tracked.Subject.LastCPUPct, rule.Action.Factor, e.totalCPUPercentToCorePercent(rule.Action.MinLimitPct))
	switch tracked.Subject.Scope {
	case model.ScopeDockerContainer:
		if err := e.docker.UpdateContainerCPU(ctx, tracked.Subject.DockerContainerID, LimitToCPUQuota(limit), 100000); err != nil {
			return err
		}
	case model.ScopeHostProcess:
		if tracked.Host == nil {
			return fmt.Errorf("manual host throttle requires a live discovery cycle")
		}
		next, err := e.cgroups.ThrottleHost(*tracked.Host, &tracked.Subject, limit)
		if err != nil {
			return err
		}
		next.LastCPUPct = tracked.Subject.LastCPUPct
		next.UpdatedAt = time.Now()
		tracked.Subject = mergeSubject(tracked.Subject, next)
	}
	tracked.Subject.State = model.StateThrottled
	tracked.Subject.CurrentLimitPct = limit
	tracked.Subject.UpdatedAt = time.Now()
	e.recordEventLocked(tracked.Subject, "manual_throttle", "manual throttle via API", tracked.Subject.LastCPUPct, limit)
	return e.store.UpsertSubject(tracked.Subject)
}

func (e *Engine) totalCPUPercentToCorePercent(totalCPUPercent float64) float64 {
	if e.cpuCount < 1 {
		return totalCPUPercent
	}
	return totalCPUPercent * float64(e.cpuCount)
}

func (e *Engine) findRule(id string) (model.Rule, error) {
	for _, rule := range e.cfg.RulesAsModel() {
		if rule.ID == id {
			return rule, nil
		}
	}
	return model.Rule{}, fmt.Errorf("rule not found: %s", id)
}
