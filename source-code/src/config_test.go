package src

import "testing"

func TestValidateConfigDocCatchesTypo(t *testing.T) {
	doc, err := ParseHK(`[gui]
-> gpu_mdoe => nvidia
`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	warnings := ValidateConfigDoc(doc)
	found := false
	for _, w := range warnings {
		if contains(w, "gpu_mdoe") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a warning about unknown key 'gpu_mdoe', got %v", warnings)
	}
}

func TestValidateConfigDocCatchesBadEnum(t *testing.T) {
	doc, err := ParseHK(`[gui]
-> gpu_mode => radeon
`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	warnings := ValidateConfigDoc(doc)
	if len(warnings) != 1 {
		t.Fatalf("expected exactly 1 warning, got %v", warnings)
	}
	if !contains(warnings[0], "gpu_mode") {
		t.Errorf("expected warning to mention gpu_mode, got %q", warnings[0])
	}
}

func TestValidateConfigDocCatchesWrongType(t *testing.T) {
	doc, err := ParseHK(`[gui]
-> enable => "yes"
`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	warnings := ValidateConfigDoc(doc)
	if len(warnings) != 1 {
		t.Fatalf("expected exactly 1 warning, got %v", warnings)
	}
}

func TestValidateConfigDocAcceptsValidConfig(t *testing.T) {
	cfg := DefaultConfig()
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	defer SaveConfig(DefaultConfig())

	doc, err := LoadHKFile(configFilePath())
	if err != nil {
		t.Fatalf("failed to reload saved config: %v", err)
	}
	warnings := ValidateConfigDoc(doc)
	if len(warnings) != 0 {
		t.Fatalf("expected a freshly-saved default config to validate cleanly, got %v", warnings)
	}
}

func TestValidateConfigDocCatchesUnknownSection(t *testing.T) {
	doc, err := ParseHK(`[gpu]
-> mode => nvidia
`)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	warnings := ValidateConfigDoc(doc)
	if len(warnings) != 1 || !contains(warnings[0], "[gpu]") {
		t.Fatalf("expected 1 warning about unknown section [gpu], got %v", warnings)
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
