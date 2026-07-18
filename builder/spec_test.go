package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestSpec(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "mini.hk")
	content := `[distro]
-> name   => hackeros-mini
-> base   => debian
-> suite  => bookworm

[kernel]
-> package => linux-image-amd64

[packages]
-> curl
-> vim

[output]
-> format => rootfs
-> path   => ./out
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write spec: %v", err)
	}
	return path
}

func TestParseBuildSpecBasic(t *testing.T) {
	dir := t.TempDir()
	path := writeTestSpec(t, dir)

	spec, err := ParseBuildSpec(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "hackeros-mini" {
		t.Errorf("expected name 'hackeros-mini', got %q", spec.Name)
	}
	if spec.Base != "debian" {
		t.Errorf("expected base 'debian', got %q", spec.Base)
	}
	if spec.Mirror != "http://deb.debian.org/debian" {
		t.Errorf("expected default debian mirror, got %q", spec.Mirror)
	}
	if len(spec.Packages) != 2 {
		t.Errorf("expected 2 packages, got %v", spec.Packages)
	}
	if spec.ConfigDir != "" {
		t.Errorf("expected no companion dir, got %q", spec.ConfigDir)
	}
}

func TestParseBuildSpecWithConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := writeTestSpec(t, dir)

	configDir := filepath.Join(dir, "mini")
	os.MkdirAll(filepath.Join(configDir, "package-lists"), 0755)
	os.MkdirAll(filepath.Join(configDir, "hooks"), 0755)
	os.MkdirAll(filepath.Join(configDir, "includes.chroot", "etc"), 0755)

	os.WriteFile(filepath.Join(configDir, "package-lists", "extra.list.chroot"),
		[]byte("htop\n# a comment\n\ntmux\n"), 0644)
	os.WriteFile(filepath.Join(configDir, "hooks", "0010-a.hook.chroot"), []byte("#!/bin/sh\necho a\n"), 0755)
	os.WriteFile(filepath.Join(configDir, "hooks", "0005-b.hook.chroot"), []byte("#!/bin/sh\necho b\n"), 0755)
	os.WriteFile(filepath.Join(configDir, "includes.chroot", "etc", "motd"), []byte("hi\n"), 0644)

	spec, err := ParseBuildSpec(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.ConfigDir == "" {
		t.Fatalf("expected companion dir to be detected")
	}

	found := map[string]bool{}
	for _, p := range spec.Packages {
		found[p] = true
	}
	for _, want := range []string{"curl", "vim", "htop", "tmux"} {
		if !found[want] {
			t.Errorf("expected package %q in merged list, got %v", want, spec.Packages)
		}
	}

	if len(spec.Hooks) != 2 {
		t.Fatalf("expected 2 hooks, got %d", len(spec.Hooks))
	}
	// hooks must be sorted by filename: 0005-b before 0010-a
	if filepath.Base(spec.Hooks[0]) != "0005-b.hook.chroot" {
		t.Errorf("expected hooks sorted, first was %q", filepath.Base(spec.Hooks[0]))
	}

	if spec.IncludesDir == "" {
		t.Errorf("expected includes.chroot to be detected")
	}
}

func TestValidateSpecRejectsUnsupportedBase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.hk")
	os.WriteFile(path, []byte("[distro]\n-> base => gentoo\n"), 0644)
	if _, err := ParseBuildSpec(path); err == nil {
		t.Fatalf("expected error for unsupported base")
	}
}

func TestValidateSpecRejectsUnsupportedFormat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.hk")
	os.WriteFile(path, []byte("[distro]\n-> base => debian\n\n[output]\n-> format => vmdk\n"), 0644)
	if _, err := ParseBuildSpec(path); err == nil {
		t.Fatalf("expected error for unsupported output format")
	}
}
