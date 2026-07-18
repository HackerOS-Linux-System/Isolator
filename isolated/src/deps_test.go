package src

import "testing"

func TestClassifyLibs(t *testing.T) {
	repo := []PackageInfo{
		{Name: "boost", Distro: "debian", Type: "lib"},
		{Name: "myapp", Distro: "debian", Type: "gui", Libs: []string{"boost", "libnss3"}},
	}
	info := &repo[1]

	catalogLibs, rawLibs := ClassifyLibs(info, repo)
	if len(catalogLibs) != 1 || catalogLibs[0].Name != "boost" {
		t.Fatalf("expected catalogLibs=[boost], got %v", catalogLibs)
	}
	if len(rawLibs) != 1 || rawLibs[0] != "libnss3" {
		t.Fatalf("expected rawLibs=[libnss3], got %v", rawLibs)
	}
}

func TestReverseLibDependents(t *testing.T) {
	repo := []PackageInfo{
		{Name: "boost", Distro: "debian", Type: "lib"},
		{Name: "boost", Distro: "archlinux", Type: "lib"},
		{Name: "myapp", Distro: "debian", Type: "gui", Libs: []string{"boost"}},
		{Name: "otherapp", Distro: "archlinux", Type: "gui", Libs: []string{"boost"}},
		{Name: "thirdapp", Distro: "debian", Type: "cli", Libs: []string{"boost"}},
	}
	dependents := ReverseLibDependents("boost", "debian", repo)
	if len(dependents) != 2 {
		t.Fatalf("expected 2 debian dependents, got %v", dependents)
	}
	// archlinux's "boost" should not pick up the archlinux dependent when
	// asking about the debian one.
	for _, d := range dependents {
		if d == "otherapp" {
			t.Fatalf("cross-distro false positive: %v", dependents)
		}
	}
}

func TestResolveTransitiveLibs(t *testing.T) {
	repo := []PackageInfo{
		{Name: "zlib", Distro: "debian", Type: "lib"},
		{Name: "freetype", Distro: "debian", Type: "lib", Libs: []string{"zlib"}},
		{Name: "harfbuzz", Distro: "debian", Type: "lib", Libs: []string{"freetype"}},
		{Name: "myapp", Distro: "debian", Type: "gui", Libs: []string{"harfbuzz", "libnss3"}},
	}
	root := &repo[3]

	resolved, cycles, crossDistro := ResolveTransitiveLibs(root, repo)
	if len(cycles) != 0 {
		t.Fatalf("expected no cycles, got %v", cycles)
	}
	if len(crossDistro) != 0 {
		t.Fatalf("expected no cross-distro mismatches, got %v", crossDistro)
	}
	names := map[string]bool{}
	for _, r := range resolved {
		names[r.Name] = true
	}
	for _, want := range []string{"harfbuzz", "freetype", "zlib"} {
		if !names[want] {
			t.Errorf("expected %q in transitively resolved set, got %v", want, resolved)
		}
	}
	if names["libnss3"] {
		t.Errorf("libnss3 is a raw string, not a catalog lib — should not appear in resolved")
	}
}

func TestResolveTransitiveLibsDetectsCycle(t *testing.T) {
	repo := []PackageInfo{
		{Name: "a", Distro: "debian", Type: "lib", Libs: []string{"b"}},
		{Name: "b", Distro: "debian", Type: "lib", Libs: []string{"a"}},
		{Name: "myapp", Distro: "debian", Type: "gui", Libs: []string{"a"}},
	}
	root := &repo[2]

	_, cycles, _ := ResolveTransitiveLibs(root, repo)
	if len(cycles) == 0 {
		t.Fatalf("expected a cycle to be detected")
	}
}

func TestResolveTransitiveLibsDetectsCrossDistro(t *testing.T) {
	repo := []PackageInfo{
		{Name: "onlyarch", Distro: "archlinux", Type: "lib"},
		{Name: "wrapper", Distro: "debian", Type: "lib", Libs: []string{"onlyarch"}},
		{Name: "myapp", Distro: "debian", Type: "gui", Libs: []string{"wrapper"}},
	}
	root := &repo[2]

	_, _, crossDistro := ResolveTransitiveLibs(root, repo)
	if len(crossDistro) != 1 || crossDistro[0].Name != "onlyarch" {
		t.Fatalf("expected onlyarch to be flagged as cross-distro two hops deep, got %v", crossDistro)
	}
}
