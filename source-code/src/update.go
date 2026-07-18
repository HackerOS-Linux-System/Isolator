package src

import (
	"fmt"
	"strings"
	"sync"
)

func HandleUpdate(dryRun bool) {
	conts := GetOurContainers()
	if len(conts) == 0 {
		PrintWarn("No managed containers found")
		return
	}

	if dryRun {
		PrintInfo(fmt.Sprintf("[dry-run] Would update %d managed container(s):", len(conts)))
		for _, cont := range conts {
			distroName := distroNameForContainer(cont)
			if distroName == "" {
				fmt.Printf("  %s  %s\n", cont, DimStyle.Render("(unknown distro — would be skipped)"))
				continue
			}
			fmt.Printf("  %s  %s\n", cont, DimStyle.Render(Distros[distroName].Adapter.Update()))
		}
		PrintInfo("[dry-run] No changes made")
		return
	}

	PrintInfo("Updating all managed containers...")
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := map[string]bool{}

	for _, cont := range conts {
		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			distroName := distroNameForContainer(c)
			if distroName == "" {
				return
			}
			if !EnsureContainerRunning(c) {
				mu.Lock()
				results[c] = false
				mu.Unlock()
				return
			}
			ok := ExecInContainer(c, Distros[distroName].Adapter.Update(), false, true)
			mu.Lock()
			results[c] = ok
			mu.Unlock()
		}(cont)
	}
	wg.Wait()

	for cont, ok := range results {
		if ok {
			PrintSuccess(fmt.Sprintf("Updated: %s", cont))
		} else {
			PrintError(fmt.Sprintf("Update failed: %s", cont))
		}
	}
}

// distroNameForContainer maps a running container's name back to the
// catalog distro key that produced it (handles both shared containers,
// e.g. "debian-testing", and isolated ones, e.g. "debian-testing-firefox").
func distroNameForContainer(cont string) string {
	for d, dd := range Distros {
		if cont == dd.ContName || strings.HasPrefix(cont, dd.ContName+"-") {
			return d
		}
	}
	return ""
}
