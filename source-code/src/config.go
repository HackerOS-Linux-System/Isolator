package src

import (
	"fmt"
	"os"
	"strings"
)

// configSchema declares every key Isolator actually understands in
// config.hk, per section, and what shape its value should be. It's used by
// ValidateConfigDoc so a typo'd key or a wrong-shaped value produces a
// clear warning instead of silently and invisibly falling back to the
// default — the previous behavior, which made typos essentially
// undebuggable ("I set gpu_mdoe and nothing changed... why?").
var configSchema = map[string]map[string]string{
	"general": {
		"default_isolated": "bool",
	},
	"gui": {
		"enable":                     "bool",
		"gpu_mode":                   "enum:auto,nvidia,amd,intel,none",
		"audio_backend":              "enum:auto,pipewire,pulseaudio,alsa,none",
		"gtk_theme":                  "string",
		"icon_theme":                 "string",
		"qt_platform":                "string",
		"shm_size":                   "string",
		"create_desktop_entries":     "bool",
		"allow_desktop_environments": "bool",
		"allow_system_containers":    "bool",
	},
	"security": {
		"require_checksum": "bool",
	},
}

// ValidateConfigDoc checks doc against configSchema and returns one
// human-readable warning per problem found: unknown sections, unknown
// keys within a known section (the classic typo case), and values whose
// shape doesn't match what's expected (e.g. a string where a bool was
// declared, or an enum value outside its allowed set). It never mutates
// doc or fails the caller — LoadConfig prints these and moves on, falling
// back to defaults for anything that didn't validate, exactly as before.
func ValidateConfigDoc(doc *HkDocument) []string {
	var warnings []string
	for _, secName := range doc.Sections.Keys() {
		schema, knownSection := configSchema[secName]
		secVal, _ := doc.Sections.Get(secName)
		if !knownSection {
			warnings = append(warnings, fmt.Sprintf("unknown config section [%s] — ignored", secName))
			continue
		}
		if secVal.Kind != HkMapKind {
			continue
		}
		for _, key := range secVal.MapVal.Keys() {
			kind, known := schema[key]
			if !known {
				warnings = append(warnings, fmt.Sprintf("unknown config key '%s' in [%s] — ignored (typo?)", key, secName))
				continue
			}
			v, _ := secVal.MapVal.Get(key)
			switch {
			case kind == "bool":
				if v.Kind != HkBool {
					warnings = append(warnings, fmt.Sprintf("[%s] -> %s should be true/false, got %s — using default", secName, key, hkKindName(v.Kind)))
				}
			case kind == "string":
				if v.Kind != HkString {
					warnings = append(warnings, fmt.Sprintf("[%s] -> %s should be a plain string, got %s — using default", secName, key, hkKindName(v.Kind)))
				}
			case strings.HasPrefix(kind, "enum:"):
				options := strings.Split(strings.TrimPrefix(kind, "enum:"), ",")
				s, err := v.AsString()
				if err != nil || !stringInSlice(s, options) {
					warnings = append(warnings, fmt.Sprintf("[%s] -> %s should be one of %s, got %q — using default", secName, key, strings.Join(options, "|"), s))
				}
			}
		}
	}
	return warnings
}

func hkKindName(k HkKind) string {
	switch k {
	case HkString:
		return "a string"
	case HkNumber:
		return "a number"
	case HkBool:
		return "a bool"
	case HkArray:
		return "an array"
	case HkMapKind:
		return "a map"
	default:
		return "an unknown type"
	}
}

func stringInSlice(s string, options []string) bool {
	for _, o := range options {
		if s == o {
			return true
		}
	}
	return false
}

// Config holds user-tunable behaviour, persisted at
// ~/.config/isolator/config.hk (the .hk format — see hk.go — replaces
// JSON/YAML for every local Isolator config/state file).
type Config struct {
	DefaultIsolated bool

	// --- Graphics / desktop integration ---------------------------------
	EnableGUI                bool
	GPUMode                  string // "auto" | "nvidia" | "amd" | "intel" | "none"
	AudioBackend             string // "auto" | "pipewire" | "pulseaudio" | "alsa" | "none"
	GTKTheme                 string
	IconTheme                string
	QtPlatform               string
	ShmSize                  string
	CreateDesktopEntries     bool
	AllowDesktopEnvironments bool
	AllowSystemContainers    bool

	// --- Safety -----------------------------------------------------------
	RequireChecksum bool
}

func DefaultConfig() Config {
	return Config{
		DefaultIsolated:          false,
		EnableGUI:                true,
		GPUMode:                  "auto",
		AudioBackend:             "auto",
		GTKTheme:                 "",
		IconTheme:                "",
		QtPlatform:               "gtk3",
		ShmSize:                  "1g",
		CreateDesktopEntries:     true,
		AllowDesktopEnvironments: false,
		AllowSystemContainers:    false,
		RequireChecksum:          false,
	}
}

func configFilePath() string {
	return ConfigPath("config.hk")
}

// LoadConfig reads config.hk, creating it with defaults if missing or
// unreadable. It never fails the caller — worst case it returns defaults.
func LoadConfig() Config {
	cfg := DefaultConfig()
	if err := EnsureConfigDir(); err != nil {
		return cfg
	}
	path := configFilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = SaveConfig(cfg)
		return cfg
	}
	doc, err := LoadHKFile(path)
	if err != nil {
		PrintWarn("Config file is corrupted (" + err.Error() + "), using defaults — run 'isolator init' to reset it")
		return cfg
	}
	if err := ResolveInterpolations(doc); err != nil {
		PrintWarn("Config interpolation error: " + err.Error())
	}
	for _, w := range ValidateConfigDoc(doc) {
		PrintWarn("config.hk: " + w)
	}

	general := doc.Section("general")
	cfg.DefaultIsolated = hkGetBool(general, "default_isolated", cfg.DefaultIsolated)

	gui := doc.Section("gui")
	cfg.EnableGUI = hkGetBool(gui, "enable", cfg.EnableGUI)
	cfg.GPUMode = hkGetString(gui, "gpu_mode", cfg.GPUMode)
	cfg.AudioBackend = hkGetString(gui, "audio_backend", cfg.AudioBackend)
	cfg.GTKTheme = hkGetString(gui, "gtk_theme", cfg.GTKTheme)
	cfg.IconTheme = hkGetString(gui, "icon_theme", cfg.IconTheme)
	cfg.QtPlatform = hkGetString(gui, "qt_platform", cfg.QtPlatform)
	cfg.ShmSize = hkGetString(gui, "shm_size", cfg.ShmSize)
	cfg.CreateDesktopEntries = hkGetBool(gui, "create_desktop_entries", cfg.CreateDesktopEntries)
	cfg.AllowDesktopEnvironments = hkGetBool(gui, "allow_desktop_environments", cfg.AllowDesktopEnvironments)
	cfg.AllowSystemContainers = hkGetBool(gui, "allow_system_containers", cfg.AllowSystemContainers)

	security := doc.Section("security")
	cfg.RequireChecksum = hkGetBool(security, "require_checksum", cfg.RequireChecksum)

	return cfg
}

// SaveConfig writes cfg to config.hk atomically.
func SaveConfig(cfg Config) error {
	if err := EnsureConfigDir(); err != nil {
		return err
	}

	doc := NewHkDocument()

	general := doc.Section("general")
	general.Set("default_isolated", hkBoolV(cfg.DefaultIsolated))

	gui := doc.Section("gui")
	gui.Set("enable", hkBoolV(cfg.EnableGUI))
	gui.Set("gpu_mode", hkStr(cfg.GPUMode))
	gui.Set("audio_backend", hkStr(cfg.AudioBackend))
	gui.Set("gtk_theme", hkStr(cfg.GTKTheme))
	gui.Set("icon_theme", hkStr(cfg.IconTheme))
	gui.Set("qt_platform", hkStr(cfg.QtPlatform))
	gui.Set("shm_size", hkStr(cfg.ShmSize))
	gui.Set("create_desktop_entries", hkBoolV(cfg.CreateDesktopEntries))
	gui.Set("allow_desktop_environments", hkBoolV(cfg.AllowDesktopEnvironments))
	gui.Set("allow_system_containers", hkBoolV(cfg.AllowSystemContainers))

	security := doc.Section("security")
	security.Set("require_checksum", hkBoolV(cfg.RequireChecksum))

	return WriteHKFile(configFilePath(), doc)
}
