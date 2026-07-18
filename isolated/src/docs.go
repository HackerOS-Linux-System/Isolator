package src

import (
	"fmt"
	"os/exec"
	"runtime"
)

const docsURL = "https://hackeros-linux-system.github.io/HackerOS-Website/tools-docs/isolator.html"

// HandleDocs opens Isolator's online documentation in the user's default
// browser. It tries the standard opener for each platform in turn
// (Isolator is Linux-focused, but there's no harm in the macOS/Windows
// fallbacks for anyone building it elsewhere) and, if none of them work
// (headless server, no GUI, no opener installed), just prints the URL so
// the person can open it themselves — never a hard failure for something
// this low-stakes.
func HandleDocs() {
	PrintInfo("Opening Isolator documentation: " + docsURL)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", docsURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", docsURL)
	default:
		cmd = exec.Command("xdg-open", docsURL)
	}

	if err := cmd.Start(); err != nil {
		PrintWarn("Couldn't launch a browser automatically: " + err.Error())
		fmt.Println("Open this URL manually: " + docsURL)
	}
}
