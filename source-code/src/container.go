package src

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/briandowns/spinner"
)

// CheckPodman verifies podman is available.
func CheckPodman() error {
	_, err := exec.LookPath(podmanBin)
	return err
}

// PullImage pulls image with a visible progress spinner, independent of
// whether stdout is a terminal. Podman's own `pull` progress bars are nice
// on an interactive TTY but vanish (or spam plain text) when output is
// redirected/logged/piped; this gives a consistent, predictable indicator
// either way. The container creation step afterward then uses
// `--pull missing` instead of `--pull always`, so this is a pure addition
// of feedback, not a behavior change — the net result (fresh image if
// needed, cached reuse otherwise) is the same as before.
func PullImage(image string) bool {
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = fmt.Sprintf(" Pulling image %s...", image)
	s.Color("cyan")
	s.Start()

	cmd := exec.Command(podmanBin, "pull", image)
	output, err := cmd.CombinedOutput()
	s.Stop()

	if err != nil {
		PrintError(fmt.Sprintf("Failed to pull image %s", image))
		if len(output) > 0 {
			fmt.Println(DimStyle.Render(string(output)))
		}
		return false
	}
	PrintSuccess(fmt.Sprintf("Image ready: %s", image))
	return true
}

// GetContainers returns list of all Podman containers (JSON).
func GetContainers() []ContainerInfo {
	cmd := exec.Command(podmanBin, "ps", "-a", "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var list []ContainerInfo
	if err := json.Unmarshal(out, &list); err != nil {
		return nil
	}
	return list
}

func ContainerExists(name string) bool {
	for _, c := range GetContainers() {
		for _, n := range c.Names {
			if n == name {
				return true
			}
		}
	}
	return false
}

func GetOurContainers() []string {
	var ours []string
	for _, c := range GetContainers() {
		for _, n := range c.Names {
			for _, base := range Containers {
				if n == base || strings.HasPrefix(n, base+"-") {
					ours = append(ours, n)
					break
				}
			}
		}
	}
	return ours
}

func GetContainerSize(name string) string {
	cmd := exec.Command(podmanBin, "ps", "-a", "--size", "--format", "json", "--filter", "name="+name)
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	var list []ContainerInfo
	if err := json.Unmarshal(out, &list); err != nil || len(list) == 0 {
		return "unknown"
	}
	return list[0].Size
}

// getPodmanRunArgs builds arguments for podman run -d.
// GUI/audio/GPU/theme/desktop-environment support is delegated to
// BuildGraphicsArgs (gui.go), which is driven by the user's config and by
// what's actually detected on the host, instead of blindly mounting
// everything for every container type.
func getPodmanRunArgs(name, image, homeDir, pkgType, initSystem string) []string {
	uid := os.Getuid()
	gid := os.Getgid()
	homeHost := homeDir
	if homeHost == "" {
		homeHost = os.Getenv("HOME")
	}
	args := []string{
		"run", "-d",
		"--name", name,
		"--hostname", name,
		"--pull", "missing",
		"--userns=keep-id",
		"--user", fmt.Sprintf("%d:%d", uid, gid),
		"--workdir", "/home/user",
		"--env", "HOME=/home/user",
		"--env", fmt.Sprintf("USER=%s", os.Getenv("USER")),
	}

	// Mount home directory
	args = append(args, "--volume", fmt.Sprintf("%s:/home/user:rw", homeHost))

	cfg := LoadConfig()
	args = append(args, BuildGraphicsArgs(graphicsContext{
		uid:        uid,
		gid:        gid,
		contName:   name,
		cfg:        cfg,
		pkgType:    pkgType,
		initSystem: initSystem,
	})...)

	// SELinux (if enabled) – may be needed for X11
	args = append(args, "--security-opt", "label=type:container_runtime_t")

	// BlackArch's official image requires a permissive seccomp profile —
	// several of its tools (raw sockets, ptrace-based tools, etc.) trip the
	// default Podman seccomp filter otherwise. See:
	// https://hub.docker.com/r/blackarchlinux/blackarch
	if strings.Contains(image, "blackarch") {
		args = append(args, "--security-opt", "seccomp=unconfined")
	}

	// Keep the container alive with a dummy command
	args = append(args, "--entrypoint", "/bin/sh")
	args = append(args, image, "-c", "while true; do sleep 1000; done")

	return args
}

// CreateContainer creates a Podman container and starts it with a persistent dummy command.
// Returns true on success, false otherwise.
func CreateContainer(name, image, homeDir, pkgType, initSystem string) bool {
	if !PullImage(image) {
		return false
	}
	args := getPodmanRunArgs(name, image, homeDir, pkgType, initSystem)
	PrintStep(fmt.Sprintf("Creating container %s (image: %s)...", name, image))
	if !ExecCommand(podmanBin, args) {
		// If run fails, try to remove any leftover container
		ExecCommand(podmanBin, []string{"rm", "--force", name})
		return false
	}
	PrintSuccess(fmt.Sprintf("Container '%s' created and started", name))
	return true
}

// EnsureContainerRunning starts the container if it is not already running.
// Note: if the container was created with the old method (without persistent command),
// it may exit immediately after start. This function will try to start it, but it's recommended
// to remove such containers and let them be recreated with the new method.
func EnsureContainerRunning(name string) bool {
	// Check container state
	cmd := exec.Command(podmanBin, "ps", "-a", "--filter", "name="+name, "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	var containers []ContainerInfo
	if err := json.Unmarshal(out, &containers); err != nil || len(containers) == 0 {
		return false
	}
	if containers[0].State == "running" {
		return true
	}
	// Start container
	PrintStep(fmt.Sprintf("Starting container %s...", name))
	return ExecCommand(podmanBin, []string{"start", name})
}

// InitContainer runs the initial package manager sync inside a fresh container.
func InitContainer(cont string, d Distro) bool {
	initCmd := d.Adapter.Init()
	return ExecInContainerWithSpinner(cont, initCmd, "Initializing package manager in container...", true)
}
