package system

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSystemCPUTimes(t *testing.T) {
	times, err := parseSystemCPUTimes("cpu  100 20 30 800 50 4 6 10 0 0")
	if err != nil {
		t.Fatal(err)
	}
	if times.idle != 850 {
		t.Fatalf("idle = %d, want 850", times.idle)
	}
	if times.total != 1020 {
		t.Fatalf("total = %d, want 1020", times.total)
	}
}

func TestCalculateSystemCPUPercent(t *testing.T) {
	prev := systemCPUTimes{total: 1000, idle: 800}
	next := systemCPUTimes{total: 1100, idle: 850}
	if got := calculateSystemCPUPercent(prev, next); got != 50 {
		t.Fatalf("cpu percent = %.1f, want 50.0", got)
	}
}

func TestReadCPUTemperatureCFromThermalZone(t *testing.T) {
	root := t.TempDir()
	thermal := filepath.Join(root, "thermal")
	hwmon := filepath.Join(root, "hwmon")
	zone := filepath.Join(thermal, "thermal_zone0")
	if err := os.MkdirAll(zone, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(zone, "type"), "x86_pkg_temp\n")
	writeTestFile(t, filepath.Join(zone, "temp"), "65500\n")

	temp := readCPUTemperatureCFrom(thermal, hwmon)
	if temp == nil || *temp != 65.5 {
		t.Fatalf("temp = %v, want 65.5", temp)
	}
}

func TestReadCPUTemperatureCFromHwmon(t *testing.T) {
	root := t.TempDir()
	thermal := filepath.Join(root, "thermal")
	hwmon := filepath.Join(root, "hwmon")
	device := filepath.Join(hwmon, "hwmon0")
	if err := os.MkdirAll(device, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(device, "name"), "k10temp\n")
	writeTestFile(t, filepath.Join(device, "temp1_label"), "Tctl\n")
	writeTestFile(t, filepath.Join(device, "temp1_input"), "70250\n")

	temp := readCPUTemperatureCFrom(thermal, hwmon)
	if temp == nil || *temp != 70.25 {
		t.Fatalf("temp = %v, want 70.25", temp)
	}
}

func TestReadRAPLDomainsSelectsPackageOnly(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "intel-rapl:0")
	dramDir := filepath.Join(root, "intel-rapl:0:0")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dramDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(packageDir, "name"), "package-0\n")
	writeTestFile(t, filepath.Join(packageDir, "energy_uj"), "12000000\n")
	writeTestFile(t, filepath.Join(packageDir, "max_energy_range_uj"), "100000000\n")
	writeTestFile(t, filepath.Join(dramDir, "name"), "dram\n")
	writeTestFile(t, filepath.Join(dramDir, "energy_uj"), "5000000\n")

	domains := readRAPLDomains(root)
	if len(domains) != 1 {
		t.Fatalf("domains len = %d, want 1: %+v", len(domains), domains)
	}
	if domains[0].energyUJ != 12000000 {
		t.Fatalf("energy = %d, want 12000000", domains[0].energyUJ)
	}
}

func TestCalculateEnergyDeltaUJHandlesWrap(t *testing.T) {
	if got := calculateEnergyDeltaUJ(95, 10, 100); got != 15 {
		t.Fatalf("delta = %d, want 15", got)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
