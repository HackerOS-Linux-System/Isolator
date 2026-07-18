package src

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/briandowns/spinner"
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

// ExecInContainerWithSpinner runs a command inside a container behind a
// progress spinner instead of streaming its raw output directly — the
// package-manager chatter from `apt-get install`/`dnf install`/`pacman -S`
// is mostly noise for a successful install, so it's captured and only
// dumped to the terminal if the command actually fails (then it's exactly
// the debugging info you'd want). Interactive/inspection uses (like
// `isolator exec`) should keep using ExecInContainer directly, which still
// streams live — hiding output behind a spinner only makes sense for
// "run this and tell me if it worked" steps like install/remove/init.
func ExecInContainerWithSpinner(cont string, cmdStr string, label string, asRoot bool) bool {
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = " " + label
	s.Color("cyan")
	s.Start()

	args := []string{"exec"}
	if asRoot {
		args = append(args, "-u", "0")
	}
	args = append(args, cont, "sh", "-c", cmdStr)
	cmd := exec.Command(podmanBin, args...)
	output, err := cmd.CombinedOutput()
	s.Stop()

	if err != nil {
		PrintError(label + " failed")
		if len(output) > 0 {
			fmt.Println(DimStyle.Render(string(output)))
		}
		return false
	}
	PrintSuccess(label)
	return true
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
