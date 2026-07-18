package src

type PackageInfo struct {
	Name   string   `json:"name"`
	Distro string   `json:"distro"`
	Type   string   `json:"type"` // "cli", "gui", "de", "lib", "system"
	Libs   []string `json:"libs,omitempty"`
}

type InstalledPackage struct {
	Pkg      string `json:"pkg"`
	Cont     string `json:"cont"`
	Distro   string `json:"distro"`
	Type     string `json:"type"`
	Isolated bool   `json:"isolated"`
	// Requires holds the names of Libs entries that were recognized as
	// first-class catalog packages (type == "lib") at install time — see
	// ClassifyLibs in deps.go. Plain raw distro-package-manager lib names
	// aren't tracked here since Isolator has no independent object for them.
	Requires []string `json:"requires,omitempty"`
}

type ContainerInfo struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
	Size   string   `json:"Size"` // w formacie "123MB (virtual 456MB)"
}
