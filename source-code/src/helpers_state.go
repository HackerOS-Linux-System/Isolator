package src

import (
	"os"
)

// Installed packages are stored in installed.hk as:
//
//   [packages]
//   -> firefox-esr
//   --> container => debian-testing
//   --> distro    => debian
//   --> type      => gui
//   --> isolated  => false
//
// Each package name is its own inline submap — a natural fit for .hk's
// "mapa inline" nesting, and it keeps the file diff-friendly (one package
// changing only touches its own four lines).

func LoadInstalled() ([]InstalledPackage, error) {
	if err := EnsureConfigDir(); err != nil {
		return nil, err
	}
	path := ConfigPath(installedFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return []InstalledPackage{}, nil
	}
	doc, err := LoadHKFile(path)
	if err != nil {
		return nil, err
	}
	pkgs := doc.Section("packages")
	var installed []InstalledPackage
	for _, name := range pkgs.Keys() {
		v, _ := pkgs.Get(name)
		if v.Kind != HkMapKind {
			continue
		}
		m := v.MapVal
		var requires []string
		if rv, ok := m.Get("requires"); ok {
			if arr, err := rv.AsArray(); err == nil {
				for _, item := range arr {
					if s, err := item.AsString(); err == nil {
						requires = append(requires, s)
					}
				}
			}
		}
		installed = append(installed, InstalledPackage{
			Pkg:      name,
			Cont:     hkGetString(m, "container", ""),
			Distro:   hkGetString(m, "distro", ""),
			Type:     hkGetString(m, "type", "cli"),
			Isolated: hkGetBool(m, "isolated", false),
			Requires: requires,
		})
	}
	return installed, nil
}

func SaveInstalled(installed []InstalledPackage) error {
	doc := NewHkDocument()
	pkgs := doc.Section("packages")
	for _, ip := range installed {
		m := NewHkMap()
		m.Set("container", hkStr(ip.Cont))
		m.Set("distro", hkStr(ip.Distro))
		m.Set("type", hkStr(ip.Type))
		m.Set("isolated", hkBoolV(ip.Isolated))
		if len(ip.Requires) > 0 {
			arr := make([]HkValue, len(ip.Requires))
			for i, r := range ip.Requires {
				arr[i] = hkStr(r)
			}
			m.Set("requires", HkValue{Kind: HkArray, Arr: arr})
		}
		pkgs.Set(ip.Pkg, HkValue{Kind: HkMapKind, MapVal: m})
	}
	return WriteHKFile(ConfigPath(installedFile), doc)
}
