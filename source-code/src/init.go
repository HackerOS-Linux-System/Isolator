package src

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HandleInit prepares a fresh Isolator install: config directory, default
// config.hk, ~/.local/bin (and a PATH hint if it's missing), and a report
// of what graphics/audio/GPU capabilities were auto-detected. This is what
// install.hl invokes right after dropping the binary into /usr/bin.
func HandleInit() {
	PrintInfo("Initializing Isolator...")

	if err := EnsureConfigDir(); err != nil {
		PrintError("Failed to create config directory: " + err.Error())
		return
	}

	if _, err := os.Stat(configFilePath()); os.IsNotExist(err) {
		if err := SaveConfig(DefaultConfig()); err != nil {
			PrintError("Failed to write default config: " + err.Error())
			return
		}
		PrintSuccess("Default config created at " + configFilePath())
	} else {
		PrintInfo("Config already exists at " + configFilePath() + " (untouched)")
	}

	binDir := filepath.Join(os.Getenv("HOME"), ".local/bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		PrintError("Failed to create " + binDir)
	} else {
		PrintSuccess("Wrapper directory ready: " + binDir)
	}
	if !strings.Contains(os.Getenv("PATH"), binDir) {
		PrintWarn(fmt.Sprintf("%s is not on your PATH — add this to your shell rc:", binDir))
		fmt.Println(DimStyle.Render(`  export PATH="$HOME/.local/bin:$PATH"`))
	}

	if err := os.MkdirAll(desktopEntryDir(), 0755); err == nil {
		PrintSuccess("Application launcher directory ready: " + desktopEntryDir())
	}

	// Pre-flight the repo cache so the very first `install` is instant.
	if LoadRepo(false) {
		PrintSuccess("Repository list is ready")
	}

	PrintGPUReport()
	PrintSuccess("Isolator is ready. Try: isolator search <term>")
}
