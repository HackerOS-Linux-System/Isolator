package src

import (
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tableModel struct {
	table table.Model
	title string
}

func (m tableModel) Init() tea.Cmd { return nil }

func (m tableModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
				case "q", "esc", "ctrl+c":
					return m, tea.Quit
			}
	}
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m tableModel) View() string {
	titleBar := TitleStyle.Render(" " + m.title + " ")
	footer := DimStyle.Render("  ↑/↓ navigate   q quit")
	return "\n" + titleBar + "\n\n" + m.table.View() + "\n\n" + footer + "\n"
}

func buildStyledTable(columns []table.Column, rows []table.Row, height int) table.Model {
	t := table.New(
		table.WithColumns(columns),
		       table.WithRows(rows),
		       table.WithFocused(true),
		       table.WithHeight(height),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240")).
	BorderBottom(true).
	Bold(true).
	Foreground(lipgloss.Color("14"))
	s.Selected = s.Selected.
	Foreground(lipgloss.Color("230")).
	Background(lipgloss.Color("57")).
	Bold(true)
	t.SetStyles(s)
	return t
}

func RunTable(title string, columns []table.Column, rows []table.Row) {
	if len(rows) == 0 {
		PrintInfo("No results")
		return
	}
	height := len(rows)
	if height > 20 {
		height = 20
	}
	t := buildStyledTable(columns, rows, height)
	m := tableModel{table: t, title: title}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		// Fallback: plain text
		for _, r := range rows {
			println(r[0], r[1], r[2], r[3])
		}
	}
}
