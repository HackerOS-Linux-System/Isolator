package src

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	"github.com/sahilm/fuzzy"
)

// searchSource adapts []PackageInfo to fuzzy.Source (fuzzy matches by name).
type searchSource []PackageInfo

func (s searchSource) String(i int) string { return s[i].Name }
func (s searchSource) Len() int            { return len(s) }

func HandleSearch(term string) {
	if !LoadRepo(false) {
		return
	}
	repoPackages := ReadRepoPackages()

	columns := []table.Column{
		{Title: "Name", Width: 24},
		{Title: "Distro", Width: 14},
		{Title: "Type", Width: 6},
		{Title: "Dependencies", Width: 34},
	}

	if strings.EqualFold(term, "all") {
		sorted := make([]PackageInfo, len(repoPackages))
		copy(sorted, repoPackages)
		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Distro != sorted[j].Distro {
				return sorted[i].Distro < sorted[j].Distro
			}
			return sorted[i].Name < sorted[j].Name
		})
		rows := make([]table.Row, 0, len(sorted))
		for _, p := range sorted {
			rows = append(rows, table.Row{p.Name, p.Distro, p.Type, strings.Join(p.Libs, ", ")})
		}
		PrintInfo(fmt.Sprintf("%d package(s) in the repository", len(rows)))
		RunTable("All packages", columns, rows)
		return
	}

	matches := fuzzy.FindFrom(term, searchSource(repoPackages))

	var rows []table.Row
	if len(matches) > 0 {
		for _, m := range matches {
			p := repoPackages[m.Index]
			rows = append(rows, table.Row{p.Name, p.Distro, p.Type, strings.Join(p.Libs, ", ")})
		}
	} else {
		// Fuzzy scoring found nothing (e.g. very short/odd query) — fall
		// back to plain substring matching so we never under-deliver
		// compared to the old behaviour.
		lowerTerm := strings.ToLower(term)
		for _, p := range repoPackages {
			if strings.Contains(strings.ToLower(p.Name), lowerTerm) {
				rows = append(rows, table.Row{p.Name, p.Distro, p.Type, strings.Join(p.Libs, ", ")})
			}
		}
	}

	if len(rows) == 0 {
		PrintError(fmt.Sprintf("No packages matching '%s'", term))
		return
	}
	PrintInfo(fmt.Sprintf("Found %d result(s) for '%s'", len(rows), term))
	RunTable(fmt.Sprintf("Search: %s", term), columns, rows)
}
