package src

import (
	"os"
	"path/filepath"
)

// HandleClean prunes dangling Podman images/build cache and sweeps leftover
// .tmp files from the config directory (interrupted writes).
func HandleClean(dryRun bool) {
	if dryRun {
		PrintInfo("[dry-run] Would run:")
		PrintInfo("  - podman image prune -f")
		PrintInfo("  - podman system prune -f")
		dir := filepath.Join(os.Getenv("HOME"), configDir)
		if entries, err := os.ReadDir(dir); err == nil {
			count := 0
			for _, e := range entries {
				if filepath.Ext(e.Name()) == ".tmp" {
					count++
				}
			}
			if count > 0 {
				PrintInfo("  - remove " + itoa(count) + " leftover .tmp file(s) from " + dir)
			}
		}
		PrintInfo("[dry-run] No changes made")
		return
	}

	PrintInfo("Cleaning up Podman image cache...")
	if ExecCommand(podmanBin, []string{"image", "prune", "-f"}) {
		PrintSuccess("Dangling images removed")
	} else {
		PrintWarn("podman image prune reported an issue (continuing)")
	}

	PrintStep("Pruning build cache...")
	ExecCommand(podmanBin, []string{"system", "prune", "-f"})

	dir := filepath.Join(os.Getenv("HOME"), configDir)
	entries, err := os.ReadDir(dir)
	if err == nil {
		removed := 0
		for _, e := range entries {
			if filepath.Ext(e.Name()) == ".tmp" {
				if os.Remove(filepath.Join(dir, e.Name())) == nil {
					removed++
				}
			}
		}
		if removed > 0 {
			PrintSuccess("Removed " + itoa(removed) + " leftover temp file(s)")
		}
	}

	PrintSuccess("Clean complete")
}
