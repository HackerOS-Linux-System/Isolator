package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/briandowns/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

// ─── Structs ─────────────────────────────────────────────────────────────────

type PackageInfo struct {
	Name   string   `json:"name"`
	Distro string   `json:"distro"`
	Type   string   `json:"type"`
	Libs   []string `json:"libs,omitempty"`
}

type InstalledPackage struct {
	Pkg      string `json:"pkg"`
	Cont     string `json:"cont"`
	Distro   string `json:"distro"`
	Type     string `json:"type"`
	Isolated bool   `json:"isolated"`
}

type DistroboxInfo struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Running bool   `json:"running"`
	Image   string `json:"image"`
}

// ─── Distro Adapters ─────────────────────────────────────────────────────────

type DistroAdapter interface {
	Install() string
	Remove() string
	Update() string
	Init() string // initial setup command after container creation
}

type DebianAdapter struct{}

func (DebianAdapter) Install() string { return "apt-get install -y" }
func (DebianAdapter) Remove() string  { return "apt-get remove -y" }
func (DebianAdapter) Update() string  { return "apt-get update && apt-get upgrade -y" }
func (DebianAdapter) Init() string    { return "apt-get update" }

type FedoraAdapter struct{}

func (FedoraAdapter) Install() string { return "dnf install -y" }
func (FedoraAdapter) Remove() string  { return "dnf remove -y" }
func (FedoraAdapter) Update() string  { return "dnf update -y" }
func (FedoraAdapter) Init() string    { return "dnf check-update; true" }

type ArchAdapter struct{}

func (ArchAdapter) Install() string { return "pacman -S --noconfirm" }
func (ArchAdapter) Remove() string  { return "pacman -R --noconfirm" }
func (ArchAdapter) Update() string  { return "pacman -Syu --noconfirm" }
func (ArchAdapter) Init() string    { return "pacman -Sy" }

type OpenSUSEAdapter struct{}

func (OpenSUSEAdapter) Install() string { return "zypper install -y" }
func (OpenSUSEAdapter) Remove() string  { return "zypper remove -y" }
func (OpenSUSEAdapter) Update() string  { return "zypper dup -y" }
func (OpenSUSEAdapter) Init() string    { return "zypper refresh" }

type UbuntuAdapter struct{}

func (UbuntuAdapter) Install() string { return "apt-get install -y" }
func (UbuntuAdapter) Remove() string  { return "apt-get remove -y" }
func (UbuntuAdapter) Update() string  { return "apt-get update && apt-get upgrade -y" }
func (UbuntuAdapter) Init() string    { return "apt-get update" }

type SlackwareAdapter struct{}

func (SlackwareAdapter) Install() string { return "slackpkg install" }
func (SlackwareAdapter) Remove() string  { return "slackpkg remove" }
func (SlackwareAdapter) Update() string  { return "slackpkg update && slackpkg upgrade-all" }
func (SlackwareAdapter) Init() string    { return "slackpkg update" }

type Distro struct {
	ContName string
	Image    string
	Adapter  DistroAdapter
}

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	repoURL       = "https://raw.githubusercontent.com/HackerOS-Linux-System/Isolator/main/repo/package-list.json"
	distroboxBin  = "distrobox"
	configDir     = ".config/isolator"
	installedFile = "installed.json"
	repoFile      = "package-list.json"
	homesDir      = ".isolator/homes"
	cacheMaxAge   = 24 * time.Hour
)

var distros = map[string]Distro{
	"debian":    {"debian-testing", "debian:testing", DebianAdapter{}},
	"fedora":    {"fedora", "registry.fedoraproject.org/fedora:latest", FedoraAdapter{}},
	"archlinux": {"archlinux", "archlinux:latest", ArchAdapter{}},
	"opensuse":  {"opensuse-tumbleweed", "registry.opensuse.org/opensuse/tumbleweed:latest", OpenSUSEAdapter{}},
	"ubuntu":    {"ubuntu", "ubuntu:latest", UbuntuAdapter{}},
	"slackware": {"slackware", "slackware64-current", SlackwareAdapter{}},
}

var containers []string

// ─── Styles ──────────────────────────────────────────────────────────────────

var (
	boldStyle    = lipgloss.NewStyle().Bold(true)
	errorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	successStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	infoStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	warnStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	cyanStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	magentaStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Help menu styles
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).
	Background(lipgloss.Color("236")).Padding(0, 2)
	cmdStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	descStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).MarginTop(1)
	flagStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

func init() {
	for _, d := range distros {
		containers = append(containers, d.ContName)
	}
}

// ─── Path helpers ─────────────────────────────────────────────────────────────

func configPath(file string) string {
	return filepath.Join(os.Getenv("HOME"), configDir, file)
}

func getRepoFilePath() string { return configPath(repoFile) }

func ensureConfigDir() error {
	return os.MkdirAll(filepath.Join(os.Getenv("HOME"), configDir), 0700)
}

// ─── Print helpers ────────────────────────────────────────────────────────────

func printError(msg string) {
	fmt.Println(errorStyle.Render("✗ Error: ") + msg)
}

func printInfo(msg string) {
	fmt.Println(infoStyle.Render("● ") + msg)
}

func printSuccess(msg string) {
	fmt.Println(successStyle.Render("✓ ") + msg)
}

func printWarn(msg string) {
	fmt.Println(warnStyle.Render("⚠ ") + msg)
}

func printStep(msg string) {
	fmt.Println(cyanStyle.Render("→ ") + msg)
}

// ─── Command execution ───────────────────────────────────────────────────────

func execCommand(bin string, args []string) bool {
	cmd := exec.Command(bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run() == nil
}

// execInContainer runs a command inside distrobox with sudo (non-interactive).
// Uses `sudo sh -c` with DEBIAN_FRONTEND=noninteractive to avoid any prompts.
func execInContainer(cont string, cmdStr string, useSudo bool) bool {
	fullCmd := cmdStr
	if useSudo {
		fullCmd = "DEBIAN_FRONTEND=noninteractive sudo -n sh -c '" + strings.ReplaceAll(cmdStr, "'", "'\\''") + "'"
	}
	args := []string{"enter", cont, "--", "sh", "-c", fullCmd}
	return execCommand(distroboxBin, args)
}

// execInContainerWithOutput runs a command and captures output.
func execInContainerWithOutput(cont string, cmdStr string, useSudo bool) (string, bool) {
	fullCmd := cmdStr
	if useSudo {
		fullCmd = "DEBIAN_FRONTEND=noninteractive sudo -n sh -c '" + strings.ReplaceAll(cmdStr, "'", "'\\''") + "'"
	}
	cmd := exec.Command(distroboxBin, "enter", cont, "--", "sh", "-c", fullCmd)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err == nil
}

// ─── Repo management ─────────────────────────────────────────────────────────

func loadRepo(force bool) bool {
	if err := ensureConfigDir(); err != nil {
		printError("Failed to create config directory: " + err.Error())
		return false
	}

	repoFilePath := getRepoFilePath()
	needsDownload := force

	if !needsDownload {
		info, err := os.Stat(repoFilePath)
		if err != nil || time.Since(info.ModTime()) > cacheMaxAge {
			needsDownload = true
		}
	}

	if needsDownload {
		s := spinner.New(spinner.CharSets[14], 80*time.Millisecond)
		s.Suffix = " Downloading repository list..."
		s.Color("cyan")
		s.Start()

		resp, err := http.Get(repoURL)
		if err != nil {
			s.Stop()
			printError("Failed to download repo list: " + err.Error())
			return false
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			s.Stop()
			printError(fmt.Sprintf("Download failed: HTTP %d", resp.StatusCode))
			return false
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			s.Stop()
			printError("Failed to read response body")
			return false
		}

		// Validate JSON before saving
		var validate []PackageInfo
		if err := json.Unmarshal(body, &validate); err != nil {
			s.Stop()
			printError("Downloaded file is invalid JSON: " + err.Error())
			return false
		}

		// Atomic write
		tmpPath := repoFilePath + ".tmp"
		if err := os.WriteFile(tmpPath, body, 0600); err != nil {
			s.Stop()
			printError("Failed to write repo file")
			return false
		}
		if err := os.Rename(tmpPath, repoFilePath); err != nil {
			s.Stop()
			os.Remove(tmpPath)
			printError("Failed to save repo file")
			return false
		}
		s.Stop()
		printSuccess(fmt.Sprintf("Repository list updated (%d packages)", len(validate)))
	}

	// Validate readable
	data, err := os.ReadFile(repoFilePath)
	if err != nil {
		printError("Failed to read repo file")
		return false
	}
	var pkgs []PackageInfo
	if err := json.Unmarshal(data, &pkgs); err != nil {
		printError("Repo cache is corrupted — run 'isolator refresh'")
		os.Remove(repoFilePath)
		return false
	}
	return true
}

func readRepoPackages() []PackageInfo {
	data, err := os.ReadFile(getRepoFilePath())
	if err != nil {
		return nil
	}
	var pkgs []PackageInfo
	json.Unmarshal(data, &pkgs)
	return pkgs
}

// ─── Container helpers ───────────────────────────────────────────────────────

func getDistroboxes() []DistroboxInfo {
	cmd := exec.Command(distroboxBin, "list", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var list []DistroboxInfo
	json.Unmarshal(out, &list)
	return list
}

func containerExists(name string) bool {
	for _, db := range getDistroboxes() {
		if db.Name == name {
			return true
		}
	}
	return false
}

func getOurContainers() []string {
	var ours []string
	for _, db := range getDistroboxes() {
		for _, base := range containers {
			if db.Name == base || strings.HasPrefix(db.Name, base+"-") {
				ours = append(ours, db.Name)
				break
			}
		}
	}
	return ours
}

func getContainerSize(name string) string {
	cmd := exec.Command("podman", "ps", "-a", "--size", "--format", "{{.Size}}", "--filter", "name=^"+name+"$")
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// createContainer creates a distrobox container, pulling image automatically.
func createContainer(name, image, homeDir string) bool {
	uid := os.Getuid()
	envFlag := fmt.Sprintf("--env=DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid)
	args := []string{
		"create",
		"--name", name,
		"--image", image,
		"--home", homeDir,
		"--yes",            // auto-confirm image pull — no interactive prompt
		"--additional-flags", envFlag,
	}
	printStep(fmt.Sprintf("Creating container %s (image: %s)...", name, image))
	return execCommand(distroboxBin, args)
}

// initContainer runs the initial package manager sync inside a fresh container.
func initContainer(cont string, d Distro) bool {
	printStep("Initializing package manager in container...")
	initCmd := d.Adapter.Init()
	return execInContainer(cont, initCmd, true)
}

// ─── Wrapper scripts ─────────────────────────────────────────────────────────

func createWrapper(pkg, contName string) bool {
	binDir := filepath.Join(os.Getenv("HOME"), ".local/bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return false
	}
	filePath := filepath.Join(binDir, pkg)
	content := fmt.Sprintf("#!/bin/sh\nexec distrobox enter %s -- %s \"$@\"\n", contName, pkg)
	if err := os.WriteFile(filePath, []byte(content), 0755); err != nil {
		return false
	}
	return true
}

func removeWrapper(pkg string) bool {
	filePath := filepath.Join(os.Getenv("HOME"), ".local/bin", pkg)
	err := os.Remove(filePath)
	return err == nil || os.IsNotExist(err)
}

// ─── Installed packages state ────────────────────────────────────────────────

func loadInstalled() ([]InstalledPackage, error) {
	if err := ensureConfigDir(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(configPath(installedFile))
	if err != nil {
		return []InstalledPackage{}, nil
	}
	var installed []InstalledPackage
	return installed, json.Unmarshal(data, &installed)
}

func saveInstalled(installed []InstalledPackage) error {
	file := configPath(installedFile)
	data, err := json.MarshalIndent(installed, "", "  ")
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// ─── Command handlers ────────────────────────────────────────────────────────

func handleInstall(pkg string, isolated bool) {
	if !loadRepo(false) {
		return
	}

	repoPackages := readRepoPackages()
	var info *PackageInfo
	for i := range repoPackages {
		if repoPackages[i].Name == pkg {
			info = &repoPackages[i]
			break
		}
	}
	if info == nil {
		printError(fmt.Sprintf("Package '%s' not found in repository", pkg))
		printInfo("Try: isolator search <term>")
		return
	}

	installed, err := loadInstalled()
	if err != nil {
		printError("Failed to load installed packages")
		return
	}
	for _, ip := range installed {
		if ip.Pkg == pkg {
			printWarn(fmt.Sprintf("Package '%s' is already installed (container: %s)", pkg, ip.Cont))
			return
		}
	}

	d, ok := distros[info.Distro]
	if !ok {
		printError("Unknown distro: " + info.Distro)
		return
	}

	printInfo(fmt.Sprintf("Installing %s  [distro: %s | type: %s]",
			      boldStyle.Render(pkg), cyanStyle.Render(info.Distro), dimStyle.Render(info.Type)))

	contName := d.ContName
	homeDir := os.Getenv("HOME")
	if isolated {
		contName += "-" + pkg
		homeDir = filepath.Join(homeDir, homesDir, pkg)
		if err := os.MkdirAll(homeDir, 0700); err != nil {
			printError("Failed to create isolated home directory")
			return
		}
		printInfo(fmt.Sprintf("Isolated mode: home → %s", homeDir))
	}

	newContainer := false
	if !containerExists(contName) {
		if !createContainer(contName, d.Image, homeDir) {
			printError(fmt.Sprintf("Failed to create container '%s'", contName))
			return
		}
		printSuccess(fmt.Sprintf("Container '%s' created", contName))
		newContainer = true
	} else {
		printInfo(fmt.Sprintf("Reusing existing container '%s'", contName))
	}

	// Init package manager on fresh containers
	if newContainer {
		if !initContainer(contName, d) {
			printWarn("Package manager init returned non-zero (may be OK for some distros)")
		}
	}

	packagesToInstall := []string{pkg}
	if len(info.Libs) > 0 {
		printInfo("Dependencies: " + strings.Join(info.Libs, ", "))
		packagesToInstall = append(info.Libs, packagesToInstall...)
	}

	installCmd := d.Adapter.Install() + " " + strings.Join(packagesToInstall, " ")
	printStep(fmt.Sprintf("Running: %s", dimStyle.Render(installCmd)))

	if !execInContainer(contName, installCmd, true) {
		printError("Installation failed")
		return
	}

	if !createWrapper(pkg, contName) {
		printError("Failed to create wrapper script in ~/.local/bin")
		return
	}

	installed = append(installed, InstalledPackage{
		Pkg:      pkg,
		Cont:     contName,
		Distro:   info.Distro,
		Type:     info.Type,
		Isolated: isolated,
	})
	if err := saveInstalled(installed); err != nil {
		printError("Failed to save installed info")
		return
	}
	printSuccess(fmt.Sprintf("'%s' installed successfully → %s",
				 pkg, filepath.Join(os.Getenv("HOME"), ".local/bin", pkg)))
}

func handleRemove(pkg string) {
	installed, err := loadInstalled()
	if err != nil {
		printError("Failed to load installed packages")
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
		printError(fmt.Sprintf("Package '%s' is not installed", pkg))
		return
	}

	if !loadRepo(false) {
		return
	}

	repoPackages := readRepoPackages()
	var info *PackageInfo
	for i := range repoPackages {
		if repoPackages[i].Name == pkg {
			info = &repoPackages[i]
			break
		}
	}
	if info == nil {
		printWarn(fmt.Sprintf("Package '%s' not found in repo — removing wrapper only", pkg))
	}

	printInfo(fmt.Sprintf("Removing %s from container '%s'", boldStyle.Render(pkg), ip.Cont))

	if !removeWrapper(pkg) {
		printError("Failed to remove wrapper script")
		return
	}

	if info != nil {
		d, ok := distros[ip.Distro]
		if !ok {
			printError("Unknown distro: " + ip.Distro)
			return
		}
		packagesToRemove := []string{pkg}
		if len(info.Libs) > 0 {
			printInfo("Also removing dependencies: " + strings.Join(info.Libs, ", "))
			packagesToRemove = append(packagesToRemove, info.Libs...)
		}
		removeCmd := d.Adapter.Remove() + " " + strings.Join(packagesToRemove, " ")
		if !execInContainer(ip.Cont, removeCmd, true) {
			printError("Package removal in container failed")
			return
		}
	}

	if ip.Isolated {
		printStep(fmt.Sprintf("Removing isolated container '%s'...", ip.Cont))
		if !execCommand(distroboxBin, []string{"rm", "--force", ip.Cont}) {
			printError("Failed to remove isolated container")
			return
		}
		isolatedHome := filepath.Join(os.Getenv("HOME"), homesDir, pkg)
		if err := os.RemoveAll(isolatedHome); err != nil {
			printWarn("Failed to remove isolated home dir: " + err.Error())
		}
	}

	installed = append(installed[:index], installed[index+1:]...)
	if err := saveInstalled(installed); err != nil {
		printError("Failed to save installed info")
		return
	}
	printSuccess(fmt.Sprintf("'%s' removed", pkg))
}

func handleUpdate() {
	printInfo("Updating all managed containers...")
	conts := getOurContainers()
	if len(conts) == 0 {
		printWarn("No managed containers found")
		return
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := map[string]bool{}

	for _, cont := range conts {
		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			distroName := ""
			for d, dd := range distros {
				if c == dd.ContName || strings.HasPrefix(c, dd.ContName+"-") {
					distroName = d
					break
				}
			}
			if distroName == "" {
				return
			}
			ok := execInContainer(c, distros[distroName].Adapter.Update(), true)
			mu.Lock()
			results[c] = ok
			mu.Unlock()
		}(cont)
	}
	wg.Wait()

	for cont, ok := range results {
		if ok {
			printSuccess(fmt.Sprintf("Updated: %s", cont))
		} else {
			printError(fmt.Sprintf("Update failed: %s", cont))
		}
	}
}

func handleRefresh() {
	printInfo("Force-refreshing repository list...")
	if loadRepo(true) {
		printSuccess("Repository refreshed")
	}
}

func handleUpgrade() {
	printInfo("Running full system upgrade...")
	if _, err := os.Stat("/usr/bin/apt"); err == nil {
		printError("System apt found in /usr/bin/ — potential conflict")
		return
	}
	if _, err := os.Stat("/usr/lib/isolator/apt"); os.IsNotExist(err) {
		printError("Isolator apt not found at /usr/lib/isolator/apt")
		return
	}
	execCommand("sudo", []string{"/usr/lib/isolator/apt", "update"})
	execCommand("sudo", []string{"/usr/lib/isolator/apt", "upgrade", "-y"})
	handleUpdate()
	printSuccess("Upgrade complete")
}

// ─── TUI model ───────────────────────────────────────────────────────────────

type tableModel struct {
	table table.Model
	title string
}

func (m tableModel) Init() tea.Cmd { return nil }

func (m tableModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
				case "q", "esc", "ctrl+c":
					return m, tea.Quit
			}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m tableModel) View() string {
	titleBar := titleStyle.Render(" " + m.title + " ")
	footer := dimStyle.Render("  ↑/↓ navigate   q quit")
	return "\n" + titleBar + "\n\n" + m.table.View() + "\n\n" + footer + "\n"
}

func buildStyledTable(columns []table.Column, rows []table.Row, height int) table.Model {
	t := table.New(
		table.WithColumns(columns),
		       table.WithRows(rows),
		       table.WithFocused(true),
		       table.WithHeight(height),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240")).
	BorderBottom(true).
	Bold(true).
	Foreground(lipgloss.Color("14"))
	s.Selected = s.Selected.
	Foreground(lipgloss.Color("230")).
	Background(lipgloss.Color("57")).
	Bold(true)
	t.SetStyles(s)
	return t
}

func runTable(title string, columns []table.Column, rows []table.Row) {
	if len(rows) == 0 {
		printInfo("No results")
		return
	}
	height := len(rows)
	if height > 20 {
		height = 20
	}
	t := buildStyledTable(columns, rows, height)
	m := tableModel{table: t, title: title}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		// Fallback: plain text
		for _, r := range rows {
			fmt.Println(strings.Join(r, "  "))
		}
	}
}

// ─── Handlers: search, status, info, list ────────────────────────────────────

func handleSearch(term string) {
	if !loadRepo(false) {
		return
	}
	repoPackages := readRepoPackages()

	columns := []table.Column{
		{Title: "Name", Width: 24},
		{Title: "Distro", Width: 14},
		{Title: "Type", Width: 6},
		{Title: "Dependencies", Width: 34},
	}
	var rows []table.Row
	for _, p := range repoPackages {
		if strings.Contains(strings.ToLower(p.Name), strings.ToLower(term)) {
			rows = append(rows, []string{p.Name, p.Distro, p.Type, strings.Join(p.Libs, ", ")})
		}
	}
	if len(rows) == 0 {
		printError(fmt.Sprintf("No packages matching '%s'", term))
		return
	}
	printInfo(fmt.Sprintf("Found %d result(s) for '%s'", len(rows), term))
	runTable(fmt.Sprintf("Search: %s", term), columns, rows)
}

func handleStatus() {
	installed, _ := loadInstalled()
	pkgMap := map[string][]string{}
	for _, ip := range installed {
		pkgMap[ip.Cont] = append(pkgMap[ip.Cont], ip.Pkg)
	}

	columns := []table.Column{
		{Title: "Container", Width: 26},
		{Title: "Status", Width: 12},
		{Title: "Size", Width: 18},
		{Title: "Packages", Width: 34},
	}
	var rows []table.Row
	for _, db := range getDistroboxes() {
		isOur := false
		for _, base := range containers {
			if db.Name == base || strings.HasPrefix(db.Name, base+"-") {
				isOur = true
				break
			}
		}
		if !isOur {
			continue
		}
		size := getContainerSize(db.Name)
		rows = append(rows, []string{db.Name, db.Status, size, strings.Join(pkgMap[db.Name], ", ")})
	}
	if len(rows) == 0 {
		printInfo("No managed containers found")
		return
	}
	runTable("Container Status", columns, rows)
}

func handleInfo(pkg string) {
	if !loadRepo(false) {
		return
	}
	repoPackages := readRepoPackages()
	for _, p := range repoPackages {
		if p.Name == pkg {
			fmt.Println()
			fmt.Println(titleStyle.Render(" Package Info "))
			fmt.Println()
			fmt.Printf("  %s  %s\n", boldStyle.Render("Name:   "), cyanStyle.Render(p.Name))
			fmt.Printf("  %s  %s\n", boldStyle.Render("Distro: "), magentaStyle.Render(p.Distro))
			fmt.Printf("  %s  %s\n", boldStyle.Render("Type:   "), p.Type)
			if len(p.Libs) > 0 {
				fmt.Printf("  %s  %s\n", boldStyle.Render("Libs:   "), strings.Join(p.Libs, ", "))
			}
			installed, _ := loadInstalled()
			for _, ip := range installed {
				if ip.Pkg == pkg {
					iso := ""
					if ip.Isolated {
						iso = " (isolated)"
					}
					fmt.Printf("  %s  %s\n", boldStyle.Render("Status: "), successStyle.Render("installed"+iso))
					fmt.Printf("  %s  %s\n", boldStyle.Render("Cont:   "), ip.Cont)
					fmt.Println()
					return
				}
			}
			fmt.Printf("  %s  %s\n", boldStyle.Render("Status: "), dimStyle.Render("not installed"))
			fmt.Println()
			return
		}
	}
	printError(fmt.Sprintf("Package '%s' not found", pkg))
}

func handleList() {
	installed, err := loadInstalled()
	if err != nil {
		printError("Failed to load installed packages")
		return
	}
	if len(installed) == 0 {
		printInfo("No packages installed yet")
		return
	}
	columns := []table.Column{
		{Title: "Package", Width: 22},
		{Title: "Distro", Width: 14},
		{Title: "Type", Width: 6},
		{Title: "Container", Width: 28},
		{Title: "Isolated", Width: 9},
	}
	var rows []table.Row
	for _, ip := range installed {
		iso := "no"
		if ip.Isolated {
			iso = "yes"
		}
		rows = append(rows, []string{ip.Pkg, ip.Distro, ip.Type, ip.Cont, iso})
	}
	runTable("Installed Packages", columns, rows)
}

// ─── Colored help ─────────────────────────────────────────────────────────────

func printColoredHelp() {
	fmt.Println()
	fmt.Println(titleStyle.Render("  Isolator — Distrobox Package Manager  "))
	fmt.Println()
	fmt.Println(sectionStyle.Render("  Usage"))
	fmt.Printf("    %s %s\n", cyanStyle.Render("isolator"), descStyle.Render("<command> [flags]"))
	fmt.Println()
	fmt.Println(sectionStyle.Render("  Commands"))

	cmds := []struct{ name, args, desc string }{
		{"install", "<pkg>", "Install a package into a distrobox container"},
		{"remove", "<pkg>", "Remove an installed package"},
		{"search", "<term>", "Search the repository for packages"},
		{"info", "<pkg>", "Show detailed info about a package"},
		{"list", "", "List all installed packages"},
		{"status", "", "Show container status dashboard"},
		{"update", "", "Update packages in all managed containers"},
		{"refresh", "", "Force re-download of the repository list"},
		{"upgrade", "", "Full system upgrade (host + containers)"},
	}
	for _, c := range cmds {
		args := ""
		if c.args != "" {
			args = " " + dimStyle.Render(c.args)
		}
		fmt.Printf("    %s%s\n      %s\n\n",
			   cmdStyle.Render(c.name), args, descStyle.Render(c.desc))
	}

	fmt.Println(sectionStyle.Render("  Flags"))
	fmt.Printf("    %s   auto-confirm image pull (default on)\n", flagStyle.Render("--yes"))
	fmt.Printf("    %s   install package in isolated container with its own home\n", flagStyle.Render("--isolated"))
	fmt.Println()
	fmt.Println(sectionStyle.Render("  Examples"))
	exs := []string{
		"isolator install firefox",
		"isolator install steam --isolated",
		"isolator search browser",
		"isolator info gimp",
		"isolator list",
		"isolator update",
	}
	for _, e := range exs {
		fmt.Printf("    %s\n", dimStyle.Render(e))
	}
	fmt.Println()
	fmt.Printf("  Use %s for command-specific help.\n\n",
		   cyanStyle.Render("isolator <command> --help"))
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	var rootCmd = &cobra.Command{
		Use:           "isolator",
		Short:         "Distrobox-based package manager",
		SilenceUsage:  true,
		SilenceErrors: true,
		Run: func(cmd *cobra.Command, args []string) {
			printColoredHelp()
		},
	}

	installCmd := &cobra.Command{
		Use:   "install <pkg>",
		Short: "Install a package",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			isolated, _ := cmd.Flags().GetBool("isolated")
			handleInstall(args[0], isolated)
		},
	}
	installCmd.Flags().Bool("isolated", false, "Install in isolated container with its own home directory")

	rootCmd.AddCommand(
		installCmd,
		&cobra.Command{
			Use:   "remove <pkg>",
			Short: "Remove an installed package",
			Args:  cobra.ExactArgs(1),
			   Run: func(cmd *cobra.Command, args []string) {
				   handleRemove(args[0])
			   },
		},
		&cobra.Command{
			Use:   "search <term>",
			Short: "Search for a package",
			Args:  cobra.ExactArgs(1),
			   Run: func(cmd *cobra.Command, args []string) {
				   handleSearch(args[0])
			   },
		},
		&cobra.Command{
			Use:   "info <pkg>",
			Short: "Show package information",
			Args:  cobra.ExactArgs(1),
			   Run: func(cmd *cobra.Command, args []string) {
				   handleInfo(args[0])
			   },
		},
		&cobra.Command{
			Use:   "list",
			Short: "List installed packages",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				handleList()
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Show container status dashboard",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				handleStatus()
			},
		},
		&cobra.Command{
			Use:   "update",
			Short: "Update all containers",
			Args:  cobra.NoArgs,
			Run: func(cmd *cobra.Command, args []string) {
				handleUpdate()
			},
		},
		&cobra.Command{
			Use:   "refresh",
		    Short: "Force re-download of the repository list",
		    Args:  cobra.NoArgs,
		    Run: func(cmd *cobra.Command, args []string) {
			    handleRefresh()
		    },
		},
		&cobra.Command{
			Use:   "upgrade",
		    Short: "Full system upgrade (host + containers)",
			   Args:  cobra.NoArgs,
		    Run: func(cmd *cobra.Command, args []string) {
			    handleUpgrade()
		    },
		},
	)

	if err := rootCmd.Execute(); err != nil {
		printError(err.Error())
		os.Exit(1)
	}
}
