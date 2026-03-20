package src

import (
	"encoding/json"
	"os"
)

func LoadInstalled() ([]InstalledPackage, error) {
	if err := EnsureConfigDir(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(ConfigPath(installedFile))
	if err != nil {
		return []InstalledPackage{}, nil
	}
	var installed []InstalledPackage
	return installed, json.Unmarshal(data, &installed)
}

func SaveInstalled(installed []InstalledPackage) error {
	file := ConfigPath(installedFile)
	data, err := json.MarshalIndent(installed, "", "  ")
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
