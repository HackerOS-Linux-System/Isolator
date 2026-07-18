package src

import (
	"fmt"
)

// HandleExec runs an arbitrary command inside the container that owns pkg,
// interactively. This is more flexible than the fixed wrapper script (which
// always execs the package binary itself) — e.g. `isolator exec firefox --
// bash` to get a shell for debugging, or to run a companion CLI tool that
// shipped in the same container.
func HandleExec(pkg string, cmdArgs []string) {
	if err := ValidatePackageName(pkg); err != nil {
		PrintError(err.Error())
		return
	}

	installed, err := LoadInstalled()
	if err != nil {
		PrintError("Failed to load installed packages")
		return
	}
	var ip *InstalledPackage
	for i := range installed {
		if installed[i].Pkg == pkg {
			ip = &installed[i]
			break
		}
	}
	if ip == nil {
		PrintError(fmt.Sprintf("Package '%s' is not installed", pkg))
		return
	}

	if !EnsureContainerRunning(ip.Cont) {
		PrintError(fmt.Sprintf("Failed to start container '%s'", ip.Cont))
		return
	}

	command := pkg
	if len(cmdArgs) > 0 {
		command = cmdArgs[0]
		cmdArgs = cmdArgs[1:]
	}

	args := []string{"exec", "-it", ip.Cont, command}
	args = append(args, cmdArgs...)
	if !ExecCommand(podmanBin, args) {
		PrintError("Command failed inside container")
	}
}
