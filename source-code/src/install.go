package src

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func HandleInstall(pkg string, isolated bool) {
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
		PrintError(fmt.Sprintf("Package '%s' not found in repository", pkg))
		PrintInfo("Try: isolator search <term>")
		return
	}

	installed, err := LoadInstalled()
	if err != nil {
		PrintError("Failed to load installed packages")
		return
	}
	for _, ip := range installed {
		if ip.Pkg == pkg {
			PrintWarn(fmt.Sprintf("Package '%s' is already installed (container: %s)", pkg, ip.Cont))
			return
		}
	}

	d, ok := Distros[info.Distro]
	if !ok {
		PrintError("Unknown distro: " + info.Distro)
		return
	}

	PrintInfo(fmt.Sprintf("Installing %s  [distro: %s | type: %s]",
			      BoldStyle.Render(pkg), CyanStyle.Render(info.Distro), DimStyle.Render(info.Type)))

	contName := d.ContName
	homeDir := os.Getenv("HOME")
	if isolated {
		contName += "-" + pkg
		homeDir = filepath.Join(homeDir, homesDir, pkg)
		if err := os.MkdirAll(homeDir, 0700); err != nil {
			PrintError("Failed to create isolated home directory")
			return
		}
		PrintInfo(fmt.Sprintf("Isolated mode: home → %s", homeDir))
	}

	newContainer := false
	if !ContainerExists(contName) {
		if !CreateContainer(contName, d.Image, homeDir, info.Type) {
			PrintError(fmt.Sprintf("Failed to create container '%s'", contName))
			return
		}
		newContainer = true
	} else {
		PrintInfo(fmt.Sprintf("Reusing existing container '%s'", contName))
		if !EnsureContainerRunning(contName) {
			PrintError(fmt.Sprintf("Failed to start container '%s'", contName))
			return
		}
	}

	if newContainer {
		if !InitContainer(contName, d) {
			PrintWarn("Package manager init returned non-zero (may be OK for some distros)")
		}
	}

	packagesToInstall := []string{pkg}
	if len(info.Libs) > 0 {
		PrintInfo("Dependencies: " + strings.Join(info.Libs, ", "))
		packagesToInstall = append(info.Libs, packagesToInstall...)
	}

	installCmd := d.Adapter.Install() + " " + strings.Join(packagesToInstall, " ")
	PrintStep(fmt.Sprintf("Running: %s", DimStyle.Render(installCmd)))

	if !ExecInContainer(contName, installCmd, false, true) {
		PrintError("Installation failed")
		return
	}

	if !CreateWrapper(pkg, contName) {
		PrintError("Failed to create wrapper script in ~/.local/bin")
		return
	}

	installed = append(installed, InstalledPackage{
		Pkg:      pkg,
		Cont:     contName,
		Distro:   info.Distro,
		Type:     info.Type,
		Isolated: isolated,
	})
	if err := SaveInstalled(installed); err != nil {
		PrintError("Failed to save installed info")
		return
	}
	PrintSuccess(fmt.Sprintf("'%s' installed successfully → %s",
				 pkg, filepath.Join(os.Getenv("HOME"), ".local/bin", pkg)))
}
