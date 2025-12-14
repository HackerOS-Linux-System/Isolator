import os
import subprocess
from pathlib import Path
import argparse
from rich.console import Console
from rich.progress import Progress, SpinnerColumn, TextColumn, BarColumn, TaskProgressColumn
from rich.theme import Theme

# Cyberpunk theme
cyberpunk_theme = Theme({
    "info": "bright_cyan",
    "warning": "bright_yellow",
    "danger": "bright_red",
    "success": "bright_green",
    "prompt": "bright_magenta",
    "progress": "green",
})

console = Console(theme=cyberpunk_theme)

# Base image for Arch Linux
BASE_IMAGE = "docker.io/library/archlinux:latest"

# Directory for Isolator (though Podman handles storage, we can use it for configs if needed)
ISOLATOR_DIR = Path.home() / ".hackeros" / "isolator"
ISOLATOR_DIR.mkdir(parents=True, exist_ok=True)

def get_container_name(package):
    # Replace slashes for repo/package names
    return f"isolator-{package.replace('/', '-')}"

def run_command(cmd, description, progress, task):
    progress.update(task, description=description)
    try:
        subprocess.run(cmd, check=True, capture_output=True, text=True)
    except subprocess.CalledProcessError as e:
        console.print(f"[danger]Error: {e.stderr.strip()}[/]")
        raise

def install(package):
    container = get_container_name(package)
    with Progress(
        SpinnerColumn(style="progress"),
        TextColumn("[progress.description]{task.description}"),
        BarColumn(style="progress"),
        TaskProgressColumn(),
        console=console
    ) as progress:
        task = progress.add_task("Initializing...", total=5)
        
        # Create container with GUI support options (X11 forwarding prep)
        cmd_create = [
            "podman", "create", "--name", container,
            "-v", "/tmp/.X11-unix:/tmp/.X11-unix",
            "--device", "/dev/dri",
            "--ipc", "host",
            BASE_IMAGE, "sleep", "infinity"
        ]
        run_command(cmd_create, "Creating container...", progress, task)
        progress.advance(task)
        
        cmd_start = ["podman", "start", container]
        run_command(cmd_start, "Starting container...", progress, task)
        progress.advance(task)
        
        cmd_update = ["podman", "exec", container, "pacman", "-Syu", "--noconfirm"]
        run_command(cmd_update, "Updating system...", progress, task)
        progress.advance(task)
        
        cmd_install = ["podman", "exec", container, "pacman", "-S", "--noconfirm", package]
        run_command(cmd_install, f"Installing {package}...", progress, task)
        progress.advance(task)
        
        cmd_stop = ["podman", "stop", container]
        run_command(cmd_stop, "Stopping container...", progress, task)
        progress.advance(task)
    
    console.print(f"[success]Package {package} installed in container {container}.[/]")
    console.print("[info]To run GUI apps, use: podman start {container}; podman exec -it -e DISPLAY=$DISPLAY {container} <app_command>[/]")

def remove(package):
    container = get_container_name(package)
    with Progress(SpinnerColumn(style="progress"), TextColumn("[progress.description]{task.description}"), console=console) as progress:
        task = progress.add_task("Removing container...", total=None)
        cmd = ["podman", "rm", "-f", container]
        run_command(cmd, "Removing container...", progress, task)
    console.print(f"[success]Removed container for {package}.[/]")

def update(package):
    container = get_container_name(package)
    with Progress(
        SpinnerColumn(style="progress"),
        TextColumn("[progress.description]{task.description}"),
        BarColumn(style="progress"),
        TaskProgressColumn(),
        console=console
    ) as progress:
        task = progress.add_task("Initializing...", total=4)
        
        cmd_start = ["podman", "start", container]
        run_command(cmd_start, "Starting container...", progress, task)
        progress.advance(task)
        
        cmd_update = ["podman", "exec", container, "pacman", "-Syu", "--noconfirm"]
        run_command(cmd_update, "Updating system...", progress, task)
        progress.advance(task)
        
        cmd_install = ["podman", "exec", container, "pacman", "-S", "--noconfirm", package]
        run_command(cmd_install, f"Reinstalling/Updating {package}...", progress, task)
        progress.advance(task)
        
        cmd_stop = ["podman", "stop", container]
        run_command(cmd_stop, "Stopping container...", progress, task)
        progress.advance(task)
    
    console.print(f"[success]Updated {package} in container {container}.[/]")

def get_all_isolator_containers():
    cmd = ["podman", "ps", "-a", "--format", "{{.Names}}", "--filter", "name=^isolator-"]
    output = subprocess.check_output(cmd).decode().strip().splitlines()
    return [name for name in output if name.startswith("isolator-")]

def update_all():
    containers = get_all_isolator_containers()
    if not containers:
        console.print("[warning]No isolator containers found.[/]")
        return
    
    for container in containers:
        package = container.replace("isolator-", "").replace("-", "/")  # Approximate reverse
        console.print(f"[info]Updating container {container} ({package})...[/]")
        with Progress(
            SpinnerColumn(style="progress"),
            TextColumn("[progress.description]{task.description}"),
            BarColumn(style="progress"),
            TaskProgressColumn(),
            console=console
        ) as progress:
            task = progress.add_task("Initializing...", total=3)
            
            cmd_start = ["podman", "start", container]
            run_command(cmd_start, "Starting container...", progress, task)
            progress.advance(task)
            
            cmd_update = ["podman", "exec", container, "pacman", "-Syu", "--noconfirm"]
            run_command(cmd_update, "Updating system...", progress, task)
            progress.advance(task)
            
            cmd_stop = ["podman", "stop", container]
            run_command(cmd_stop, "Stopping container...", progress, task)
            progress.advance(task)
        
        console.print(f"[success]Updated {container}.[/]")

def how_working():
    explanation = """
[info]How Isolator Works:[/]

Isolator is a tool for managing isolated packages in Podman containers on HackerOS.
- Each package gets its own Arch Linux-based container.
- Containers are named 'isolator-<package>' and stored via Podman (default storage: ~/.local/share/containers).
- Install: Creates a new container, updates the system, installs the package via pacman, and stops it.
- Remove: Force-removes the container.
- Update: Starts the container, updates the system and reinstalls the package, then stops it.
- Update-all: Updates the system in all isolator containers.
- GUI Support: Containers are created with X11 and GPU device mounts for GUI apps (e.g., Steam). To run: podman start <container>; podman exec -it -e DISPLAY=$DISPLAY <container> <app_command>

Note: For AUR packages, you may need to install 'yay' manually inside the container first. This tool uses pacman for official repos.
"""
    console.print(explanation)

def main():
    console.print("[prompt]Isolator - Cyberpunk Container Isolator for HackerOS[/]")
    
    parser = argparse.ArgumentParser(description="Isolator CLI")
    subparsers = parser.add_subparsers(dest="command", required=True)

    install_parser = subparsers.add_parser("install", help="Install a package in a new container")
    install_parser.add_argument("package", help="Package name")

    remove_parser = subparsers.add_parser("remove", help="Remove a package container")
    remove_parser.add_argument("package", help="Package name")

    update_parser = subparsers.add_parser("update", help="Update a package container")
    update_parser.add_argument("package", help="Package name (or container)")

    subparsers.add_parser("update-all", help="Update all isolator containers")

    subparsers.add_parser("how-working", help="Explain how the tool works")

    args = parser.parse_args()

    try:
        if args.command == "install":
            install(args.package)
        elif args.command == "remove":
            remove(args.package)
        elif args.command == "update":
            update(args.package)
        elif args.command == "update-all":
            update_all()
        elif args.command == "how-working":
            how_working()
    except Exception as e:
        console.print(f"[danger]Critical error: {str(e)}[/]")

if __name__ == "__main__":
    main()
