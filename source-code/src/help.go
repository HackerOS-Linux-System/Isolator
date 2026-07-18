package src

import (
	"fmt"
)

func PrintColoredHelp() {
	fmt.Println()
	fmt.Println(TitleStyle.Render("  Isolator — Podman Package Manager  "))
	fmt.Println()
	fmt.Println(SectionStyle.Render("  Usage"))
	fmt.Printf("    %s %s\n", CyanStyle.Render("isolator"), DescStyle.Render("<command> [flags]"))
	fmt.Println()
	fmt.Println(SectionStyle.Render("  Commands"))

	cmds := []struct{ name, args, desc string }{
		{"init", "", "First-run setup: config, PATH check, GPU/audio detection"},
		{"install", "<pkg>", "Install a package into a Podman container"},
		{"remove", "<pkg>", "Remove an installed package"},
		{"exec", "<pkg> -- <cmd>", "Run an arbitrary command inside a package's container"},
		{"search", "<term>", "Fuzzy-search the repository for packages"},
		{"info", "<pkg>", "Show detailed info about a package"},
		{"list", "", "List all installed packages"},
		{"status", "", "Show container status dashboard"},
		{"update", "", "Update packages in all managed containers"},
		{"refresh", "", "Force re-download of the repository list"},
		{"upgrade", "", "Full system upgrade (host + containers)"},
		{"autoremove", "", "Remove orphaned containers with no packages left"},
		{"clean", "", "Prune dangling Podman images and build cache"},
		{"snapshot", "<container>", "Save a rollback point for a container"},
		{"rollback", "<container>", "Restore a container from its latest snapshot"},
		{"snapshots", "", "List saved snapshots"},
	}
	for _, c := range cmds {
		args := ""
		if c.args != "" {
			args = " " + DimStyle.Render(c.args)
		}
		fmt.Printf("    %s%s\n      %s\n\n",
			CmdStyle.Render(c.name), args, DescStyle.Render(c.desc))
	}

	fmt.Println(SectionStyle.Render("  Flags"))
	fmt.Printf("    %s      install package in isolated container with its own home\n", FlagStyle.Render("--isolated"))
	fmt.Printf("    %s        remove even if another installed package depends on it\n", FlagStyle.Render("--force"))
	fmt.Println()
	fmt.Println(SectionStyle.Render("  Config"))
	fmt.Printf("    %s\n", DescStyle.Render("~/.config/isolator/config.hk — GPU mode, audio backend, themes,"))
	fmt.Printf("    %s\n", DescStyle.Render("desktop-launcher creation, checksum enforcement, and more."))
	fmt.Println()
	fmt.Println(SectionStyle.Render("  Examples"))
	exs := []string{
		"isolator init",
		"isolator install firefox",
		"isolator install steam --isolated",
		"isolator exec firefox -- bash",
		"isolator search browser",
		"isolator info gimp",
		"isolator snapshot fedora",
		"isolator rollback fedora",
		"isolator autoremove",
	}
	for _, e := range exs {
		fmt.Printf("    %s\n", DimStyle.Render(e))
	}
	fmt.Println()
	fmt.Printf("  Use %s for command-specific help.\n\n",
		CyanStyle.Render("isolator <command> --help"))
}
