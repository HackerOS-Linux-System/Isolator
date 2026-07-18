package src

import (
	"fmt"
	"os"
)

func HandleUpgrade(dryRun bool) {
	if dryRun {
		PrintInfo("[dry-run] Would run full system upgrade:")
		if _, err := os.Stat("/usr/bin/apt"); err == nil {
			PrintWarn("[dry-run] System apt found in /usr/bin/ — the real run would abort here (potential conflict)")
			return
		}
		if _, err := os.Stat("/usr/lib/isolator/apt"); os.IsNotExist(err) {
			PrintWarn("[dry-run] Isolator apt not found at /usr/lib/isolator/apt — the real run would abort here")
			return
		}
		fmt.Println("  - sudo /usr/lib/isolator/apt update")
		fmt.Println("  - sudo /usr/lib/isolator/apt upgrade -y")
		HandleUpdate(true)
		PrintInfo("[dry-run] No changes made")
		return
	}

	PrintInfo("Running full system upgrade...")
	if _, err := os.Stat("/usr/bin/apt"); err == nil {
		PrintError("System apt found in /usr/bin/ — potential conflict")
		return
	}
	if _, err := os.Stat("/usr/lib/isolator/apt"); os.IsNotExist(err) {
		PrintError("Isolator apt not found at /usr/lib/isolator/apt")
		return
	}
	ExecCommand("sudo", []string{"/usr/lib/isolator/apt", "update"})
	ExecCommand("sudo", []string{"/usr/lib/isolator/apt", "upgrade", "-y"})
	HandleUpdate(false)
	PrintSuccess("Upgrade complete")
}
