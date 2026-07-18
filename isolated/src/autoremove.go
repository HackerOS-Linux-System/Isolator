package src

import (
	"fmt"
)

// HandleAutoremove finds containers managed by Isolator that no longer have
// any package installed in them (e.g. the last package sharing a distro
// container was removed) and deletes them, freeing disk space.
func HandleAutoremove(dryRun bool) {
	installed, err := LoadInstalled()
	if err != nil {
		PrintError("Failed to load installed packages")
		return
	}

	inUse := map[string]bool{}
	for _, ip := range installed {
		inUse[ip.Cont] = true
	}

	ours := GetOurContainers()
	var orphans []string
	for _, name := range ours {
		if !inUse[name] {
			orphans = append(orphans, name)
		}
	}

	if len(orphans) == 0 {
		PrintInfo("No orphaned containers to remove")
		return
	}

	if dryRun {
		PrintInfo(fmt.Sprintf("[dry-run] Would remove %d orphaned container(s):", len(orphans)))
		for _, o := range orphans {
			fmt.Println("  " + o)
		}
		PrintInfo("[dry-run] No changes made")
		return
	}

	PrintInfo(fmt.Sprintf("Found %d orphaned container(s):", len(orphans)))
	for _, o := range orphans {
		fmt.Println("  " + DimStyle.Render(o))
	}

	for _, o := range orphans {
		PrintStep("Removing " + o + "...")
		if ExecCommand(podmanBin, []string{"rm", "--force", o}) {
			PrintSuccess("Removed " + o)
		} else {
			PrintError("Failed to remove " + o)
		}
	}
}
