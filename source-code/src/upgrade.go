package src

import (
	"os"
)

func HandleUpgrade() {
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
	HandleUpdate()
	PrintSuccess("Upgrade complete")
}
