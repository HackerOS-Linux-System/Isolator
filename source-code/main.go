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
    "syscall"
)

// Stałe ANSI do kolorowania tekstu
const (
    ANSI_RESET   = "\x1b[0m"
    ANSI_BOLD    = "\x1b[1m"
    ANSI_RED     = "\x1b[31m"
    ANSI_GREEN   = "\x1b[32m"
    ANSI_YELLOW  = "\x1b[33m"
    ANSI_BLUE    = "\x1b[34m"
    ANSI_MAGENTA = "\x1b[35m"
    ANSI_CYAN    = "\x1b[36m"
    ANSI_WHITE   = "\x1b[37m"
)

// Konfiguracja
const (
    REPO_URL      = "https://raw.githubusercontent.com/HackerOS-Linux-System/Isolator/main/repo/package-list.json"
    LOCK_FILE     = ".config/isolator/lock"
    DISTROBOX_BIN = "distrobox"
)

var CONTAINERS = []string{"archlinux", "fedora", "debian-testing", "opensuse-tumbleweed"}

// Struktura pakietu (odpowiada JSON)
type PackageInfo struct {
    Name   string `json:"name"`
    Distro string `json:"distro"`
    Type   string `json:"type"` // cli lub gui
}

// Zmienna globalna przechowująca listę pakietów
var repoPackages []PackageInfo

func main() {
    args := os.Args[1:]
    if len(args) == 0 {
        printHelp()
        return
    }

    cmd := args[0]
    switch cmd {
        case "init":
            handleInit()
        case "install":
            if len(args) < 2 {
                printError("Missing package name")
                return
            }
            handleInstall(args[1])
        case "remove":
            if len(args) < 2 {
                printError("Missing package name")
                return
            }
            handleRemove(args[1])
        case "update":
            handleUpdate()
        case "refresh":
            handleRefresh()
        case "upgrade":
            handleUpgrade()
        case "search":
            if len(args) < 2 {
                printError("Missing search term")
                return
            }
            handleSearch(args[1])
        default:
            printError("Unknown command")
            printHelp()
    }
}

func printHelp() {
    fmt.Printf("%s%sIsolator CLI Tool%s\n", ANSI_BOLD, ANSI_CYAN, ANSI_RESET)
    fmt.Println("Usage: isolator <command> [args]")
    fmt.Println()
    fmt.Printf("%sCommands:%s\n", ANSI_YELLOW, ANSI_RESET)
    fmt.Println(" init - Initialize isolator (create containers)")
    fmt.Println(" install <pkg> - Install a package")
    fmt.Println(" remove <pkg> - Remove a package")
    fmt.Println(" update - Update everything")
    fmt.Println(" refresh - Refresh repositories")
    fmt.Println(" upgrade - Upgrade all possible (with checks)")
    fmt.Println(" search <pkg> - Search for a package")
}

func printError(msg string) {
    fmt.Fprintf(os.Stderr, "%s%sError: %s%s\n", ANSI_BOLD, ANSI_RED, msg, ANSI_RESET)
}

func printInfo(msg string) {
    fmt.Printf("%s%sInfo: %s%s\n", ANSI_BOLD, ANSI_BLUE, msg, ANSI_RESET)
}

func printSuccess(msg string) {
    fmt.Printf("%s%sSuccess: %s%s\n", ANSI_BOLD, ANSI_GREEN, msg, ANSI_RESET)
}

func getHomeDir() string {
    home, err := os.UserHomeDir()
    if err != nil {
        return os.Getenv("HOME") // Fallback
    }
    return home
}

func handleInit() {
    homeDir := getHomeDir()
    lockPath := filepath.Join(homeDir, LOCK_FILE)

    if _, err := os.Stat(lockPath); err == nil {
        printError("Isolator already initialized (lock file exists)")
        return
    }

    printInfo("Initializing isolator...")
    uid := os.Getuid()

    // Tworzenie kontenerów
    for _, cont := range CONTAINERS {
        distro := cont
        if cont == "debian-testing" {
            distro = "debian:testing"
        } else if cont == "opensuse-tumbleweed" {
            distro = "opensuse/tumbleweed"
        }

        envFlag := fmt.Sprintf("--env=DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid)
        cmdArgs := []string{
            "create",
            "--name", cont,
            "--image", distro,
            "--home", homeDir,
            "--additional-flags", envFlag,
            "--yes",
        }

        if !execCommand(DISTROBOX_BIN, cmdArgs...) {
            printError(fmt.Sprintf("Failed to create container: %s", cont))
            return
        }
        printSuccess(fmt.Sprintf("Created container: %s", cont))
    }

    // Tworzenie pliku blokady
    if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
        printError("Failed to create config directory")
        return
    }
    if err := os.WriteFile(lockPath, []byte("initialized"), 0644); err != nil {
        printError("Failed to create lock file")
        return
    }

    printSuccess("Initialization complete")
}

func handleInstall(pkg string) {
    if !loadRepo(false) {
        return
    }

    var found bool
    var info PackageInfo

    for _, p := range repoPackages {
        if p.Name == pkg {
            found = true
            info = p
            break
        }
    }

    if !found {
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
        case "archlinux-yay":
            installer = "yay -S --noconfirm"
        case "opensuse":
            installer = "zypper install -y"
        default:
            printError("Unknown distro type in package definition")
            return
    }

    contName := getContainerName(info.Distro)
    if contName == "" {
        printError("Unknown distro mapping")
        return
    }

    // Instalacja w kontenerze
    installCmd := fmt.Sprintf("%s %s", installer, pkg)
    if !execInContainer(contName, installCmd) {
        printError("Installation failed")
        return
    }

    // Tworzenie wrapperów
    if info.Type == "cli" {
        createCliWrapper(pkg, contName)
    } else if info.Type == "gui" {
        createGuiDesktop(pkg, contName)
    }

    printSuccess("Installation complete")
}

func handleRemove(pkg string) {
    if !loadRepo(false) {
        return
    }

    var found bool
    var info PackageInfo

    for _, p := range repoPackages {
        if p.Name == pkg {
            found = true
            info = p
            break
        }
    }

    if !found {
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
        case "archlinux-yay":
            remover = "yay -R --noconfirm"
        case "opensuse":
            remover = "zypper remove -y"
    }

    contName := getContainerName(info.Distro)
    if contName == "" {
        printError("Unknown distro")
        return
    }

    removeCmd := fmt.Sprintf("%s %s", remover, pkg)
    if !execInContainer(contName, removeCmd) {
        printError("Removal failed")
        return
    }

    if info.Type == "cli" {
        removeCliWrapper(pkg)
    } else if info.Type == "gui" {
        removeGuiDesktop(pkg)
    }

    printSuccess("Removal complete")
}

func handleUpdate() {
    printInfo("Updating everything...")
    for _, cont := range CONTAINERS {
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
        }
        // Wykonujemy update, ale nie przerywamy pętli w razie błędu jednego kontenera
        execInContainer(cont, updater)
    }
    printSuccess("Update complete")
}

func handleRefresh() {
    printInfo("Refreshing repositories...")
    if loadRepo(true) {
        printSuccess("Refresh complete")
    }
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

    // Upgrade systemu hosta
    execCommand("sudo", "/usr/lib/isolator/apt", "update")
    execCommand("sudo", "/usr/lib/isolator/apt", "upgrade", "-y")

    // Upgrade kontenerów
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
            fmt.Printf("%s%s -> %s -> %s%s\n", ANSI_GREEN, p.Name, p.Distro, p.Type, ANSI_RESET)
            found = true
        }
    }
    if !found {
        printError("No packages found")
    }
}

// Pobiera i parsuje listę pakietów
func loadRepo(force bool) bool {
    tempFile := "/tmp/package-list.json"

    _, err := os.Stat(tempFile)
    if force || os.IsNotExist(err) {
        printInfo("Downloading repo list...")

        // Używamy natywnego HTTP clienta zamiast exec curl
        resp, err := http.Get(REPO_URL)
        if err != nil {
            printError(fmt.Sprintf("Failed to download repo list: %v", err))
            return false
        }
        defer resp.Body.Close()

        data, err := io.ReadAll(resp.Body)
        if err != nil {
            printError("Failed to read response body")
            return false
        }

        if err := os.WriteFile(tempFile, data, 0644); err != nil {
            printError("Failed to write temp file")
            return false
        }
    }

    fileData, err := os.ReadFile(tempFile)
    if err != nil {
        printError("Failed to read repo file")
        return false
    }

    if err := json.Unmarshal(fileData, &repoPackages); err != nil {
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
        case "archlinux", "archlinux-yay":
            return "archlinux"
        case "opensuse":
            return "opensuse-tumbleweed"
    }
    return ""
}

func execCommand(bin string, args ...string) bool {
    cmd := exec.Command(bin, args...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr

    if err := cmd.Run(); err != nil {
        return false
    }
    return true
}

func execInContainer(cont string, cmd string) bool {
    // distrobox enter <cont> -- sudo -S sh -c "<cmd>"
    args := []string{"enter", cont, "--", "sudo", "-S", "sh", "-c", cmd}
    return execCommand(DISTROBOX_BIN, args...)
}

func createCliWrapper(pkg string, cont string) {
    homeDir := getHomeDir()
    binDir := filepath.Join(homeDir, ".local", "bin")

    if err := os.MkdirAll(binDir, 0755); err != nil {
        printError("Failed to create ~/.local/bin directory")
        return
    }

    wrapperPath := filepath.Join(binDir, pkg)
    content := fmt.Sprintf("#!/bin/sh\ndistrobox enter %s -- %s \"$@\"", cont, pkg)

    if err := os.WriteFile(wrapperPath, []byte(content), 0755); err != nil {
        printError("Failed to write CLI wrapper")
        return
    }

    // Upewnij się, że jest wykonywalny (chociaż WriteFile z 0755 powinno zadziałać przy tworzeniu)
    os.Chmod(wrapperPath, 0755)
}

func removeCliWrapper(pkg string) {
    homeDir := getHomeDir()
    wrapperPath := filepath.Join(homeDir, ".local", "bin", pkg)
    os.Remove(wrapperPath)
}

func createGuiDesktop(pkg string, cont string) {
    homeDir := getHomeDir()
    appsDir := filepath.Join(homeDir, ".local", "share", "applications")

    if err := os.MkdirAll(appsDir, 0755); err != nil {
        printError("Failed to create ~/.local/share/applications directory")
        return
    }

    desktopPath := filepath.Join(appsDir, fmt.Sprintf("%s.desktop", pkg))
    content := fmt.Sprintf("[Desktop Entry]\nName=%s\nExec=distrobox enter %s -- %s\nType=Application", pkg, cont, pkg)

    if err := os.WriteFile(desktopPath, []byte(content), 0644); err != nil {
        printError("Failed to write GUI desktop file")
        return
    }
}

func removeGuiDesktop(pkg string) {
    homeDir := getHomeDir()
    desktopPath := filepath.Join(homeDir, ".local", "share", "applications", fmt.Sprintf("%s.desktop", pkg))
    os.Remove(desktopPath)
}
