package src

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// findDependents returns the names of other installed packages, sharing
// pkg's container, whose recorded Requires (set at install time — see
// ClassifyLibs in deps.go) lists pkg. This reads directly from the
// persisted installed.hk state instead of re-deriving the same answer by
// re-scanning the repo's Libs lists on every removal — the repo lookup
// already happened once, at install time, and its result was saved.
//
// One consequence: a package installed before Requires existed (or whose
// only dependencies were raw, non-cataloged lib strings) won't show up as
// a recorded dependent even if it happens to still need pkg. That's a
// known, accepted trade-off for not re-deriving state that's supposed to
// be authoritative.
func findDependents(pkg string, cont string, installed []InstalledPackage) []string {
	var dependents []string
	for _, ip := range installed {
		if ip.Pkg == pkg || ip.Cont != cont {
			continue
		}
		for _, req := range ip.Requires {
			if req == pkg {
				dependents = append(dependents, ip.Pkg)
				break
			}
		}
	}
	return dependents
}

func HandleRemove(pkg string, force bool, dryRun bool) {
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
	var index int
	for i, p := range installed {
		if p.Pkg == pkg {
			ip = &installed[i]
			index = i
			break
		}
	}
	if ip == nil {
		PrintError(fmt.Sprintf("Package '%s' is not installed", pkg))
		return
	}

	if !LoadRepo(false) {
		return
	}

	repoPackages := ReadRepoPackages()
	var info *PackageInfo
	for i := range repoPackages {
		if repoPackages[i].Name == pkg {
			info = &repoPackages[i]
			break
		}
	}
	if info == nil {
		PrintWarn(fmt.Sprintf("Package '%s' not found in repo — removing wrapper only", pkg))
	}

	if !ip.Isolated {
		if dependents := findDependents(pkg, ip.Cont, installed); len(dependents) > 0 {
			PrintWarn(fmt.Sprintf("'%s' is a dependency of: %s (sharing container '%s')", pkg, strings.Join(dependents, ", "), ip.Cont))
			if !force {
				PrintError("Refusing to remove — pass --force to remove anyway and risk breaking those packages")
				return
			}
			PrintWarn("--force given, proceeding anyway")
		}
	}

	if dryRun {
		PrintInfo(fmt.Sprintf("[dry-run] Would remove '%s' from container '%s'", pkg, ip.Cont))
		fmt.Println("  - remove wrapper script: " + filepath.Join(os.Getenv("HOME"), ".local/bin", pkg))
		fmt.Println("  - remove .desktop launcher (if any)")
		if info != nil {
			packagesToRemove := []string{pkg}
			if len(info.Libs) > 0 {
				packagesToRemove = append(packagesToRemove, info.Libs...)
			}
			fmt.Println("  - run in container: " + Distros[ip.Distro].Adapter.Remove() + " " + strings.Join(packagesToRemove, " "))
		}
		if ip.Isolated {
			fmt.Println("  - remove isolated container: " + ip.Cont)
			fmt.Println("  - remove isolated home dir: " + filepath.Join(os.Getenv("HOME"), homesDir, pkg))
		}
		PrintInfo("[dry-run] No changes made")
		return
	}

	PrintInfo(fmt.Sprintf("Removing %s from container '%s'", BoldStyle.Render(pkg), ip.Cont))

	if !RemoveWrapper(pkg) {
		PrintError("Failed to remove wrapper script")
		return
	}
	RemoveDesktopEntry(pkg)
	if ip.Type == "system" {
		if d, ok := Distros[ip.Distro]; ok && d.InitSystem != "systemd" {
			RemoveRcdScript(ip.Cont, pkg)
		}
	}

	if info != nil {
		d, ok := Distros[ip.Distro]
		if !ok {
			PrintError("Unknown distro: " + ip.Distro)
			return
		}
		packagesToRemove := []string{pkg}
		if len(info.Libs) > 0 {
			PrintInfo("Also removing dependencies: " + strings.Join(info.Libs, ", "))
			packagesToRemove = append(packagesToRemove, info.Libs...)
		}
		removeCmd := d.Adapter.Remove() + " " + strings.Join(packagesToRemove, " ")
		if !ExecInContainerWithSpinner(ip.Cont, removeCmd, fmt.Sprintf("Removing %s from container...", pkg), true) {
			PrintError("Package removal in container failed")
			return
		}
	}

	if ip.Isolated {
		PrintStep(fmt.Sprintf("Removing isolated container '%s'...", ip.Cont))
		if !ExecCommand(podmanBin, []string{"rm", "--force", ip.Cont}) {
			PrintError("Failed to remove isolated container")
			return
		}
		isolatedHome := filepath.Join(os.Getenv("HOME"), homesDir, pkg)
		if err := os.RemoveAll(isolatedHome); err != nil {
			PrintWarn("Failed to remove isolated home dir: " + err.Error())
		}
	}

	installed = append(installed[:index], installed[index+1:]...)
	if err := SaveInstalled(installed); err != nil {
		PrintError("Failed to save installed info")
		return
	}
	PrintSuccess(fmt.Sprintf("'%s' removed", pkg))
}
