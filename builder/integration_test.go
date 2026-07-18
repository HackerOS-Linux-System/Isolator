package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireRootAndDebootstrap skips unless we can actually exercise the real
// bootstrap pipeline: root privileges (chroot/mount need it) and
// debootstrap on PATH. Unlike Isolator's own podman-gated integration
// tests, this one is expected to genuinely run in this project's dev/CI
// environment — debootstrap-based bootstrapping was validated by hand
// against a real Ubuntu mirror during development, and this test exercises
// the same code path automatically.
func requireRootAndDebootstrap(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("not running as root — skipping real bootstrap integration test")
	}
	if _, err := exec.LookPath("debootstrap"); err != nil {
		t.Skip("debootstrap not on PATH — skipping real bootstrap integration test")
	}
}

// TestIntegration_RealDebootstrapAndPackage runs an actual (minimal, fast)
// debootstrap against a real mirror, then exercises the rootfs tarball
// packaging + checksum step against the real result — no mocking.
func TestIntegration_RealDebootstrapAndPackage(t *testing.T) {
	requireRootAndDebootstrap(t)

	dir := t.TempDir()
	rootfs := filepath.Join(dir, "rootfs")
	if err := os.MkdirAll(rootfs, 0755); err != nil {
		t.Fatalf("mkdir rootfs: %v", err)
	}

	spec := &BuildSpec{
		Name:   "builder-integration-test",
		Base:   "ubuntu",
		Suite:  "noble",
		Mirror: "http://archive.ubuntu.com/ubuntu",
	}

	if err := runDebootstrapBootstrap(spec, "", true, rootfs); err != nil {
		t.Fatalf("real debootstrap failed: %v", err)
	}

	// Sanity-check the result actually looks like a Linux base system.
	for _, mustExist := range []string{"bin", "etc", "usr"} {
		if _, err := os.Stat(filepath.Join(rootfs, mustExist)); err != nil {
			t.Errorf("expected %s to exist in bootstrapped rootfs: %v", mustExist, err)
		}
	}
	osRelease := filepath.Join(rootfs, "etc", "os-release")
	if data, err := os.ReadFile(osRelease); err != nil {
		t.Errorf("expected /etc/os-release in bootstrapped rootfs: %v", err)
	} else if !strings.Contains(string(data), "Ubuntu") {
		t.Errorf("expected Ubuntu in os-release, got: %s", data)
	}

	// Package it and verify the checksum sidecar is correct.
	outAbs := dir
	if err := packageRootfsFallback(spec, rootfs, outAbs); err != nil {
		t.Fatalf("packaging failed: %v", err)
	}
	tarPath := filepath.Join(outAbs, spec.Name+"-rootfs.tar.gz")
	sumPath := tarPath + ".sha256"

	if _, err := os.Stat(tarPath); err != nil {
		t.Fatalf("expected tarball at %s: %v", tarPath, err)
	}
	sumData, err := os.ReadFile(sumPath)
	if err != nil {
		t.Fatalf("expected checksum file at %s: %v", sumPath, err)
	}
	if !strings.Contains(string(sumData), filepath.Base(tarPath)) {
		t.Errorf("checksum file doesn't reference the tarball name: %s", sumData)
	}

	// Verify the checksum actually matches the file (not just present).
	cmd := exec.Command("sha256sum", "-c", filepath.Base(sumPath))
	cmd.Dir = outAbs
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("sha256sum -c failed: %v\n%s", err, out)
	}
}

// TestIntegration_EmbedIsolatorAndFirstBoot exercises the smaller
// post-bootstrap helpers against a fake (non-bootstrapped) rootfs
// directory — these don't need root or debootstrap, just a directory tree,
// so they always run.
func TestIntegration_EmbedIsolatorAndFirstBoot(t *testing.T) {
	dir := t.TempDir()
	rootfs := filepath.Join(dir, "rootfs")
	os.MkdirAll(rootfs, 0755)

	if err := writeHostname(rootfs, "test-host"); err != nil {
		t.Fatalf("writeHostname: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(rootfs, "etc", "hostname"))
	if err != nil || strings.TrimSpace(string(data)) != "test-host" {
		t.Errorf("expected hostname file with 'test-host', got %q (err=%v)", data, err)
	}

	if err := writeFirstBootUnit(rootfs); err != nil {
		t.Fatalf("writeFirstBootUnit: %v", err)
	}
	unitPath := filepath.Join(rootfs, "etc/systemd/system/isolator-first-boot.service")
	if _, err := os.Stat(unitPath); err != nil {
		t.Errorf("expected first-boot unit at %s: %v", unitPath, err)
	}
	linkPath := filepath.Join(rootfs, "etc/systemd/system/multi-user.target.wants/isolator-first-boot.service")
	if target, err := os.Readlink(linkPath); err != nil {
		t.Errorf("expected symlink at %s: %v", linkPath, err)
	} else if target != "../isolator-first-boot.service" {
		t.Errorf("unexpected symlink target: %s", target)
	}
}
