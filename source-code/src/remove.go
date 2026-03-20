package src

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func HandleRemove(pkg string) {
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

	PrintInfo(fmt.Sprintf("Removing %s from container '%s'", BoldStyle.Render(pkg), ip.Cont))

	if !RemoveWrapper(pkg) {
		PrintError("Failed to remove wrapper script")
		return
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
		if !ExecInContainer(ip.Cont, removeCmd, false, true) {
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
