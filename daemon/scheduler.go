package main

import (
	"fmt"
	"os/exec"
	"sync"
	"time"
)

// Scheduler runs Isolator's maintenance commands on independent intervals,
// entirely by shelling out to the real `isolator` binary — the daemon
// itself has zero container-management logic; it's purely a clock that
// knows when to say "isolator update", the same way a person running cron
// jobs would, just built in and aware of Isolator's own commands
// (including --dry-run, which `daemon trigger --dry-run <task>` exposes
// for testing a schedule without side effects).
type Scheduler struct {
	cfg DaemonConfig
	log *Logger

	mu      sync.Mutex
	lastRun map[string]time.Time
	running bool
	stopCh  chan struct{}
}

func NewScheduler(cfg DaemonConfig, log *Logger) *Scheduler {
	return &Scheduler{
		cfg:     cfg,
		log:     log,
		lastRun: map[string]time.Time{},
		stopCh:  make(chan struct{}),
	}
}

// Run blocks, ticking once a minute and firing any task whose interval has
// elapsed since it last ran. A minute granularity is plenty for
// hour/day/week-scale maintenance intervals and keeps the loop cheap.
func (s *Scheduler) Run() {
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	s.log.Printf("daemon started — update every %s, autoremove every %s, clean every %s",
		s.cfg.UpdateInterval, s.cfg.AutoremoveInterval, s.cfg.CleanInterval)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Run once immediately on startup so a freshly-started daemon doesn't
	// wait a full interval before doing anything useful.
	s.tick()

	for {
		select {
		case <-ticker.C:
			s.tick()
		case <-s.stopCh:
			s.log.Printf("daemon stopping")
			return
		}
	}
}

func (s *Scheduler) Stop() {
	close(s.stopCh)
}

func (s *Scheduler) tick() {
	s.maybeRun("update", s.cfg.UpdateInterval, s.runUpdate)
	s.maybeRun("autoremove", s.cfg.AutoremoveInterval, func() { s.runIsolator("autoremove") })
	s.maybeRun("clean", s.cfg.CleanInterval, func() { s.runIsolator("clean") })
}

func (s *Scheduler) maybeRun(name string, interval time.Duration, fn func()) {
	if interval <= 0 {
		return // 0 or negative disables the task entirely
	}
	s.mu.Lock()
	last, ok := s.lastRun[name]
	due := !ok || time.Since(last) >= interval
	if due {
		s.lastRun[name] = time.Now()
	}
	s.mu.Unlock()

	if due {
		s.log.Printf("running scheduled task: %s", name)
		fn()
	}
}

func (s *Scheduler) runUpdate() {
	if s.cfg.SnapshotBeforeUpdate {
		s.runIsolator("snapshot", "--all")
	}
	s.runIsolator("update")
}

// runIsolator shells out to the real isolator binary and logs its outcome.
func (s *Scheduler) runIsolator(args ...string) {
	cmd := exec.Command("isolator", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.log.Printf("isolator %v failed: %v\n%s", args, err, out)
		return
	}
	s.log.Printf("isolator %v completed", args)
}

// TriggerNow runs a named task immediately, out of band from its regular
// schedule — used by both `daemon trigger <task>` run directly and by the
// socket API's "trigger" command against a running daemon.
func TriggerNow(task string, dryRun bool) error {
	args := taskArgs(task, dryRun)
	if args == nil {
		return fmt.Errorf("unknown task %q (expected: update, autoremove, clean, snapshot, rollback)", task)
	}
	cmd := exec.Command("isolator", args...)
	cmd.Stdout, cmd.Stderr = stdoutStderr()
	return cmd.Run()
}

func taskArgs(task string, dryRun bool) []string {
	var args []string
	switch task {
	case "update":
		args = []string{"update"}
	case "autoremove":
		args = []string{"autoremove"}
	case "clean":
		args = []string{"clean"}
	case "snapshot":
		args = []string{"snapshot", "--all"}
	case "rollback":
		args = []string{"rollback", "--all"}
	default:
		return nil
	}
	if dryRun {
		args = append(args, "--dry-run")
	}
	return args
}
