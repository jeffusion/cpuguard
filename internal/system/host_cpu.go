package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"cpuguard/internal/model"
)

type HostCPUSampler struct {
	prevSystemJiffies map[string]uint64
	prevSubjectTicks  map[string]uint64
	cpuCount          int
}

func NewHostCPUSampler(cpuCount int) *HostCPUSampler {
	if cpuCount < 1 {
		cpuCount = 1
	}
	return &HostCPUSampler{
		prevSystemJiffies: map[string]uint64{},
		prevSubjectTicks:  map[string]uint64{},
		cpuCount:          cpuCount,
	}
}

func (s *HostCPUSampler) Sample(subject model.HostSubject) (float64, error) {
	systemJiffies, err := readSystemJiffies()
	if err != nil {
		return 0, err
	}
	var totalTicks uint64
	for _, pid := range subject.Descendants {
		ticks, err := readProcTicks(pid)
		if err != nil {
			continue
		}
		totalTicks += ticks
	}
	key := subject.SubjectID()
	prevSubject := s.prevSubjectTicks[key]
	prevSystem := s.prevSystemJiffies[key]
	s.prevSubjectTicks[key] = totalTicks
	s.prevSystemJiffies[key] = systemJiffies
	if prevSystem == 0 || systemJiffies <= prevSystem || totalTicks < prevSubject {
		return 0, nil
	}
	deltaSubject := totalTicks - prevSubject
	deltaSystem := systemJiffies - prevSystem
	cpuPct := (float64(deltaSubject) / float64(deltaSystem)) * float64(s.cpuCount) * 100
	return cpuPct, nil
}

func readSystemJiffies() (uint64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(strings.SplitN(string(data), "\n", 2)[0])
	if len(fields) < 2 || fields[0] != "cpu" {
		return 0, fmt.Errorf("unexpected /proc/stat format")
	}
	var total uint64
	for _, field := range fields[1:] {
		v, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return 0, err
		}
		total += v
	}
	return total, nil
}

func readProcTicks(pid int) (uint64, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	stat := string(data)
	end := strings.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, fmt.Errorf("unexpected stat")
	}
	fields := strings.Fields(stat[end+2:])
	if len(fields) < 15 {
		return 0, fmt.Errorf("unexpected stat fields")
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, err
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, err
	}
	return utime + stime, nil
}

type SampledCPU struct {
	At     time.Time
	CPUPct float64
}
