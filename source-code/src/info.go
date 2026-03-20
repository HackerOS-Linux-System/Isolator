package src

import (
	"fmt"
	"strings"
)

func HandleInfo(pkg string) {
	if !LoadRepo(false) {
		return
	}
	repoPackages := ReadRepoPackages()
	for _, p := range repoPackages {
		if p.Name == pkg {
			fmt.Println()
			fmt.Println(TitleStyle.Render(" Package Info "))
			fmt.Println()
			fmt.Printf("  %s  %s\n", BoldStyle.Render("Name:   "), CyanStyle.Render(p.Name))
			fmt.Printf("  %s  %s\n", BoldStyle.Render("Distro: "), MagentaStyle.Render(p.Distro))
			fmt.Printf("  %s  %s\n", BoldStyle.Render("Type:   "), p.Type)
			if len(p.Libs) > 0 {
				fmt.Printf("  %s  %s\n", BoldStyle.Render("Libs:   "), strings.Join(p.Libs, ", "))
			}
			installed, _ := LoadInstalled()
			for _, ip := range installed {
				if ip.Pkg == pkg {
					iso := ""
					if ip.Isolated {
						iso = " (isolated)"
					}
					fmt.Printf("  %s  %s\n", BoldStyle.Render("Status: "), SuccessStyle.Render("installed"+iso))
					fmt.Printf("  %s  %s\n", BoldStyle.Render("Cont:   "), ip.Cont)
					fmt.Println()
					return
				}
			}
			fmt.Printf("  %s  %s\n", BoldStyle.Render("Status: "), DimStyle.Render("not installed"))
			fmt.Println()
			return
		}
	}
	PrintError(fmt.Sprintf("Package '%s' not found", pkg))
}
