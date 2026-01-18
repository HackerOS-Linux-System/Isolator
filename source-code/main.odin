package main

import "core:fmt"
import "core:os"
import "core:os/os2"
import "core:strings"
import "core:path/filepath"
import "core:encoding/json"
import "core:sys/unix"
import "core:time"

ANSI_RESET :: "\x1b[0m"
ANSI_BOLD :: "\x1b[1m"
ANSI_RED :: "\x1b[31m"
ANSI_GREEN :: "\x1b[32m"
ANSI_YELLOW :: "\x1b[33m"
ANSI_BLUE :: "\x1b[34m"
ANSI_MAGENTA :: "\x1b[35m"
ANSI_CYAN :: "\x1b[36m"
ANSI_WHITE :: "\x1b[37m"

REPO_URL :: "https://raw.githubusercontent.com/HackerOS-Linux-System/Isolator/main/repo/package-list.json"
LOCK_FILE :: ".config/isolator/lock"
CONTAINERS :: []string{"archlinux", "fedora", "debian-testing", "opensuse-tumbleweed"}
DISTROBOX_BIN :: "distrobox"
PODMAN_BIN :: "podman"

Package_Info :: struct {
    name: string,
    distro: string,
    type: string, // cli or gui
}

repo_packages: [dynamic]Package_Info

main :: proc() {
    args := os.args[1:]
    if len(args) == 0 {
        print_help()
        return
    }
    cmd := args[0]
    switch cmd {
        case "init":
            handle_init()
        case "install":
            if len(args) < 2 { print_error("Missing package name"); return }
            handle_install(args[1])
        case "remove":
            if len(args) < 2 { print_error("Missing package name"); return }
            handle_remove(args[1])
        case "update":
            handle_update()
        case "refresh":
            handle_refresh()
        case "upgrade":
            handle_upgrade()
        case "search":
            if len(args) < 2 { print_error("Missing search term"); return }
            handle_search(args[1])
        case:
            print_error("Unknown command")
            print_help()
    }
}

print_help :: proc() {
    fmt.printf("%s%sIsolator CLI Tool%s\n", ANSI_BOLD, ANSI_CYAN, ANSI_RESET)
    fmt.printf("Usage: isolator <command> [args]\n\n")
    fmt.printf("%sCommands:%s\n", ANSI_YELLOW, ANSI_RESET)
    fmt.printf(" init - Initialize isolator (create containers)\n")
    fmt.printf(" install <pkg> - Install a package\n")
    fmt.printf(" remove <pkg> - Remove a package\n")
    fmt.printf(" update - Update everything\n")
    fmt.printf(" refresh - Refresh repositories\n")
    fmt.printf(" upgrade - Upgrade all possible (with checks)\n")
    fmt.printf(" search <pkg> - Search for a package\n")
}

print_error :: proc(msg: string) {
    fmt.eprintf("%s%sError: %s%s\n", ANSI_BOLD, ANSI_RED, msg, ANSI_RESET)
}

print_info :: proc(msg: string) {
    fmt.printf("%s%sInfo: %s%s\n", ANSI_BOLD, ANSI_BLUE, msg, ANSI_RESET)
}

print_success :: proc(msg: string) {
    fmt.printf("%s%sSuccess: %s%s\n", ANSI_BOLD, ANSI_GREEN, msg, ANSI_RESET)
}

handle_init :: proc() {
    home_dir := os.get_env("HOME")
    lock_path := filepath.join([]string{home_dir, LOCK_FILE})
    if os.exists(lock_path) {
        print_error("Isolator already initialized (lock file exists)")
        return
    }
    print_info("Initializing isolator...")
    uid := os2.get_uid()
    // Create containers with shared home and dbus
    for cont in CONTAINERS {
        distro := cont
        if cont == "debian-testing" { distro = "debian:testing" }
        else if cont == "opensuse-tumbleweed" { distro = "opensuse/tumbleweed" }
        env_flag := fmt.tprintf("--env=DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/%d/bus", uid)
        cmd_args := []string{"create", "--name", cont, "--image", distro, "--home", os.get_env("HOME"), "--additional-flags", env_flag, "--yes"}
        if !exec_command(DISTROBOX_BIN, cmd_args) {
            print_error(fmt.tprintf("Failed to create container: %s", cont))
            return
        }
        print_success(fmt.tprintf("Created container: %s", cont))
    }
    // Create lock file
    ok := os.write_entire_file(lock_path, transmute([]u8)string("initialized"))
    if !ok {
        print_error("Failed to create lock file")
        return
    }
    print_success("Initialization complete")
}

handle_install :: proc(pkg: string) {
    if !load_repo() { return }
    found: bool
    info: Package_Info
    for p in repo_packages {
        if p.name == pkg {
            found = true
            info = p
            break
        }
    }
    if !found {
        print_error(fmt.tprintf("Package not found: %s", pkg))
        return
    }
    print_info(fmt.tprintf("Installing %s from %s (%s)", pkg, info.distro, info.type))
    // Install in container
    installer: string
    switch info.distro {
        case "debian": installer = "apt install -y"
        case "fedora": installer = "dnf install -y"
        case "archlinux": installer = "pacman -S --noconfirm"
        case "archlinux-yay": installer = "yay -S --noconfirm"
        case "opensuse": installer = "zypper install -y"
    }
    cont_name := get_container_name(info.distro)
    if cont_name == "" {
        print_error("Unknown distro")
        return
    }
    if !exec_in_container(cont_name, fmt.tprintf("%s %s", installer, pkg)) {
        print_error("Installation failed")
        return
    }
    // Create wrapper or desktop
    if info.type == "cli" {
        create_cli_wrapper(pkg, cont_name)
    } else if info.type == "gui" {
        create_gui_desktop(pkg, cont_name)
    }
    print_success("Installation complete")
}

handle_remove :: proc(pkg: string) {
    if !load_repo() { return }
    found: bool
    info: Package_Info
    for p in repo_packages {
        if p.name == pkg {
            found = true
            info = p
            break
        }
    }
    if !found {
        print_error(fmt.tprintf("Package not found: %s", pkg))
        return
    }
    print_info(fmt.tprintf("Removing %s from %s", pkg, info.distro))
    // Remove from container
    remover: string
    switch info.distro {
        case "debian": remover = "apt remove -y"
        case "fedora": remover = "dnf remove -y"
        case "archlinux": remover = "pacman -R --noconfirm"
        case "archlinux-yay": remover = "yay -R --noconfirm"
        case "opensuse": remover = "zypper remove -y"
    }
    cont_name := get_container_name(info.distro)
    if cont_name == "" {
        print_error("Unknown distro")
        return
    }
    if !exec_in_container(cont_name, fmt.tprintf("%s %s", remover, pkg)) {
        print_error("Removal failed")
        return
    }
    // Remove wrapper or desktop
    if info.type == "cli" {
        remove_cli_wrapper(pkg)
    } else if info.type == "gui" {
        remove_gui_desktop(pkg)
    }
    print_success("Removal complete")
}

handle_update :: proc() {
    print_info("Updating everything...")
    for cont in CONTAINERS {
        updater: string
        switch cont {
            case "debian-testing": updater = "apt update && apt upgrade -y"
            case "fedora": updater = "dnf update -y"
            case "archlinux": updater = "pacman -Syu --noconfirm"
            case "opensuse-tumbleweed": updater = "zypper dup -y"
        }
        exec_in_container(cont, updater)
    }
    print_success("Update complete")
}

handle_refresh :: proc() {
    print_info("Refreshing repositories...")
    load_repo(true) // Force reload
    print_success("Refresh complete")
}

handle_upgrade :: proc() {
    print_info("Upgrading all...")
    // Check apt location
    if os.exists("/usr/bin/apt") {
        print_error("System apt found in /usr/bin/ - potential conflict")
        return
    }
    if !os.exists("/usr/lib/isolator/apt") {
        print_error("Isolator apt not found in /usr/lib/isolator/apt")
        return
    }
    // Run system upgrade
    exec_command("sudo", []string{"/usr/lib/isolator/apt", "update"})
    exec_command("sudo", []string{"/usr/lib/isolator/apt", "upgrade", "-y"})
    // Upgrade containers
    handle_update()
    print_success("Upgrade complete")
}

handle_search :: proc(term: string) {
    if !load_repo() { return }
    print_info(fmt.tprintf("Searching for %s...", term))
    found := false
    for p in repo_packages {
        if strings.contains(p.name, term) {
            fmt.printf("%s%s -> %s -> %s%s\n", ANSI_GREEN, p.name, p.distro, p.type, ANSI_RESET)
            found = true
        }
    }
    if !found {
        print_error("No packages found")
    }
}

load_repo :: proc(force: bool = false) -> bool {
    temp_file := "/tmp/package-list.json"
    if force || !os.exists(temp_file) {
        print_info("Downloading repo list...")
        if !exec_command("curl", []string{"-L", "-o", temp_file, REPO_URL}) {
            print_error("Failed to download repo list")
            return false
        }
    }
    data, ok := os.read_entire_file(temp_file)
    if !ok {
        print_error("Failed to read repo file")
        return false
    }
    json_data, parse_err := json.parse(data[:])
    if parse_err != .None {
        print_error("Failed to parse JSON")
        return false
    }
    defer json.destroy_value(json_data)
    arr, is_array := json_data.(json.Array)
    if !is_array {
        print_error("Repo JSON is not an array")
        return false
    }
    clear(&repo_packages)
    for val in arr {
        obj, is_obj := val.(json.Object)
        if !is_obj { continue }
        name_val, has_name := obj["name"]
        distro_val, has_distro := obj["distro"]
        type_val, has_type := obj["type"]
        if !has_name || !has_distro || !has_type { continue }
        name, name_ok := name_val.(json.String)
        distro, distro_ok := distro_val.(json.String)
        typ, type_ok := type_val.(json.String)
        if !name_ok || !distro_ok || !type_ok { continue }
        append(&repo_packages, Package_Info{name = string(name), distro = string(distro), type = string(typ)})
    }
    return true
}

get_container_name :: proc(distro: string) -> string {
    switch distro {
        case "debian": return "debian-testing"
        case "fedora": return "fedora"
        case "archlinux", "archlinux-yay": return "archlinux"
        case "opensuse": return "opensuse-tumbleweed"
    }
    return ""
}

exec_command :: proc(bin: string, args: []string) -> bool {
    full_command := make([dynamic]string)
    defer delete(full_command)
    append(&full_command, bin)
    for arg in args {
        append(&full_command, arg)
    }
    desc := os2.Process_Desc{
        command = full_command[:],
        env = nil,
        stdin = os2.stdin,
        stdout = os2.stdout,
        stderr = os2.stderr,
    }
    p, start_err := os2.process_start(desc)
    if start_err != nil {
        return false
    }
    state, wait_err := os2.process_wait(p, time.Duration(-1))
    if wait_err != nil {
        return false
    }
    return state.exit_code == 0
}

exec_in_container :: proc(cont: string, cmd: string) -> bool {
    args := []string{"enter", cont, "--", "sudo", "-S", "sh", "-c", cmd}
    return exec_command(DISTROBOX_BIN, args)
}

create_cli_wrapper :: proc(pkg: string, cont: string) {
    home_dir := os.get_env("HOME")
    wrapper_path := filepath.join([]string{home_dir, ".local/bin", pkg})
    content := fmt.tprintf("#!/bin/sh\ndistrobox enter %s -- %s \"$@\"", cont, pkg)
    os.write_entire_file(wrapper_path, transmute([]u8)content)
    c_path := strings.clone_to_cstring(wrapper_path)
    defer delete(c_path)
    unix.sys_chmod(c_path, 0o755)
}

remove_cli_wrapper :: proc(pkg: string) {
    home_dir := os.get_env("HOME")
    wrapper_path := filepath.join([]string{home_dir, ".local/bin", pkg})
    os.remove(wrapper_path)
}

create_gui_desktop :: proc(pkg: string, cont: string) {
    home_dir := os.get_env("HOME")
    desktop_path := filepath.join([]string{home_dir, ".local/share/applications", fmt.tprintf("%s.desktop", pkg)})
    content := fmt.tprintf("[Desktop Entry]\nName=%s\nExec=distrobox enter %s -- %s\nType=Application", pkg, cont, pkg)
    os.write_entire_file(desktop_path, transmute([]u8)content)
}

remove_gui_desktop :: proc(pkg: string) {
    home_dir := os.get_env("HOME")
    desktop_path := filepath.join([]string{home_dir, ".local/share/applications", fmt.tprintf("%s.desktop", pkg)})
    os.remove(desktop_path)
}
