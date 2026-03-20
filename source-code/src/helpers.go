package src

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
)

var (
	BoldStyle    = lipgloss.NewStyle().Bold(true)
	ErrorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9"))
	SuccessStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	InfoStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12"))
	WarnStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11"))
	CyanStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	MagentaStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	DimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	TitleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Background(lipgloss.Color("236")).Padding(0, 2)
	CmdStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	DescStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	SectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("11")).MarginTop(1)
	FlagStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

func PrintError(msg string) {
	fmt.Println(ErrorStyle.Render("✗ Error: ") + msg)
}

func PrintInfo(msg string) {
	fmt.Println(InfoStyle.Render("● ") + msg)
}

func PrintSuccess(msg string) {
	fmt.Println(SuccessStyle.Render("✓ ") + msg)
}

func PrintWarn(msg string) {
	fmt.Println(WarnStyle.Render("⚠ ") + msg)
}

func PrintStep(msg string) {
	fmt.Println(CyanStyle.Render("→ ") + msg)
}

func ConfigPath(file string) string {
	return filepath.Join(os.Getenv("HOME"), configDir, file)
}

func GetRepoFilePath() string {
	return ConfigPath(repoFile)
}

func EnsureConfigDir() error {
	return os.MkdirAll(filepath.Join(os.Getenv("HOME"), configDir), 0700)
}
