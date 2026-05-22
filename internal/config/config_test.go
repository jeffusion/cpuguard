package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := []byte(`
rules:
  - id: sample
    scope: host_process
    selector:
      process_name: java
`)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Global.SampleInterval.Duration == 0 {
		t.Fatal("expected default sample interval")
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("unexpected rules count: %d", len(cfg.Rules))
	}
}
