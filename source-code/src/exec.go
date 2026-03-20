package src

import (
	"os"
	"os/exec"
	"strings"
)

func ExecCommand(bin string, args []string) bool {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run() == nil
}

// ExecInContainer runs a command inside a container.
// If asRoot is true, the command is executed as root (UID 0) inside the container.
// Otherwise, it runs as the default user (the one mapped via --userns=keep-id).
// interactive flag adds -it for interactive commands.
func ExecInContainer(cont string, cmdStr string, interactive bool, asRoot bool) bool {
	args := []string{"exec"}
	if interactive {
		args = append(args, "-it")
	}
	if asRoot {
		args = append(args, "-u", "0")
	}
	args = append(args, cont, "sh", "-c", cmdStr)
	return ExecCommand(podmanBin, args)
}

// ExecInContainerWithOutput runs a command and captures output.
// If asRoot is true, runs as root.
func ExecInContainerWithOutput(cont string, cmdStr string, asRoot bool) (string, bool) {
	args := []string{"exec"}
	if asRoot {
		args = append(args, "-u", "0")
	}
	args = append(args, cont, "sh", "-c", cmdStr)
	cmd := exec.Command(podmanBin, args...)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err == nil
}
