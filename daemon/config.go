package main

import (
	"fmt"
	"os"
	"time"

	"isolator-daemon/internal/hk"
)

// DaemonConfig is the daemon's own schedule, parsed from daemon.hk —
// separate from Isolator's config.hk (the daemon doesn't manage
// GUI/GPU/theme settings, it manages *when* to run Isolator's own
// maintenance commands unattended).
type DaemonConfig struct {
	UpdateInterval       time.Duration
	AutoremoveInterval   time.Duration
	CleanInterval        time.Duration
	SnapshotBeforeUpdate bool
	SocketPath           string
	LogPath              string
}

func DefaultDaemonConfig() DaemonConfig {
	return DaemonConfig{
		UpdateInterval:       24 * time.Hour,
		AutoremoveInterval:   7 * 24 * time.Hour,
		CleanInterval:        7 * 24 * time.Hour,
		SnapshotBeforeUpdate: true,
		SocketPath:           "/run/isolator-daemon.sock",
		LogPath:              "",
	}
}

// LoadDaemonConfig reads a daemon.hk file. Missing keys fall back to
// DefaultDaemonConfig's values; a missing file entirely just returns the
// defaults outright (the daemon is meant to run zero-config out of the
// box — "expand later if you want" rather than "configure before first
// run").
func LoadDaemonConfig(path string) (DaemonConfig, error) {
	cfg := DefaultDaemonConfig()
	if path == "" {
		return cfg, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil
	}
	doc, err := hk.LoadHKFile(path)
	if err != nil {
		return cfg, fmt.Errorf("parsing %s: %w", path, err)
	}
	if err := hk.ResolveInterpolations(doc); err != nil {
		return cfg, err
	}

	sched := doc.Section("schedule")
	if d, err := getDuration(sched, "update_interval"); err == nil {
		cfg.UpdateInterval = d
	}
	if d, err := getDuration(sched, "autoremove_interval"); err == nil {
		cfg.AutoremoveInterval = d
	}
	if d, err := getDuration(sched, "clean_interval"); err == nil {
		cfg.CleanInterval = d
	}
	if v, ok := sched.Get("snapshot_before_update"); ok {
		if b, err := v.AsBool(); err == nil {
			cfg.SnapshotBeforeUpdate = b
		}
	}

	sock := doc.Section("socket")
	if v, ok := sock.Get("path"); ok {
		if s, err := v.AsString(); err == nil && s != "" {
			cfg.SocketPath = s
		}
	}

	logSec := doc.Section("log")
	if v, ok := logSec.Get("path"); ok {
		if s, err := v.AsString(); err == nil {
			cfg.LogPath = s
		}
	}

	return cfg, nil
}

func getDuration(m *hk.HkMap, key string) (time.Duration, error) {
	v, ok := m.Get(key)
	if !ok {
		return 0, fmt.Errorf("not set")
	}
	s, err := v.AsString()
	if err != nil {
		return 0, err
	}
	return time.ParseDuration(s)
}
