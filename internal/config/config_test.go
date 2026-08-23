package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := NewDefaultConfig()
	if cfg.Font != DefaultFont {
		t.Fatalf("expected default font %q, got %q", DefaultFont, cfg.Font)
	}
	if len(cfg.Apps) != 1 || cfg.Apps[0] != "*" {
		t.Fatalf("expected default apps [*], got %v", cfg.Apps)
	}
	if !cfg.Restart {
		t.Fatalf("expected default restart to be true")
	}
	if len(DefaultApps()) != 1 || DefaultApps()[0] != "*" {
		t.Fatalf("DefaultApps() mismatch: %v", DefaultApps())
	}
}

func TestConfig_MatchesApp(t *testing.T) {
	tests := []struct {
		name     string
		cfgApps  []string
		appID    string
		expected bool
	}{
		{"wildcard matches slack", []string{"*"}, "slack", true},
		{"wildcard matches signal", []string{"*"}, "signal", true},
		{"all keyword matches paseo", []string{"all"}, "paseo", true},
		{"specific match", []string{"slack", "signal"}, "slack", true},
		{"specific mismatch", []string{"slack", "signal"}, "paseo", false},
		{"case insensitive match", []string{"Slack", "Signal"}, "slack", true},
		{"case insensitive query", []string{"slack"}, "Slack", true},
		{"empty apps list matches nothing", []string{}, "slack", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Font:    DefaultFont,
				Apps:    tt.cfgApps,
				Restart: true,
			}
			got := cfg.MatchesApp(tt.appID)
			if got != tt.expected {
				t.Fatalf("MatchesApp(%q) with apps %v = %v, want %v", tt.appID, tt.cfgApps, got, tt.expected)
			}
		})
	}
}

func TestConfig_EffectiveFont(t *testing.T) {
	cfg := &Config{
		Font: "Global Font",
		Apps: []string{"*"},
		AppsConfig: map[string]AppConfig{
			"slack": {
				Font: "Custom Slack Font",
			},
		},
	}

	if got := cfg.EffectiveFont("slack"); got != "Custom Slack Font" {
		t.Fatalf("expected %q, got %q", "Custom Slack Font", got)
	}
	if got := cfg.EffectiveFont("signal"); got != "Global Font" {
		t.Fatalf("expected %q, got %q", "Global Font", got)
	}

	// Empty global font falls back to DefaultFont
	emptyCfg := &Config{
		Font: "",
		Apps: []string{"*"},
	}
	if got := emptyCfg.EffectiveFont("anything"); got != DefaultFont {
		t.Fatalf("expected DefaultFont %q, got %q", DefaultFont, got)
	}
}

func TestConfig_EffectiveRestart(t *testing.T) {
	falseVal := false
	trueVal := true
	cfg := &Config{
		Font:    "Global Font",
		Restart: true,
		AppsConfig: map[string]AppConfig{
			"slack": {
				Restart: &falseVal,
			},
			"custom": {
				Restart: &trueVal,
			},
		},
	}

	if got := cfg.EffectiveRestart("slack"); got != false {
		t.Fatalf("expected false, got %v", got)
	}
	if got := cfg.EffectiveRestart("custom"); got != true {
		t.Fatalf("expected true, got %v", got)
	}
	if got := cfg.EffectiveRestart("signal"); got != true {
		t.Fatalf("expected true, got %v", got)
	}
}

func TestLoadAndSaveConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")

	original := &Config{
		Font:    "Fira Code",
		Apps:    []string{"slack", "signal"},
		Restart: false,
		AppsConfig: map[string]AppConfig{
			"slack": {
				Font: "JetBrains Mono",
			},
		},
	}

	if err := SaveConfig(configPath, original); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if loaded.Font != original.Font {
		t.Errorf("Font mismatch: got %q, want %q", loaded.Font, original.Font)
	}
	if len(loaded.Apps) != len(original.Apps) || loaded.Apps[0] != original.Apps[0] || loaded.Apps[1] != original.Apps[1] {
		t.Errorf("Apps mismatch: got %v, want %v", loaded.Apps, original.Apps)
	}
	if loaded.Restart != original.Restart {
		t.Errorf("Restart mismatch: got %v, want %v", loaded.Restart, original.Restart)
	}
	if loaded.AppsConfig["slack"].Font != "JetBrains Mono" {
		t.Errorf("AppConfig font mismatch: got %q, want %q", loaded.AppsConfig["slack"].Font, "JetBrains Mono")
	}

	// Test SaveConfig error on bad directory path
	if err := SaveConfig("/dev/null/impossible/config.toml", original); err == nil {
		t.Fatalf("expected error saving to impossible directory")
	}
}

func TestLoadConfig_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Non-existent file
	if _, err := LoadConfig(filepath.Join(tempDir, "missing.toml")); err == nil {
		t.Fatalf("expected error for missing file")
	}

	// 2. Invalid TOML
	badFile := filepath.Join(tempDir, "bad.toml")
	_ = os.WriteFile(badFile, []byte("invalid = [toml"), 0o644)
	if _, err := LoadConfig(badFile); err == nil {
		t.Fatalf("expected error for invalid TOML")
	}

	// 3. TOML with empty font & empty apps -> defaults applied
	emptyFile := filepath.Join(tempDir, "empty.toml")
	_ = os.WriteFile(emptyFile, []byte("font = \"\"\napps = []\n"), 0o644)
	loaded, err := LoadConfig(emptyFile)
	if err != nil {
		t.Fatalf("LoadConfig on empty fields failed: %v", err)
	}
	if loaded.Font != DefaultFont {
		t.Errorf("expected default font %q, got %q", DefaultFont, loaded.Font)
	}
	if len(loaded.Apps) == 0 || loaded.Apps[0] != "*" {
		t.Errorf("expected default apps [*], got %v", loaded.Apps)
	}
}

func TestXDGDirs(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tempDir, "cache"))

	configDir, err := GetConfigDir()
	if err != nil {
		t.Fatalf("GetConfigDir failed: %v", err)
	}

	expected := filepath.Join(tempDir, AppName)
	if configDir != expected {
		t.Fatalf("GetConfigDir = %q, want %q", configDir, expected)
	}

	configPath, err := GetConfigFilePath()
	if err != nil {
		t.Fatalf("GetConfigFilePath failed: %v", err)
	}
	expectedPath := filepath.Join(expected, ConfigFileName)
	if configPath != expectedPath {
		t.Fatalf("GetConfigFilePath = %q, want %q", configPath, expectedPath)
	}

	cacheDir, err := GetCacheDir()
	if err != nil {
		t.Fatalf("GetCacheDir failed: %v", err)
	}
	expectedCache := filepath.Join(tempDir, "cache", AppName)
	if cacheDir != expectedCache {
		t.Fatalf("GetCacheDir = %q, want %q", cacheDir, expectedCache)
	}

	// Unset env to test fallback
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")

	fallbackConfig, err := GetConfigDir()
	if err != nil || fallbackConfig == "" {
		t.Fatalf("fallback GetConfigDir failed: %v", err)
	}

	fallbackCache, err := GetCacheDir()
	if err != nil || fallbackCache == "" {
		t.Fatalf("fallback GetCacheDir failed: %v", err)
	}

	// Unset HOME to test error return path
	t.Setenv("HOME", "")
	if _, err := GetConfigDir(); err == nil {
		t.Logf("GetConfigDir with empty HOME succeeded or platform handled")
	}
	if _, err := GetConfigFilePath(); err == nil {
		t.Logf("GetConfigFilePath with empty HOME handled")
	}
	if _, err := GetCacheDir(); err == nil {
		t.Logf("GetCacheDir with empty HOME handled")
	}
}

func TestEnsureConfigFile(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "better-fonts", "config.toml")

	// 1. First run -> creates template
	cfg, err := EnsureConfigFile(configPath)
	if err != nil {
		t.Fatalf("EnsureConfigFile failed: %v", err)
	}

	if cfg.Font != DefaultFont {
		t.Errorf("expected font %q, got %q", DefaultFont, cfg.Font)
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading generated config file: %v", err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, DefaultFont) {
		t.Errorf("config missing default font: %s", contentStr)
	}
	if !strings.Contains(contentStr, "apps = [") {
		t.Errorf("config missing apps definition: %s", contentStr)
	}
	if !strings.Contains(contentStr, "# Better Fonts CLI Configuration") {
		t.Errorf("config missing header comment: %s", contentStr)
	}

	// 2. Second run -> loads existing
	cfg2, err := EnsureConfigFile(configPath)
	if err != nil {
		t.Fatalf("EnsureConfigFile on existing file failed: %v", err)
	}
	if cfg2.Font != DefaultFont {
		t.Errorf("expected font %q on re-load, got %q", DefaultFont, cfg2.Font)
	}

	// 3. Error on impossible path
	if _, err := EnsureConfigFile("/dev/null/impossible/config.toml"); err == nil {
		t.Fatalf("expected error on impossible path")
	}
}
