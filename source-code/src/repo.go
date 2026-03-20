package src

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/briandowns/spinner"
)

func LoadRepo(force bool) bool {
	if err := EnsureConfigDir(); err != nil {
		PrintError("Failed to create config directory: " + err.Error())
		return false
	}

	repoFilePath := GetRepoFilePath()
	needsDownload := force

	if !needsDownload {
		info, err := os.Stat(repoFilePath)
		if err != nil || time.Since(info.ModTime()) > cacheMaxAge {
			needsDownload = true
		}
	}

	if needsDownload {
		s := spinner.New(spinner.CharSets[14], 80*time.Millisecond)
		s.Suffix = " Downloading repository list..."
		s.Color("cyan")
		s.Start()

		resp, err := http.Get(repoURL)
		if err != nil {
			s.Stop()
			PrintError("Failed to download repo list: " + err.Error())
			return false
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			s.Stop()
			PrintError(fmt.Sprintf("Download failed: HTTP %d", resp.StatusCode))
			return false
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			s.Stop()
			PrintError("Failed to read response body")
			return false
		}

		// Validate JSON before saving
		var validate []PackageInfo
		if err := json.Unmarshal(body, &validate); err != nil {
			s.Stop()
			PrintError("Downloaded file is invalid JSON: " + err.Error())
			return false
		}

		// Atomic write
		tmpPath := repoFilePath + ".tmp"
		if err := os.WriteFile(tmpPath, body, 0600); err != nil {
			s.Stop()
			PrintError("Failed to write repo file")
			return false
		}
		if err := os.Rename(tmpPath, repoFilePath); err != nil {
			s.Stop()
			os.Remove(tmpPath)
			PrintError("Failed to save repo file")
			return false
		}
		s.Stop()
		PrintSuccess(fmt.Sprintf("Repository list updated (%d packages)", len(validate)))
	}

	// Validate readable
	data, err := os.ReadFile(repoFilePath)
	if err != nil {
		PrintError("Failed to read repo file")
		return false
	}
	var pkgs []PackageInfo
	if err := json.Unmarshal(data, &pkgs); err != nil {
		PrintError("Repo cache is corrupted — run 'isolator refresh'")
		os.Remove(repoFilePath)
		return false
	}
	return true
}

func ReadRepoPackages() []PackageInfo {
	data, err := os.ReadFile(GetRepoFilePath())
	if err != nil {
		return nil
	}
	var pkgs []PackageInfo
	json.Unmarshal(data, &pkgs)
	return pkgs
}
