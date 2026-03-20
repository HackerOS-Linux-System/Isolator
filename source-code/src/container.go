package src

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// CheckPodman verifies podman is available.
func CheckPodman() error {
	_, err := exec.LookPath(podmanBin)
	return err
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
// It adds mounts for GUI/DE when necessary and keeps the container alive.
func getPodmanRunArgs(name, image, homeDir, pkgType string) []string {
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
		"--pull", "always",
		"--userns=keep-id",
		"--user", fmt.Sprintf("%d:%d", uid, gid),
		"--workdir", "/home/user",
		"--env", "HOME=/home/user",
		"--env", fmt.Sprintf("USER=%s", os.Getenv("USER")),
	}

	// Mount home directory
	args = append(args, "--volume", fmt.Sprintf("%s:/home/user:rw", homeHost))

	// Always add GUI/DE mounts (safe for CLI as well)
	// X11
	if _, err := os.Stat("/tmp/.X11-unix"); err == nil {
		args = append(args, "--volume", "/tmp/.X11-unix:/tmp/.X11-unix:rw")
		if display := os.Getenv("DISPLAY"); display != "" {
			args = append(args, "--env", "DISPLAY="+display)
		}
	}
	// Wayland
	if _, err := os.Stat("/run/user/" + strconv.Itoa(uid) + "/wayland-0"); err == nil {
		args = append(args, "--volume", fmt.Sprintf("/run/user/%d:/run/user/%d:rw", uid, uid))
		args = append(args, "--env", "WAYLAND_DISPLAY=wayland-0")
	}
	// PulseAudio / PipeWire
	if _, err := os.Stat("/run/user/" + strconv.Itoa(uid) + "/pulse"); err == nil {
		args = append(args, "--volume", fmt.Sprintf("/run/user/%d/pulse:/run/user/%d/pulse:rw", uid, uid))
	}
	if _, err := os.Stat("/run/user/" + strconv.Itoa(uid) + "/pipewire-0"); err == nil {
		args = append(args, "--volume", fmt.Sprintf("/run/user/%d/pipewire-0:/run/user/%d/pipewire-0:rw", uid, uid))
	}
	// D-Bus
	if _, err := os.Stat("/run/user/" + strconv.Itoa(uid) + "/bus"); err == nil {
		args = append(args, "--env", fmt.Sprintf("DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid))
	}
	// GPU / DRI
	if _, err := os.Stat("/dev/dri"); err == nil {
		args = append(args, "--device", "/dev/dri:/dev/dri")
	}
	// SELinux (if enabled) – may be needed for X11
	args = append(args, "--security-opt", "label=type:container_runtime_t")

	// Keep the container alive with a dummy command
	args = append(args, "--entrypoint", "/bin/sh")
	args = append(args, image, "-c", "while true; do sleep 1000; done")

	return args
}

// CreateContainer creates a Podman container and starts it with a persistent dummy command.
// Returns true on success, false otherwise.
func CreateContainer(name, image, homeDir, pkgType string) bool {
	args := getPodmanRunArgs(name, image, homeDir, pkgType)
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
	PrintStep("Initializing package manager in container...")
	initCmd := d.Adapter.Init()
	return ExecInContainer(cont, initCmd, false, true)
}
