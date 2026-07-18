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

// BlackArchAdapter reuses pacman semantics 1:1 — BlackArch is Arch Linux
// with ~2600 extra security-tool packages layered on top via an additional
// pacman repo that's already configured in the official
// blackarchlinux/blackarch image, so no separate command set is needed.
type BlackArchAdapter struct{ ArchAdapter }

type Distro struct {
	ContName string
	Image    string
	Adapter  DistroAdapter
	// InitSystem tells Isolator whether this distro's container image
	// actually boots systemd as PID 1. "de" and "system" packages request
	// --systemd=always + cgroup access; that flag is meaningless (and
	// actively misleading) on a distro whose default image has no systemd
	// at all — Slackware being the notable example, which traditionally
	// ships BSD-style /etc/rc.d init scripts instead.
	InitSystem string // "systemd" | "sysvinit" | "none"
}

var Distros = map[string]Distro{
	"debian":    {"debian-testing", "debian:testing", DebianAdapter{}, "systemd"},
	"fedora":    {"fedora", "registry.fedoraproject.org/fedora:latest", FedoraAdapter{}, "systemd"},
	"archlinux": {"archlinux", "archlinux:latest", ArchAdapter{}, "systemd"},
	"opensuse":  {"opensuse-tumbleweed", "registry.opensuse.org/opensuse/tumbleweed:latest", OpenSUSEAdapter{}, "systemd"},
	"ubuntu":    {"ubuntu", "ubuntu:latest", UbuntuAdapter{}, "systemd"},
	// The official slackware64-current image has no systemd — it boots
	// (if it boots an init at all, since containers normally skip PID 1
	// init entirely) via the classic BSD-style /etc/rc.d scripts. "de"
	// and "system" packages targeting this distro get a clear warning
	// instead of a silently-ignored --systemd=always flag.
	"slackware": {"slackware", "slackware64-current", SlackwareAdapter{}, "sysvinit"},

	// BlackArch: official image, ~2600 pentest/cybersec tools available via
	// pacman once installed. Needs --security-opt seccomp=unconfined,
	// which container.go adds automatically for this distro (see
	// needsSeccompUnconfined in container.go).
	"blackarch": {"blackarch", "blackarchlinux/blackarch:latest", BlackArchAdapter{}, "systemd"},
}

var Containers []string

func init() {
	for _, d := range Distros {
		Containers = append(Containers, d.ContName)
	}
}
