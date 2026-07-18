package src

import (
	"time"
)

const (
	Version       = "0.7"
	repoURL       = "https://raw.githubusercontent.com/HackerOS-Linux-System/Isolator/main/repo/package-list.json"
	podmanBin     = "podman"
	configDir     = ".config/isolated"
	installedFile = "installed.hk"
	repoFile      = "package-list.json"
	homesDir      = ".isolated/homes"
	cacheMaxAge   = 24 * time.Hour
)
