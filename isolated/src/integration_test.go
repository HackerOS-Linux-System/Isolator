package src

import (
	"os/exec"
	"testing"
	"time"
)

// requirePodman skips the test unless a real, working podman binary is on
// PATH. This is what makes these "integration" tests instead of unit
// tests: they exercise the actual container lifecycle, not mocked
// behavior. They're written to run in CI / on any developer machine with
// Podman installed, even though this particular sandbox has no Podman
// (containers-in-a-container generally isn't available here), so they
// will show as skipped here and PASS/FAIL for real everywhere else.
func requirePodman(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("podman"); err != nil {
		t.Skip("podman not available — skipping integration test")
	}
	if err := exec.Command("podman", "info").Run(); err != nil {
		t.Skip("podman is installed but not usable in this environment — skipping integration test")
	}
}

func TestIntegration_ContainerLifecycle(t *testing.T) {
	requirePodman(t)

	name := "isolator-integration-test-container"
	// Clean up anything left over from a previous failed run first.
	exec.Command("podman", "rm", "--force", name).Run()
	defer exec.Command("podman", "rm", "--force", name).Run()

	if ContainerExists(name) {
		t.Fatalf("test container unexpectedly already exists")
	}

	if !PullImage("alpine:latest") {
		t.Fatalf("failed to pull alpine:latest")
	}

	if !CreateContainer(name, "alpine:latest", "", "cli", "systemd") {
		t.Fatalf("CreateContainer failed")
	}

	// Give it a moment to settle before we check status.
	time.Sleep(500 * time.Millisecond)

	if !ContainerExists(name) {
		t.Fatalf("container should exist after CreateContainer")
	}
	if !EnsureContainerRunning(name) {
		t.Fatalf("EnsureContainerRunning failed on a container that should be running")
	}

	if !ExecInContainer(name, "echo hello-from-container", false, false) {
		t.Fatalf("ExecInContainer failed to run a trivial command")
	}

	out, ok := ExecInContainerWithOutput(name, "echo hello-output", false)
	if !ok {
		t.Fatalf("ExecInContainerWithOutput failed")
	}
	if out != "hello-output" {
		t.Fatalf("expected 'hello-output', got %q", out)
	}

	if !ExecCommand("podman", []string{"rm", "--force", name}) {
		t.Fatalf("failed to remove test container")
	}
	if ContainerExists(name) {
		t.Fatalf("container should not exist after removal")
	}
}

// TestNonSystemdDistroSkipsSystemdFlag verifies that a distro whose
// Distro.InitSystem isn't "systemd" doesn't get --systemd=always silently
// attached — this is a pure function test (getPodmanRunArgs doesn't shell
// out), so it runs everywhere, unlike the podman-gated tests above.
func TestNonSystemdDistroSkipsSystemdFlag(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowSystemContainers = true
	cfg.EnableGUI = true
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}
	defer SaveConfig(DefaultConfig())

	args := getPodmanRunArgs("isolator-it-sysvinit-check", "alpine:latest", "", "system", "sysvinit")
	for i, a := range args {
		if a == "--systemd" && i+1 < len(args) && args[i+1] == "always" {
			t.Fatalf("expected no --systemd=always for a sysvinit distro, got args: %v", args)
		}
	}
}
