package src

import (
	"fmt"
	"strings"
	"sync"
)

func HandleUpdate() {
	PrintInfo("Updating all managed containers...")
	conts := GetOurContainers()
	if len(conts) == 0 {
		PrintWarn("No managed containers found")
		return
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := map[string]bool{}

	for _, cont := range conts {
		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			distroName := ""
			for d, dd := range Distros {
				if c == dd.ContName || strings.HasPrefix(c, dd.ContName+"-") {
					distroName = d
					break
				}
			}
			if distroName == "" {
				return
			}
			// Ensure container is running before update
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
