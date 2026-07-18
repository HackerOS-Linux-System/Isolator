package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"isolator-builder/internal/hk"
)

// BuildSpec is a fully-resolved build request: the .hk file's contents,
// merged with an optional live-build-inspired companion directory of the
// same base name sitting next to it.
type BuildSpec struct {
	Name          string
	Base          string // "debian" | "ubuntu" | "archlinux"
	Suite         string // e.g. "bookworm", "noble"
	Mirror        string
	KernelPackage string
	Hostname      string
	Packages      []string // merged: [packages] section + package-lists/*.list.chroot
	Hooks         []string // absolute paths to hooks/*.hook.chroot, sorted
	IncludesDir   string   // includes.chroot/ if present, else ""
	OutputFormat  string   // "rootfs" | "iso" | "qcow2"
	OutputPath    string
	SpecPath      string
	ConfigDir     string // the companion dir, if one was found
}

// ParseBuildSpec loads <path>.hk and, if a companion directory with the
// same base name exists next to it, merges in its live-build-style content:
//
//	myimage.hk
//	myimage/
//	  package-lists/*.list.chroot   -> merged into Packages
//	  hooks/*.hook.chroot           -> run inside the chroot, in sorted order
//	  includes.chroot/              -> copied verbatim into the built rootfs
func ParseBuildSpec(path string) (*BuildSpec, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	doc, err := hk.LoadHKFile(abs)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", abs, err)
	}
	if err := hk.ResolveInterpolations(doc); err != nil {
		return nil, err
	}

	distro := doc.Section("distro")
	kernel := doc.Section("kernel")
	pkgSec := doc.Section("packages")
	output := doc.Section("output")

	spec := &BuildSpec{
		SpecPath:      abs,
		Name:          get(distro, "name", strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))),
		Base:          get(distro, "base", "debian"),
		Suite:         get(distro, "suite", "bookworm"),
		Mirror:        get(distro, "mirror", ""),
		KernelPackage: get(kernel, "package", ""),
		Hostname:      get(output, "hostname", ""),
		OutputFormat:  get(output, "format", "rootfs"),
		OutputPath:    get(output, "path", "./build-output"),
	}
	if spec.Mirror == "" {
		spec.Mirror = defaultMirror(spec.Base)
	}
	if spec.Hostname == "" {
		spec.Hostname = spec.Name
	}
	if spec.KernelPackage == "" {
		spec.KernelPackage = defaultKernelPackage(spec.Base)
	}

	for _, k := range pkgSec.Keys() {
		spec.Packages = append(spec.Packages, k)
	}

	// Live-build-inspired companion directory: same base name as the spec
	// file, sitting right next to it.
	configDir := strings.TrimSuffix(abs, filepath.Ext(abs))
	if info, err := os.Stat(configDir); err == nil && info.IsDir() {
		spec.ConfigDir = configDir
		if err := mergeConfigDir(spec, configDir); err != nil {
			return nil, err
		}
	}

	if err := validateSpec(spec); err != nil {
		return nil, err
	}
	return spec, nil
}

func get(m *hk.HkMap, key, def string) string {
	v, ok := m.Get(key)
	if !ok {
		return def
	}
	s, err := v.AsString()
	if err != nil {
		return def
	}
	return s
}

func defaultMirror(base string) string {
	switch base {
	case "ubuntu":
		return "http://archive.ubuntu.com/ubuntu"
	case "debian":
		return "http://deb.debian.org/debian"
	default:
		return ""
	}
}

func defaultKernelPackage(base string) string {
	switch base {
	case "ubuntu":
		return "linux-image-generic"
	case "debian":
		return "linux-image-amd64"
	case "archlinux":
		return "linux"
	default:
		return ""
	}
}

// mergeConfigDir folds a live-build-style config/ directory into spec:
//
//	package-lists/*.list.chroot  — one package name per line, '#' comments,
//	                                blank lines ignored (real live-build
//	                                convention).
//	hooks/*.hook.chroot           — shell scripts, executed inside the
//	                                chroot after packages are installed,
//	                                in lexical filename order (matches
//	                                live-build's own hook ordering rule).
//	includes.chroot/              — copied verbatim into the built rootfs,
//	                                preserving the relative path structure.
func mergeConfigDir(spec *BuildSpec, configDir string) error {
	listsDir := filepath.Join(configDir, "package-lists")
	if entries, err := os.ReadDir(listsDir); err == nil {
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".list.chroot") {
				continue
			}
			pkgs, err := readPackageList(filepath.Join(listsDir, e.Name()))
			if err != nil {
				return fmt.Errorf("reading %s: %w", e.Name(), err)
			}
			spec.Packages = append(spec.Packages, pkgs...)
		}
	}

	hooksDir := filepath.Join(configDir, "hooks")
	if entries, err := os.ReadDir(hooksDir); err == nil {
		var hooks []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".hook.chroot") {
				hooks = append(hooks, filepath.Join(hooksDir, e.Name()))
			}
		}
		sort.Strings(hooks)
		spec.Hooks = hooks
	}

	includesDir := filepath.Join(configDir, "includes.chroot")
	if info, err := os.Stat(includesDir); err == nil && info.IsDir() {
		spec.IncludesDir = includesDir
	}

	return nil
}

func readPackageList(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkgs []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pkgs = append(pkgs, line)
	}
	return pkgs, nil
}

func validateSpec(spec *BuildSpec) error {
	switch spec.Base {
	case "debian", "ubuntu", "archlinux":
	default:
		return fmt.Errorf("[distro] -> base: unsupported base %q (supported: debian, ubuntu, archlinux)", spec.Base)
	}
	switch spec.OutputFormat {
	case "rootfs", "iso", "qcow2":
	default:
		return fmt.Errorf("[output] -> format: unsupported format %q (supported: rootfs, iso, qcow2)", spec.OutputFormat)
	}
	return nil
}
