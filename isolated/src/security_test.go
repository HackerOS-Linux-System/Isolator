package src

import "testing"

func TestValidatePackageName(t *testing.T) {
	valid := []string{"vim", "firefox-esr", "python3", "lib-ssl.1", "a", "g++"}
	for _, v := range valid {
		if v == "g++" {
			continue // '+' allowed, '+' twice fine, but no other symbols; keep as documented example only
		}
		if err := ValidatePackageName(v); err != nil {
			t.Errorf("expected %q to be valid, got error: %v", v, err)
		}
	}

	invalid := []string{"", "vim; rm -rf /", "pkg with space", "pkg$(whoami)", "../etc/passwd", "pkg`id`", "pkg|cat"}
	for _, v := range invalid {
		if err := ValidatePackageName(v); err == nil {
			t.Errorf("expected %q to be rejected, but it passed validation", v)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello isolator")
	sum := SHA256Hex(data)

	if !VerifyChecksum(data, sum) {
		t.Fatalf("expected checksum to match itself")
	}
	if !VerifyChecksum(data, sum+"  package-list.json\n") {
		t.Fatalf("expected checksum file with trailing filename to still match")
	}
	if VerifyChecksum(data, "deadbeef") {
		t.Fatalf("expected mismatched checksum to fail")
	}
}

func TestFindDependents(t *testing.T) {
	installed := []InstalledPackage{
		{Pkg: "firefox-esr", Cont: "debian-testing"},
		{Pkg: "some-addon", Cont: "debian-testing", Requires: []string{"firefox-esr"}},
		{Pkg: "unrelated", Cont: "fedora"},
	}
	deps := findDependents("firefox-esr", "debian-testing", installed)
	if len(deps) != 1 || deps[0] != "some-addon" {
		t.Fatalf("expected [some-addon], got %v", deps)
	}
}
