package src

// ClassifyLibs splits a package's declared Libs into two groups:
//
//   - catalogLibs: entries that are themselves real catalog packages with
//     type == "lib" — Isolator knows about these as first-class dependency
//     objects (they show up in `isolator info`'s reverse lookup, and
//     `isolator remove` treats them as tracked dependencies rather than
//     opaque strings).
//   - rawLibs: everything else — plain distro package-manager names (like
//     "libnss3" on Debian) that aren't cataloged separately. These still
//     get installed exactly as before; they're just not "known" to
//     Isolator as independent objects.
//
// Both groups get installed identically (they're real package-manager names
// either way) — this function only affects what Isolator *understands*
// about the dependency, not what gets run.
func ClassifyLibs(info *PackageInfo, repoPackages []PackageInfo) (catalogLibs []PackageInfo, rawLibs []string) {
	byName := map[string]*PackageInfo{}
	for i := range repoPackages {
		if repoPackages[i].Type == "lib" {
			byName[repoPackages[i].Name] = &repoPackages[i]
		}
	}
	for _, libName := range info.Libs {
		if lib, ok := byName[libName]; ok {
			catalogLibs = append(catalogLibs, *lib)
		} else {
			rawLibs = append(rawLibs, libName)
		}
	}
	return catalogLibs, rawLibs
}

// (Single-level cross-distro checking used to live here as
// WarnCrossDistroLibs; it's superseded by the full-depth check built into
// ResolveTransitiveLibs below, which catches the same problem even when it
// shows up two or three hops into the dependency graph instead of the
// first.)

// ReverseLibDependents returns the names of every catalog package whose
// Libs list references libName, restricted to the same distro (a Debian
// package depending on a same-named Arch lib entry would be a false
// positive otherwise).
func ReverseLibDependents(libName, libDistro string, repoPackages []PackageInfo) []string {
	var dependents []string
	for _, p := range repoPackages {
		if p.Distro != libDistro {
			continue
		}
		for _, l := range p.Libs {
			if l == libName {
				dependents = append(dependents, p.Name)
				break
			}
		}
	}
	return dependents
}

// ResolveTransitiveLibs walks the full dependency graph starting from
// root's Libs, following catalog "lib" entries that themselves declare
// further Libs (a lib depending on another lib — e.g. a font-rendering lib
// pulling in freetype, which pulls in zlib), until no new libs are found.
//
// There's no version concept anywhere in the catalog (see types.go's
// PackageInfo — packages are just names), so "conflict" here can't mean a
// version clash the way it would in a real package manager. What it *can*
// still mean, and does: a transitively-required lib cataloged for a
// different distro than root — installing it verbatim into root's
// container would almost certainly fail, and checking only one level deep
// (as the original ClassifyLibs/WarnCrossDistroLibs did) misses it if the
// mismatch shows up two or three hops into the graph instead of the first.
//
// Returns the de-duplicated set of resolved catalog libs (discovery
// order), any dependency cycles found (each as the path that closed the
// loop), and any cross-distro mismatches found at any depth.
func ResolveTransitiveLibs(root *PackageInfo, repoPackages []PackageInfo) (resolved []PackageInfo, cycles [][]string, crossDistro []PackageInfo) {
	byName := map[string]*PackageInfo{}
	for i := range repoPackages {
		if repoPackages[i].Type == "lib" {
			byName[repoPackages[i].Name] = &repoPackages[i]
		}
	}

	visited := map[string]bool{}

	var visit func(name string, path []string)
	visit = func(name string, path []string) {
		for _, p := range path {
			if p == name {
				cycles = append(cycles, append(append([]string{}, path...), name))
				return
			}
		}
		lib, ok := byName[name]
		if !ok {
			return // a raw, non-cataloged lib string — nothing further to walk
		}
		if visited[name] {
			return
		}
		visited[name] = true

		if lib.Distro != root.Distro {
			crossDistro = append(crossDistro, *lib)
		}
		resolved = append(resolved, *lib)

		nextPath := append(append([]string{}, path...), name)
		for _, sub := range lib.Libs {
			visit(sub, nextPath)
		}
	}

	for _, l := range root.Libs {
		visit(l, []string{root.Name})
	}
	return resolved, cycles, crossDistro
}
