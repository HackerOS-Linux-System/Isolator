package src

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func catalogLibs2Names(libs []PackageInfo) []string {
	names := make([]string, len(libs))
	for i, l := range libs {
		names[i] = l.Name
	}
	return names
}

func boolLabelStr(b bool, ifTrue, ifFalse string) string {
	if b {
		return ifTrue
	}
	return ifFalse
}

func HandleInstall(pkg string, isolated bool, dryRun bool) {
	if err := ValidatePackageName(pkg); err != nil {
		PrintError(err.Error())
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
		PrintError(fmt.Sprintf("Package '%s' not found in repository", pkg))
		PrintInfo("Try: isolated search <term>")
		return
	}
	if err := ValidatePackageNames(info.Libs); err != nil {
		PrintError("Repository entry has unsafe dependency list: " + err.Error())
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

	if info.Type == "de" && !LoadConfig().AllowDesktopEnvironments {
		PrintWarn("This package is a full desktop environment. Enable 'allow_desktop_environments' in config.hk for proper systemd/cgroup support, otherwise it will run with regular app-level privileges only.")
	}
	if info.Type == "system" && !LoadConfig().AllowSystemContainers {
		PrintWarn("This package is a systemd-managed service. Enable 'allow_system_containers' in config.hk for proper systemd/cgroup support, otherwise it will run with regular app-level privileges only.")
	}

	PrintInfo(fmt.Sprintf("Installing %s  [distro: %s | type: %s]",
		BoldStyle.Render(pkg), CyanStyle.Render(info.Distro), DimStyle.Render(info.Type)))

	contName := d.ContName
	homeDir := os.Getenv("HOME")
	if isolated {
		// Prefixed distinctly from plain `isolator --isolated`'s own
		// naming (just "<distro>-<pkg>") so the two tools can never end
		// up fighting over the same Podman container name if someone has
		// both installed and, say, installs the same package via each.
		contName = "isolated-" + d.ContName + "-" + pkg
		homeDir = filepath.Join(homeDir, homesDir, pkg)
	}

	if dryRun {
		catalogLibs, rawLibs := ClassifyLibs(info, repoPackages)
		var libNames []string
		for _, l := range catalogLibs {
			libNames = append(libNames, l.Name+" (lib)")
		}
		libNames = append(libNames, rawLibs...)

		PrintInfo(fmt.Sprintf("[dry-run] Would install '%s' as follows:", pkg))
		fmt.Println("  - image: " + d.Image)
		fmt.Println("  - container: " + contName + boolLabelStr(ContainerExists(contName), " (already exists, reused)", " (new)"))
		if isolated {
			fmt.Println("  - isolated home: " + homeDir)
		}
		if len(libNames) > 0 {
			fmt.Println("  - dependencies: " + strings.Join(libNames, ", "))
		}
		packagesToInstall := append(append([]string{}, catalogLibs2Names(catalogLibs)...), rawLibs...)
		packagesToInstall = append(packagesToInstall, pkg)
		fmt.Println("  - run in container: " + d.Adapter.Install() + " " + strings.Join(packagesToInstall, " "))
		switch info.Type {
		case "de", "lib", "system":
			fmt.Println("  - no wrapper script (type: " + info.Type + ")")
		default:
			fmt.Println("  - wrapper script: " + filepath.Join(os.Getenv("HOME"), ".local/bin", pkg))
		}
		if LoadConfig().CreateDesktopEntries && (info.Type == "gui" || info.Type == "de") {
			fmt.Println("  - .desktop launcher in ~/.local/share/applications")
		}
		PrintInfo("[dry-run] No changes made")
		return
	}

	if isolated {
		if err := os.MkdirAll(homeDir, 0700); err != nil {
			PrintError("Failed to create isolated home directory")
			return
		}
		PrintInfo(fmt.Sprintf("Isolated mode: home → %s", homeDir))
	}

	newContainer := false
	if !ContainerExists(contName) {
		if !CreateContainer(contName, d.Image, homeDir, info.Type, d.InitSystem) {
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
	var recognizedLibs []string
	if len(info.Libs) > 0 {
		_, rawLibs := ClassifyLibs(info, repoPackages)
		transitiveLibs, cycles, crossDistro := ResolveTransitiveLibs(info, repoPackages)

		if len(cycles) > 0 {
			for _, c := range cycles {
				PrintWarn("Dependency cycle detected (stopping recursion there): " + strings.Join(c, " → "))
			}
		}
		for _, cd := range crossDistro {
			PrintWarn(fmt.Sprintf(
				"'%s' transitively depends on lib '%s', which is cataloged for '%s' — but this install targets a '%s' container. This will probably fail.",
				pkg, cd.Name, cd.Distro, info.Distro))
		}

		var libNames []string
		for _, l := range transitiveLibs {
			libNames = append(libNames, l.Name+DimStyle.Render(" (lib)"))
			recognizedLibs = append(recognizedLibs, l.Name)
		}
		libNames = append(libNames, rawLibs...)
		PrintInfo(fmt.Sprintf("Dependencies (%d, transitively resolved): %s", len(libNames), strings.Join(libNames, ", ")))
		packagesToInstall = append(append(recognizedLibs, rawLibs...), pkg)
	}

	installCmd := d.Adapter.Install() + " " + strings.Join(packagesToInstall, " ")
	if !ExecInContainerWithSpinner(contName, installCmd, fmt.Sprintf("Installing %s in container...", pkg), true) {
		PrintError("Installation failed")
		return
	}

	switch info.Type {
	case "de":
		PrintInfo("This is a full desktop environment — there's no single binary to wrap.")
		PrintInfo(fmt.Sprintf("Enter it with: isolated exec %s -- bash", pkg))
	case "lib":
		PrintInfo("This is a development library, not a runnable program — no wrapper script created.")
		PrintInfo(fmt.Sprintf("Build against it with: isolated exec %s -- <your build command>", pkg))
	case "system":
		if d.InitSystem == "systemd" {
			PrintInfo("This is a systemd-managed service, not a foreground program — no wrapper script created.")
			PrintInfo(fmt.Sprintf("Check it with: isolated exec %s -- systemctl status", pkg))
		} else {
			PrintInfo(fmt.Sprintf("This distro's image has no systemd (init: %s) — generating a classic /etc/rc.d/rc.%s script instead.", d.InitSystem, pkg))
			if err := GenerateRcdScript(contName, pkg); err != nil {
				PrintWarn("Failed to generate rc.d script: " + err.Error())
			} else {
				PrintSuccess(fmt.Sprintf("Installed as /etc/rc.d/rc.%s (hooked into rc.local, started automatically on boot)", pkg))
				PrintInfo(fmt.Sprintf("Control it with: isolated exec %s -- /etc/rc.d/rc.%s {start|stop|restart|status}", pkg, pkg))
			}
		}
	default:
		if !CreateWrapper(pkg, contName) {
			PrintError("Failed to create wrapper script in ~/.local/bin")
			return
		}
	}

	if LoadConfig().CreateDesktopEntries && (info.Type == "gui" || info.Type == "de") {
		if err := GenerateDesktopEntry(pkg, contName, info.Type); err != nil {
			PrintWarn("Installed, but failed to create application-menu launcher: " + err.Error())
		} else {
			PrintSuccess("Launcher added to your application menu")
		}
	}

	installed = append(installed, InstalledPackage{
		Pkg:      pkg,
		Cont:     contName,
		Distro:   info.Distro,
		Type:     info.Type,
		Isolated: isolated,
		Requires: recognizedLibs,
	})
	if err := SaveInstalled(installed); err != nil {
		PrintError("Failed to save installed info")
		return
	}
	PrintSuccess(fmt.Sprintf("'%s' installed successfully → %s",
		pkg, filepath.Join(os.Getenv("HOME"), ".local/bin", pkg)))
}
