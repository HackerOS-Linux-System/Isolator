package src

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/table"
)

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
	var rows []table.Row
	for _, p := range repoPackages {
		if strings.Contains(strings.ToLower(p.Name), strings.ToLower(term)) {
			rows = append(rows, []string{p.Name, p.Distro, p.Type, strings.Join(p.Libs, ", ")})
		}
	}
	if len(rows) == 0 {
		PrintError(fmt.Sprintf("No packages matching '%s'", term))
		return
	}
	PrintInfo(fmt.Sprintf("Found %d result(s) for '%s'", len(rows), term))
	RunTable(fmt.Sprintf("Search: %s", term), columns, rows)
}
