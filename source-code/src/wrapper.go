package src

import (
	"fmt"
	"os"
	"path/filepath"
)

func CreateWrapper(pkg, contName string) bool {
	binDir := filepath.Join(os.Getenv("HOME"), ".local/bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return false
	}
	filePath := filepath.Join(binDir, pkg)
	content := fmt.Sprintf("#!/bin/sh\nexec %s exec -it %s %s \"$@\"\n", podmanBin, contName, pkg)
	if err := os.WriteFile(filePath, []byte(content), 0755); err != nil {
		return false
	}
	return true
}

func RemoveWrapper(pkg string) bool {
	filePath := filepath.Join(os.Getenv("HOME"), ".local/bin", pkg)
	err := os.Remove(filePath)
	return err == nil || os.IsNotExist(err)
}
