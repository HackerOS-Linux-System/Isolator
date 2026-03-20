package src

type DistroAdapter interface {
	Install() string
	Remove() string
	Update() string
	Init() string // initial setup command after container creation
}

type DebianAdapter struct{}

func (DebianAdapter) Install() string { return "apt-get install -y" }
func (DebianAdapter) Remove() string  { return "apt-get remove -y" }
func (DebianAdapter) Update() string  { return "apt-get update && apt-get upgrade -y" }
func (DebianAdapter) Init() string    { return "apt-get update" }

type FedoraAdapter struct{}

func (FedoraAdapter) Install() string { return "dnf install -y" }
func (FedoraAdapter) Remove() string  { return "dnf remove -y" }
func (FedoraAdapter) Update() string  { return "dnf update -y" }
func (FedoraAdapter) Init() string    { return "dnf check-update; true" }

type ArchAdapter struct{}

func (ArchAdapter) Install() string { return "pacman -S --noconfirm" }
func (ArchAdapter) Remove() string  { return "pacman -R --noconfirm" }
func (ArchAdapter) Update() string  { return "pacman -Syu --noconfirm" }
func (ArchAdapter) Init() string    { return "pacman -Sy" }

type OpenSUSEAdapter struct{}

func (OpenSUSEAdapter) Install() string { return "zypper install -y" }
func (OpenSUSEAdapter) Remove() string  { return "zypper remove -y" }
func (OpenSUSEAdapter) Update() string  { return "zypper dup -y" }
func (OpenSUSEAdapter) Init() string    { return "zypper refresh" }

type UbuntuAdapter struct{}

func (UbuntuAdapter) Install() string { return "apt-get install -y" }
func (UbuntuAdapter) Remove() string  { return "apt-get remove -y" }
func (UbuntuAdapter) Update() string  { return "apt-get update && apt-get upgrade -y" }
func (UbuntuAdapter) Init() string    { return "apt-get update" }

type SlackwareAdapter struct{}

func (SlackwareAdapter) Install() string { return "slackpkg install" }
func (SlackwareAdapter) Remove() string  { return "slackpkg remove" }
func (SlackwareAdapter) Update() string  { return "slackpkg update && slackpkg upgrade-all" }
func (SlackwareAdapter) Init() string    { return "slackpkg update" }

type Distro struct {
	ContName string
	Image    string
	Adapter  DistroAdapter
}

var Distros = map[string]Distro{
	"debian":    {"debian-testing", "debian:testing", DebianAdapter{}},
	"fedora":    {"fedora", "registry.fedoraproject.org/fedora:latest", FedoraAdapter{}},
	"archlinux": {"archlinux", "archlinux:latest", ArchAdapter{}},
	"opensuse":  {"opensuse-tumbleweed", "registry.opensuse.org/opensuse/tumbleweed:latest", OpenSUSEAdapter{}},
	"ubuntu":    {"ubuntu", "ubuntu:latest", UbuntuAdapter{}},
	"slackware": {"slackware", "slackware64-current", SlackwareAdapter{}},
}

var Containers []string

func init() {
	for _, d := range Distros {
		Containers = append(Containers, d.ContName)
	}
}
