package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultDaemonConfig(t *testing.T) {
	cfg := DefaultDaemonConfig()
	if cfg.UpdateInterval != 24*time.Hour {
		t.Errorf("expected default update interval of 24h, got %s", cfg.UpdateInterval)
	}
	if !cfg.SnapshotBeforeUpdate {
		t.Errorf("expected snapshot_before_update to default true")
	}
}

func TestLoadDaemonConfigMissingFile(t *testing.T) {
	cfg, err := LoadDaemonConfig(filepath.Join(t.TempDir(), "nope.hk"))
	if err != nil {
		t.Fatalf("expected no error for missing file, got %v", err)
	}
	if cfg.UpdateInterval != DefaultDaemonConfig().UpdateInterval {
		t.Errorf("expected defaults when file is missing")
	}
}

func TestLoadDaemonConfigParsesIntervals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.hk")
	content := `[schedule]
-> update_interval        => 1h
-> autoremove_interval    => 48h
-> clean_interval         => 72h
-> snapshot_before_update => false

[socket]
-> path => /tmp/custom.sock

[log]
-> path => /tmp/custom.log
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadDaemonConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.UpdateInterval != time.Hour {
		t.Errorf("expected 1h, got %s", cfg.UpdateInterval)
	}
	if cfg.AutoremoveInterval != 48*time.Hour {
		t.Errorf("expected 48h, got %s", cfg.AutoremoveInterval)
	}
	if cfg.CleanInterval != 72*time.Hour {
		t.Errorf("expected 72h, got %s", cfg.CleanInterval)
	}
	if cfg.SnapshotBeforeUpdate {
		t.Errorf("expected snapshot_before_update=false to be respected")
	}
	if cfg.SocketPath != "/tmp/custom.sock" {
		t.Errorf("expected custom socket path, got %s", cfg.SocketPath)
	}
	if cfg.LogPath != "/tmp/custom.log" {
		t.Errorf("expected custom log path, got %s", cfg.LogPath)
	}
}

func TestTaskArgs(t *testing.T) {
	cases := map[string][]string{
		"update":     {"update"},
		"autoremove": {"autoremove"},
		"clean":      {"clean"},
		"snapshot":   {"snapshot", "--all"},
		"rollback":   {"rollback", "--all"},
	}
	for task, want := range cases {
		got := taskArgs(task, false)
		if len(got) != len(want) {
			t.Errorf("taskArgs(%q): expected %v, got %v", task, want, got)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("taskArgs(%q): expected %v, got %v", task, want, got)
			}
		}
	}
	if taskArgs("bogus", false) != nil {
		t.Errorf("expected nil for unknown task")
	}
	got := taskArgs("update", true)
	if got[len(got)-1] != "--dry-run" {
		t.Errorf("expected --dry-run appended, got %v", got)
	}
}
