package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"cpuguard/internal/model"
)

const cgroupRoot = "/sys/fs/cgroup"

type CgroupManager struct {
	Root string
}

func (m *CgroupManager) ThrottleHost(subject model.HostSubject, existing *model.Subject, limitPct float64) (model.Subject, error) {
	originalPath := subject.Root.CgroupPath
	if originalPath == "" {
		originalPath = "/"
	}
	managedPath := existing.ManagedCgroupPath
	if managedPath == "" {
		managedPath = joinCgroupPath("/cpuguard", sanitize(subject.Rule.ID), modelHostPID(subject.Root.PID))
	}
	fullManaged, err := m.ensureManagedCgroup(managedPath)
	if err != nil {
		return model.Subject{}, err
	}
	cpuMax := fmt.Sprintf("%d %d\n", limitToCPUQuota(limitPct), 100000)
	if err := os.WriteFile(filepath.Join(fullManaged, "cpu.max"), []byte(cpuMax), 0o644); err != nil {
		return model.Subject{}, err
	}
	originals := map[string]string{}
	if existing.HostOriginalCgroups != "" {
		_ = json.Unmarshal([]byte(existing.HostOriginalCgroups), &originals)
	}
	for _, pid := range subject.Descendants {
		pidText := modelHostPID(pid)
		if _, ok := originals[pidText]; !ok {
			if cg, err := readCgroupPath(pid); err == nil && cg != "" {
				originals[pidText] = cg
			} else {
				originals[pidText] = originalPath
			}
		}
		if err := os.WriteFile(filepath.Join(fullManaged, "cgroup.procs"), []byte(pidText), 0o644); err != nil {
			continue
		}
	}
	originalsJSON, _ := json.Marshal(originals)
	return model.Subject{
		ID:                  subject.SubjectID(),
		RuleID:              subject.Rule.ID,
		Scope:               model.ScopeHostProcess,
		TargetRef:           subject.TargetReference(),
		DisplayName:         subject.TargetReference(),
		State:               model.StateThrottled,
		CurrentLimitPct:     limitPct,
		OriginalCgroupPath:  originalPath,
		ManagedCgroupPath:   managedPath,
		HostOriginalCgroups: string(originalsJSON),
	}, nil
}

func (m *CgroupManager) ReleaseHost(subject model.Subject) error {
	if subject.ManagedCgroupPath == "" || subject.OriginalCgroupPath == "" {
		return nil
	}
	fullManaged := filepath.Join(m.root(), strings.TrimPrefix(subject.ManagedCgroupPath, "/"))
	fullOriginal := filepath.Join(m.root(), strings.TrimPrefix(subject.OriginalCgroupPath, "/"))
	data, err := os.ReadFile(filepath.Join(fullManaged, "cgroup.procs"))
	originals := map[string]string{}
	if subject.HostOriginalCgroups != "" {
		_ = json.Unmarshal([]byte(subject.HostOriginalCgroups), &originals)
	}
	if err == nil {
		for _, pid := range strings.Fields(string(data)) {
			target := originals[pid]
			if target == "" {
				target = subject.OriginalCgroupPath
			}
			if target == "" {
				target = "/"
			}
			fullTarget := filepath.Join(m.root(), strings.TrimPrefix(target, "/"))
			if _, err := os.Stat(fullTarget); err != nil {
				fullTarget = fullOriginal
			}
			_ = os.WriteFile(filepath.Join(fullTarget, "cgroup.procs"), []byte(pid), 0o644)
		}
	}
	_ = os.WriteFile(filepath.Join(fullManaged, "cpu.max"), []byte("max 100000\n"), 0o644)
	_ = os.Remove(fullManaged)
	return nil
}

func (m *CgroupManager) ensureManagedCgroup(path string) (string, error) {
	root := m.root()
	parts := strings.FieldsFunc(strings.Trim(path, "/"), func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return root, nil
	}
	current := root
	for i, part := range parts {
		if i > 0 {
			if err := enableCPUController(current); err != nil {
				return "", fmt.Errorf("enable cpu controller for %s: %w", current, err)
			}
		}
		current = filepath.Join(current, part)
		if err := os.Mkdir(current, 0o755); err != nil && !os.IsExist(err) {
			return "", err
		}
	}
	return current, nil
}

func (m *CgroupManager) root() string {
	if m.Root != "" {
		return m.Root
	}
	return cgroupRoot
}

func enableCPUController(path string) error {
	controllers, err := readCgroupFields(filepath.Join(path, "cgroup.controllers"))
	if err != nil {
		return err
	}
	if !slices.Contains(controllers, "cpu") {
		return fmt.Errorf("cpu controller is not available")
	}
	enabled, err := readCgroupFields(filepath.Join(path, "cgroup.subtree_control"))
	if err != nil {
		return err
	}
	if slices.Contains(enabled, "cpu") {
		return nil
	}
	return os.WriteFile(filepath.Join(path, "cgroup.subtree_control"), []byte("+cpu\n"), 0o644)
}

func readCgroupFields(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(data)), nil
}

func joinCgroupPath(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part != "" {
			clean = append(clean, part)
		}
	}
	if len(clean) == 0 {
		return "/"
	}
	return "/" + strings.Join(clean, "/")
}

func sanitize(v string) string {
	replacer := strings.NewReplacer("/", "_", " ", "_", ":", "_")
	return replacer.Replace(v)
}

func modelHostPID(pid int) string {
	if pid == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", pid)
}

func limitToCPUQuota(limitPct float64) int64 {
	if limitPct <= 0 {
		return 100000
	}
	return int64((limitPct / 100.0) * 100000.0)
}
