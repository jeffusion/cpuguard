package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cpuguard/internal/model"
)

type SystemMetricsSampler struct {
	mu        sync.Mutex
	cpuCount  int
	prev      systemCPUTimes
	prevPower map[string]raplEnergySample
}

type systemCPUTimes struct {
	total uint64
	idle  uint64
}

type raplEnergySample struct {
	energyUJ   uint64
	maxRangeUJ uint64
	at         time.Time
}

type raplDomain struct {
	id         string
	name       string
	energyUJ   uint64
	maxRangeUJ uint64
}

func NewSystemMetricsSampler(cpuCount int) *SystemMetricsSampler {
	if cpuCount < 1 {
		cpuCount = 1
	}
	return &SystemMetricsSampler{cpuCount: cpuCount, prevPower: map[string]raplEnergySample{}}
}

func (s *SystemMetricsSampler) Sample() model.SystemMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	times, err := readSystemCPUTimes()
	cpuPct := 0.0
	if err == nil && s.prev.total > 0 && times.total > s.prev.total {
		cpuPct = calculateSystemCPUPercent(s.prev, times)
	}
	if err == nil {
		s.prev = times
	}
	load1, load5, load15 := readLoadAverage()
	cpuTempC := readCPUTemperatureC()
	cpuPowerW := s.sampleCPUPowerWatts(now)
	return model.SystemMetrics{
		CPUPercent: cpuPct,
		CPUCount:   s.cpuCount,
		Load1:      load1,
		Load5:      load5,
		Load15:     load15,
		CPUTempC:   cpuTempC,
		CPUPowerW:  cpuPowerW,
		SampledAt:  now,
	}
}

func (s *SystemMetricsSampler) sampleCPUPowerWatts(now time.Time) *float64 {
	domains := readRAPLDomains("/sys/class/powercap")
	if len(domains) == 0 {
		s.prevPower = map[string]raplEnergySample{}
		return nil
	}
	next := make(map[string]raplEnergySample, len(domains))
	totalWatts := 0.0
	hasPower := false
	for _, domain := range domains {
		prev, ok := s.prevPower[domain.id]
		next[domain.id] = raplEnergySample{energyUJ: domain.energyUJ, maxRangeUJ: domain.maxRangeUJ, at: now}
		if !ok {
			continue
		}
		seconds := now.Sub(prev.at).Seconds()
		if seconds <= 0 {
			continue
		}
		delta := calculateEnergyDeltaUJ(prev.energyUJ, domain.energyUJ, domain.maxRangeUJ)
		totalWatts += float64(delta) / 1_000_000 / seconds
		hasPower = true
	}
	s.prevPower = next
	if !hasPower {
		return nil
	}
	return floatPtr(totalWatts)
}

func readSystemCPUTimes() (systemCPUTimes, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return systemCPUTimes{}, err
	}
	line := strings.SplitN(string(data), "\n", 2)[0]
	return parseSystemCPUTimes(line)
}

func parseSystemCPUTimes(line string) (systemCPUTimes, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return systemCPUTimes{}, fmt.Errorf("unexpected /proc/stat cpu line")
	}
	values := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return systemCPUTimes{}, err
		}
		values = append(values, value)
	}
	user := values[0]
	nice := values[1]
	system := values[2]
	idle := values[3]
	iowait := uint64(0)
	irq := uint64(0)
	softIRQ := uint64(0)
	steal := uint64(0)
	if len(values) > 4 {
		iowait = values[4]
	}
	if len(values) > 5 {
		irq = values[5]
	}
	if len(values) > 6 {
		softIRQ = values[6]
	}
	if len(values) > 7 {
		steal = values[7]
	}
	idleAll := idle + iowait
	nonIdle := user + nice + system + irq + softIRQ + steal
	return systemCPUTimes{total: idleAll + nonIdle, idle: idleAll}, nil
}

func calculateSystemCPUPercent(prev, next systemCPUTimes) float64 {
	if next.total <= prev.total || next.idle < prev.idle {
		return 0
	}
	totalDelta := next.total - prev.total
	idleDelta := next.idle - prev.idle
	if totalDelta == 0 || idleDelta > totalDelta {
		return 0
	}
	return (float64(totalDelta-idleDelta) / float64(totalDelta)) * 100
}

func readLoadAverage() (float64, float64, float64) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	load1, _ := strconv.ParseFloat(fields[0], 64)
	load5, _ := strconv.ParseFloat(fields[1], 64)
	load15, _ := strconv.ParseFloat(fields[2], 64)
	return load1, load5, load15
}

func readCPUTemperatureC() *float64 {
	return readCPUTemperatureCFrom("/sys/class/thermal", "/sys/class/hwmon")
}

func ReadCPUTemperatureCForDoctor() *float64 {
	return readCPUTemperatureC()
}

func readCPUTemperatureCFrom(thermalRoot string, hwmonRoot string) *float64 {
	candidates := []float64{}
	for _, path := range glob(filepath.Join(thermalRoot, "thermal_zone*", "temp")) {
		zoneDir := filepath.Dir(path)
		sensorType := strings.ToLower(strings.TrimSpace(readString(filepath.Join(zoneDir, "type"))))
		if !isCPUTemperatureSensor(sensorType, sensorType) {
			continue
		}
		if temp, ok := readMilliCelsius(path); ok {
			candidates = append(candidates, temp)
		}
	}
	for _, path := range glob(filepath.Join(hwmonRoot, "hwmon*", "temp*_input")) {
		hwmonDir := filepath.Dir(path)
		name := strings.ToLower(strings.TrimSpace(readString(filepath.Join(hwmonDir, "name"))))
		label := strings.ToLower(strings.TrimSpace(readString(strings.TrimSuffix(path, "_input") + "_label")))
		if !isCPUTemperatureSensor(name, label) {
			continue
		}
		if temp, ok := readMilliCelsius(path); ok {
			candidates = append(candidates, temp)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	maxTemp := candidates[0]
	for _, temp := range candidates[1:] {
		if temp > maxTemp {
			maxTemp = temp
		}
	}
	return floatPtr(maxTemp)
}

func readRAPLDomains(root string) []raplDomain {
	domainPaths := glob(filepath.Join(root, "intel-rapl*"))
	domains := make([]raplDomain, 0, len(domainPaths))
	for _, domainDir := range domainPaths {
		energyPath := filepath.Join(domainDir, "energy_uj")
		name := strings.ToLower(strings.TrimSpace(readString(filepath.Join(domainDir, "name"))))
		if !isCPUPowerDomain(name) {
			continue
		}
		energy, ok := readUint(energyPath)
		if !ok {
			continue
		}
		maxRange, _ := readUint(filepath.Join(domainDir, "max_energy_range_uj"))
		domains = append(domains, raplDomain{
			id:         domainDir,
			name:       name,
			energyUJ:   energy,
			maxRangeUJ: maxRange,
		})
	}
	return domains
}

func ReadRAPLDomainsForDoctor(root string) []raplDomain {
	return readRAPLDomains(root)
}

func isCPUTemperatureSensor(name string, label string) bool {
	text := strings.ToLower(name + " " + label)
	if strings.Contains(text, "nvme") || strings.Contains(text, "gpu") || strings.Contains(text, "wifi") {
		return false
	}
	keywords := []string{"cpu", "package", "coretemp", "k10temp", "tctl", "tdie", "x86_pkg_temp", "soc"}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func isCPUPowerDomain(name string) bool {
	name = strings.ToLower(name)
	if strings.Contains(name, "dram") || strings.Contains(name, "uncore") || strings.Contains(name, "psys") {
		return false
	}
	return strings.Contains(name, "package") || strings.Contains(name, "cpu")
}

func readMilliCelsius(path string) (float64, bool) {
	value, ok := readUint(path)
	if !ok {
		return 0, false
	}
	temp := float64(value) / 1000
	if temp <= 0 || temp >= 150 {
		return 0, false
	}
	return temp, true
}

func calculateEnergyDeltaUJ(prev uint64, next uint64, maxRange uint64) uint64 {
	if next >= prev {
		return next - prev
	}
	if maxRange == 0 || prev > maxRange {
		return 0
	}
	return (maxRange - prev) + next
}

func readString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func readUint(path string) (uint64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func glob(pattern string) []string {
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return paths
}

func floatPtr(value float64) *float64 {
	return &value
}
