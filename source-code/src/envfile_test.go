package src

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dev.hk")
	content := `[environment]
-> name    => myproject
-> distro  => debian
-> shell   => bash

[packages]
-> python3
-> nodejs
-> git

[env]
-> DATABASE_URL => postgres://localhost/dev
-> NODE_ENV     => development
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	spec, err := ParseEnvFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Name != "myproject" {
		t.Errorf("expected name 'myproject', got %q", spec.Name)
	}
	if spec.Distro != "debian" {
		t.Errorf("expected distro 'debian', got %q", spec.Distro)
	}
	if len(spec.Packages) != 3 {
		t.Fatalf("expected 3 packages, got %d: %v", len(spec.Packages), spec.Packages)
	}
	if spec.EnvVars["NODE_ENV"] != "development" {
		t.Errorf("expected NODE_ENV=development, got %q", spec.EnvVars["NODE_ENV"])
	}
}

func TestParseEnvFileUnknownDistro(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.hk")
	content := `[environment]
-> name   => bad
-> distro => not-a-real-distro
`
	os.WriteFile(path, []byte(content), 0644)
	if _, err := ParseEnvFile(path); err == nil {
		t.Fatalf("expected error for unknown distro")
	}
}
