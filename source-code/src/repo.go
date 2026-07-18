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

// httpClient is a hardened client: bounded timeout so a hung/slow endpoint
// can never freeze the CLI.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

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
		if !downloadRepo(repoFilePath) {
			return false
		}
	}

	// Validate readable + validate every package name in it.
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
	for _, p := range pkgs {
		if err := ValidatePackageName(p.Name); err != nil {
			PrintError("Repository contains an unsafe package entry: " + err.Error())
			PrintInfo("Refusing to trust this repo cache — run 'isolator refresh'")
			os.Remove(repoFilePath)
			return false
		}
	}
	return true
}

// downloadRepo fetches package-list.json (and, best-effort, its .sha256
// sidecar) and atomically replaces the local cache. If a checksum file is
// published, it MUST match or the download is rejected. If no checksum file
// exists at all (404), we proceed but warn.
func downloadRepo(repoFilePath string) bool {
	s := spinner.New(spinner.CharSets[14], 80*time.Millisecond)
	s.Suffix = " Downloading repository list..."
	s.Color("cyan")
	s.Start()
	defer s.Stop()

	body, err := fetchURL(repoURL)
	if err != nil {
		PrintError("Failed to download repo list: " + err.Error())
		return false
	}

	var validate []PackageInfo
	if err := json.Unmarshal(body, &validate); err != nil {
		PrintError("Downloaded file is invalid JSON: " + err.Error())
		return false
	}
	for _, p := range validate {
		if err := ValidatePackageName(p.Name); err != nil {
			PrintError("Downloaded repo contains an unsafe entry: " + err.Error())
			return false
		}
	}

	sumBody, sumErr := fetchURL(repoURL + ".sha256")
	switch {
	case sumErr == nil:
		if !VerifyChecksum(body, string(sumBody)) {
			PrintError("Checksum mismatch for repository list — refusing to install a possibly tampered file")
			return false
		}
	default:
		if LoadConfig().RequireChecksum {
			PrintError("No checksum published for the repository list, and require_checksum is enabled — refusing download")
			return false
		}
		PrintWarn("No checksum published for the repository list (repo.sha256 missing) — integrity not verified")
	}

	tmpPath := repoFilePath + ".tmp"
	if err := os.WriteFile(tmpPath, body, 0600); err != nil {
		PrintError("Failed to write repo file")
		return false
	}
	if err := os.Rename(tmpPath, repoFilePath); err != nil {
		os.Remove(tmpPath)
		PrintError("Failed to save repo file")
		return false
	}
	PrintSuccess(fmt.Sprintf("Repository list updated (%d packages)", len(validate)))
	return true
}

// fetchURL performs a bounded GET and returns the body, or an error for any
// non-200 response.
func fetchURL(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", fmt.Sprintf("isolator/%s (+https://github.com/HackerOS-Linux-System/Isolator)", Version))

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, 32<<20) // 32MB hard cap, repo list is tiny
	return io.ReadAll(limited)
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
