package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

type PackageInfo struct {
	Name   string   `json:"name"`
	Distro string   `json:"distro"`
	Type   string   `json:"type"`
	Libs   []string `json:"libs,omitempty"`
}

const (
	repoURL      = "https://raw.githubusercontent.com/HackerOS-Linux-System/Isolator/main/repo/package-list.json"
	lockFile     = ".config/isolator/lock"
	distroboxBin = "distrobox"
	tempRepoFile = "/tmp/package-list.json"
)

var containers = []string{"archlinux", "fedora", "debian-testing", "opensuse-tumbleweed", "ubuntu", "slackware"}
var repoPackages []PackageInfo

var (
	boldStyle    = lipgloss.NewStyle().Bold(true)
	errorStyle   = boldStyle.Copy().Foreground(lipgloss.Color("1")) // red
	successStyle = boldStyle.Copy().Foreground(lipgloss.Color("2")) // green
	infoStyle    = boldStyle.Copy().Foreground(lipgloss.Color("4")) // blue
	warnStyle    = boldStyle.Copy().Foreground(lipgloss.Color("3")) // yellow
	cyanStyle    = boldStyle.Copy().Foreground(lipgloss.Color("6")) // cyan
)

func printError(msg string) {
	fmt.Println(errorStyle.Render("Error: " + msg))
}

func printInfo(msg string) {
	fmt.Println(infoStyle.Render("Info: " + msg))
}

func printSuccess(msg string) {
	fmt.Println(successStyle.Render("Success: " + msg))
}

func execCommand(bin string, args []string) bool {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	return err == nil
}

func execInContainer(cont string, cmdStr string, useSudo bool) bool {
	args := []string{"enter", cont, "--"}
	if useSudo {
		args = append(args, "sudo", "-S", "sh", "-c", cmdStr)
	} else {
		args = append(args, "sh", "-c", cmdStr)
	}
	return execCommand(distroboxBin, args)
}

func loadRepo(force bool) bool {
	_, statErr := os.Stat(tempRepoFile)
	if !force && statErr == nil {
		// exists, no force
	} else {
		printInfo("Downloading repo list...")
		resp, err := http.Get(repoURL)
		if err != nil {
			printError(fmt.Sprintf("Failed to download repo list: %s", err.Error()))
			return false
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			printError(fmt.Sprintf("Failed to download: status %d", resp.StatusCode))
			return false
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			printError("Failed to read response body")
			return false
		}
		err = os.WriteFile(tempRepoFile, body, 0644)
		if err != nil {
			printError("Failed to write repo file")
			return false
		}
	}
	data, err := os.ReadFile(tempRepoFile)
	if err != nil {
		printError("Failed to read repo file")
		return false
	}
	err = json.Unmarshal(data, &repoPackages)
	if err != nil {
		printError("Failed to parse JSON")
		return false
	}
	return true
}

func getContainerName(distro string) string {
	switch distro {
	case "debian":
		return "debian-testing"
	case "fedora":
		return "fedora"
	case "archlinux":
		return "archlinux"
	case "opensuse":
		return "opensuse-tumbleweed"
	case "ubuntu":
		return "ubuntu"
	case "slackware":
		return "slackware"
	}
	return ""
}

func handleInit() {
	home := os.Getenv("HOME")
	if home == "" {
		printError("HOME environment variable not set")
		return
	}
	lockPath := filepath.Join(home, lockFile)
	if _, err := os.Stat(lockPath); err == nil {
		printError("Isolator already initialized (lock file exists)")
		return
	}
	printInfo("Initializing isolator...")
	uid := os.Getuid()
	for _, cont := range containers {
		distro := cont
		if cont == "debian-testing" {
			distro = "debian:testing"
		} else if cont == "opensuse-tumbleweed" {
			distro = "opensuse/tumbleweed"
		} else if cont == "ubuntu" {
			distro = "ubuntu"
		} else if cont == "slackware" {
			distro = "slackware64-current"
		}
		envFlag := fmt.Sprintf("--env=DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%s/bus", strconv.Itoa(uid))
		args := []string{"create", "--name", cont, "--image", distro, "--home", home, "--additional-flags", envFlag}
		if !execCommand(distroboxBin, args) {
			printError(fmt.Sprintf("Failed to create container: %s", cont))
			return
		}
		printSuccess(fmt.Sprintf("Created container: %s", cont))
	}
	err := os.WriteFile(lockPath, []byte("initialized"), 0644)
	if err != nil {
		printError("Failed to create lock file")
		return
	}
	printSuccess("Initialization complete")
}

func handleInstall(pkg string) {
	if !loadRepo(false) {
		return
	}
	var info *PackageInfo
	for i := range repoPackages {
		if repoPackages[i].Name == pkg {
			info = &repoPackages[i]
			break
		}
	}
	if info == nil {
		printError(fmt.Sprintf("Package not found: %s", pkg))
		return
	}
	printInfo(fmt.Sprintf("Installing %s from %s (%s)", pkg, info.Distro, info.Type))
	var installer string
	switch info.Distro {
	case "debian":
		installer = "apt install -y"
	case "fedora":
		installer = "dnf install -y"
	case "archlinux":
		installer = "pacman -S --noconfirm"
	case "opensuse":
		installer = "zypper install -y"
	case "ubuntu":
		installer = "apt install -y"
	case "slackware":
		installer = "slackpkg install"
	default:
		printError("Unknown distro")
		return
	}
	contName := getContainerName(info.Distro)
	if contName == "" {
		printError("Unknown distro")
		return
	}
	packagesToInstall := []string{pkg}
	if len(info.Libs) > 0 {
		printInfo(fmt.Sprintf("Also installing dependencies: %s", strings.Join(info.Libs, ", ")))
		packagesToInstall = append(info.Libs, pkg)
	}
	installCmd := fmt.Sprintf("%s %s", installer, strings.Join(packagesToInstall, " "))
	if !execInContainer(contName, installCmd, true) {
		printError("Installation failed")
		return
	}
	var exportCmd string
	if info.Type == "gui" {
		exportCmd = fmt.Sprintf("distrobox-export --app %s", pkg)
	} else if info.Type == "cli" {
		binPath := fmt.Sprintf("/usr/bin/%s", pkg)
		exportCmd = fmt.Sprintf("distrobox-export --bin %s --export-path ~/.local/bin", binPath)
	}
	if !execInContainer(contName, exportCmd, false) {
		printError("Export failed")
		return
	}
	printSuccess("Installation complete")
}

func handleRemove(pkg string) {
	if !loadRepo(false) {
		return
	}
	var info *PackageInfo
	for i := range repoPackages {
		if repoPackages[i].Name == pkg {
			info = &repoPackages[i]
			break
		}
	}
	if info == nil {
		printError(fmt.Sprintf("Package not found: %s", pkg))
		return
	}
	printInfo(fmt.Sprintf("Removing %s from %s", pkg, info.Distro))
	var remover string
	switch info.Distro {
	case "debian":
		remover = "apt remove -y"
	case "fedora":
		remover = "dnf remove -y"
	case "archlinux":
		remover = "pacman -R --noconfirm"
	case "opensuse":
		remover = "zypper remove -y"
	case "ubuntu":
		remover = "apt remove -y"
	case "slackware":
		remover = "slackpkg remove"
	default:
		printError("Unknown distro")
		return
	}
	contName := getContainerName(info.Distro)
	if contName == "" {
		printError("Unknown distro")
		return
	}
	// First delete export
	var deleteCmd string
	if info.Type == "gui" {
		deleteCmd = fmt.Sprintf("distrobox-export --delete --app %s", pkg)
	} else if info.Type == "cli" {
		binPath := fmt.Sprintf("/usr/bin/%s", pkg)
		deleteCmd = fmt.Sprintf("distrobox-export --delete --bin %s", binPath)
	}
	if !execInContainer(contName, deleteCmd, false) {
		printError("Delete export failed")
		return
	}
	// Then remove package
	removeCmd := fmt.Sprintf("%s %s", remover, pkg)
	if !execInContainer(contName, removeCmd, true) {
		printError("Removal failed")
		return
	}
	printSuccess("Removal complete")
}

func handleUpdate() {
	printInfo("Updating everything...")
	var wg sync.WaitGroup
	for _, cont := range containers {
		var updater string
		switch cont {
		case "debian-testing":
			updater = "apt update && apt upgrade -y"
		case "fedora":
			updater = "dnf update -y"
		case "archlinux":
			updater = "pacman -Syu --noconfirm"
		case "opensuse-tumbleweed":
			updater = "zypper dup -y"
		case "ubuntu":
			updater = "apt update && apt upgrade -y"
		case "slackware":
			updater = "slackpkg update && slackpkg upgrade-all"
		}
		wg.Add(1)
		go func(c string, u string) {
			defer wg.Done()
			execInContainer(c, u, true)
		}(cont, updater)
	}
	wg.Wait()
	printSuccess("Update complete")
}

func handleRefresh() {
	printInfo("Refreshing repositories...")
	loadRepo(true)
	printSuccess("Refresh complete")
}

func handleUpgrade() {
	printInfo("Upgrading all...")
	if _, err := os.Stat("/usr/bin/apt"); err == nil {
		printError("System apt found in /usr/bin/ - potential conflict")
		return
	}
	if _, err := os.Stat("/usr/lib/isolator/apt"); os.IsNotExist(err) {
		printError("Isolator apt not found in /usr/lib/isolator/apt")
		return
	}
	execCommand("sudo", []string{"/usr/lib/isolator/apt", "update"})
	execCommand("sudo", []string{"/usr/lib/isolator/apt", "upgrade", "-y"})
	handleUpdate()
	printSuccess("Upgrade complete")
}

func handleSearch(term string) {
	if !loadRepo(false) {
		return
	}
	printInfo(fmt.Sprintf("Searching for %s...", term))
	found := false
	for _, p := range repoPackages {
		if strings.Contains(p.Name, term) {
			fmt.Println(successStyle.Render(fmt.Sprintf("%s -> %s -> %s", p.Name, p.Distro, p.Type)))
			found = true
		}
	}
	if !found {
		printError("No packages found")
	}
}

func main() {
	var rootCmd = &cobra.Command{
		Use:   "isolator [args]",
		Short: cyanStyle.Render("Isolator CLI Tool"),
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	rootCmd.AddCommand(
		&cobra.Command{
			Use:   "init",
			Short: "Initialize isolator (create containers)",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				handleInit()
			},
		},
		&cobra.Command{
			Use:   "install ",
			Short: "Install a package",
			Args:  cobra.ExactArgs(1),
			Run: func(cmd *cobra.Command, args []string) {
				handleInstall(args[0])
			},
		},
		&cobra.Command{
			Use:   "remove ",
			Short: "Remove a package",
			Args:  cobra.ExactArgs(1),
			Run: func(cmd *cobra.Command, args []string) {
				handleRemove(args[0])
			},
		},
		&cobra.Command{
			Use:   "update",
			Short: "Update everything",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				handleUpdate()
			},
		},
		&cobra.Command{
			Use:   "refresh",
			Short: "Refresh repositories",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				handleRefresh()
			},
		},
		&cobra.Command{
			Use:   "upgrade",
			Short: "Upgrade all possible (with checks)",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				handleUpgrade()
			},
		},
		&cobra.Command{
			Use:   "search ",
			Short: "Search for a package",
			Args:  cobra.ExactArgs(1),
			Run: func(cmd *cobra.Command, args []string) {
				handleSearch(args[0])
			},
		},
	)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
