package src

import (
	"github.com/charmbracelet/bubbles/table"
)

func HandleList() {
	installed, err := LoadInstalled()
	if err != nil {
		PrintError("Failed to load installed packages")
		return
	}
	if len(installed) == 0 {
		PrintInfo("No packages installed yet")
		return
	}
	columns := []table.Column{
		{Title: "Package", Width: 22},
		{Title: "Distro", Width: 14},
		{Title: "Type", Width: 6},
		{Title: "Container", Width: 28},
		{Title: "Isolated", Width: 9},
	}
	var rows []table.Row
	for _, ip := range installed {
		iso := "no"
		if ip.Isolated {
			iso = "yes"
		}
		rows = append(rows, []string{ip.Pkg, ip.Distro, ip.Type, ip.Cont, iso})
	}
	RunTable("Installed Packages", columns, rows)
}
