package src

type PackageInfo struct {
	Name   string   `json:"name"`
	Distro string   `json:"distro"`
	Type   string   `json:"type"` // "cli", "gui", "de"
	Libs   []string `json:"libs,omitempty"`
}

type InstalledPackage struct {
	Pkg      string `json:"pkg"`
	Cont     string `json:"cont"`
	Distro   string `json:"distro"`
	Type     string `json:"type"`
	Isolated bool   `json:"isolated"`
}

type ContainerInfo struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
	Size   string   `json:"Size"` // w formacie "123MB (virtual 456MB)"
}
