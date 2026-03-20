package src

import (
	"time"
)

const (
	repoURL       = "https://raw.githubusercontent.com/HackerOS-Linux-System/Isolator/main/repo/package-list.json"
	podmanBin     = "podman"
	configDir     = ".config/isolator"
	installedFile = "installed.json"
	repoFile      = "package-list.json"
	homesDir      = ".isolator/homes"
	cacheMaxAge   = 24 * time.Hour
)
