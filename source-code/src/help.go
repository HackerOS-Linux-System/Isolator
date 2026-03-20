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
		{"install", "<pkg>", "Install a package into a Podman container"},
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
			args = " " + DimStyle.Render(c.args)
		}
		fmt.Printf("    %s%s\n      %s\n\n",
			   CmdStyle.Render(c.name), args, DescStyle.Render(c.desc))
	}

	fmt.Println(SectionStyle.Render("  Flags"))
	fmt.Printf("    %s   auto-confirm image pull (default on)\n", FlagStyle.Render("--yes"))
	fmt.Printf("    %s   install package in isolated container with its own home\n", FlagStyle.Render("--isolated"))
	fmt.Println()
	fmt.Println(SectionStyle.Render("  Examples"))
	exs := []string{
		"isolator install firefox",
		"isolator install steam --isolated",
		"isolator search browser",
		"isolator info gimp",
		"isolator list",
		"isolator update",
	}
	for _, e := range exs {
		fmt.Printf("    %s\n", DimStyle.Render(e))
	}
	fmt.Println()
	fmt.Printf("  Use %s for command-specific help.\n\n",
		   CyanStyle.Render("isolator <command> --help"))
}
