package src

import (
	"strings"

	"github.com/charmbracelet/bubbles/table"
)

func HandleStatus() {
	installed, _ := LoadInstalled()
	pkgMap := map[string][]string{}
	for _, ip := range installed {
		pkgMap[ip.Cont] = append(pkgMap[ip.Cont], ip.Pkg)
	}

	columns := []table.Column{
		{Title: "Container", Width: 26},
		{Title: "Status", Width: 12},
		{Title: "Size", Width: 18},
		{Title: "Packages", Width: 34},
	}
	var rows []table.Row
	for _, db := range GetContainers() {
		for _, name := range db.Names {
			isOur := false
			for _, base := range Containers {
				if name == base || strings.HasPrefix(name, base+"-") {
					isOur = true
					break
				}
			}
			if !isOur {
				continue
			}
			size := GetContainerSize(name)
			rows = append(rows, []string{name, db.State, size, strings.Join(pkgMap[name], ", ")})
		}
	}
	if len(rows) == 0 {
		PrintInfo("No managed containers found")
		return
	}
	RunTable("Container Status", columns, rows)
}
