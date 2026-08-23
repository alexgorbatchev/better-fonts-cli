package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const (
	AppName         = "better-fonts"
	ConfigFileName  = "config.toml"
	DefaultFont     = "Maple Mono Normal NF"
	DefaultRestart  = true
)

// DefaultApps returns the default list of apps to patch.
func DefaultApps() []string {
	return []string{"*"}
}

// Config represents the user-facing configuration structure.
type Config struct {
	Font       string               `toml:"font"`
	Apps       []string             `toml:"apps"`
	Restart    bool                 `toml:"restart"`
	AppsConfig map[string]AppConfig `toml:"apps_config,omitempty"`
	CustomApps []CustomAppDef       `toml:"custom_apps,omitempty"`
}

// AppConfig represents per-app overrides.
type AppConfig struct {
	Font    string `toml:"font,omitempty"`
	Restart *bool  `toml:"restart,omitempty"`
}

// CustomAppDef allows users to declare custom Electron or Native apps in config.toml.
type CustomAppDef struct {
	ID          string   `toml:"id"`
	Name        string   `toml:"name"`
	Path        string   `toml:"path"`
	Driver      string   `toml:"driver,omitempty"` // "electron" (default) or "native-hook"
	ProcessName string   `toml:"process_name"`
	AsarPath    string   `toml:"asar_path,omitempty"`
	PreloadPath string   `toml:"preload_path,omitempty"`
	UnpackFlags []string `toml:"unpack_flags,omitempty"`
}

// NewDefaultConfig initializes a Config struct with default values.
func NewDefaultConfig() *Config {
	return &Config{
		Font:       DefaultFont,
		Apps:       DefaultApps(),
		Restart:    DefaultRestart,
		AppsConfig: make(map[string]AppConfig),
		CustomApps: []CustomAppDef{},
	}
}

// GetConfigDir returns the XDG config directory for better-fonts.
func GetConfigDir() (string, error) {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, AppName), nil
	}
	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("getting user config dir: %w", err)
	}
	return filepath.Join(baseDir, AppName), nil
}

// GetConfigFilePath returns the full path to config.toml under XDG config dir.
func GetConfigFilePath() (string, error) {
	dir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ConfigFileName), nil
}

// GetCacheDir returns the XDG cache directory for better-fonts.
func GetCacheDir() (string, error) {
	if xdgCache := os.Getenv("XDG_CACHE_HOME"); xdgCache != "" {
		return filepath.Join(xdgCache, AppName), nil
	}
	baseDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("getting user cache dir: %w", err)
	}
	return filepath.Join(baseDir, AppName), nil
}

// LoadConfig reads and decodes a TOML config file from the specified path.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	cfg := NewDefaultConfig()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing TOML config %s: %w", path, err)
	}

	if cfg.Font == "" {
		cfg.Font = DefaultFont
	}
	if len(cfg.Apps) == 0 {
		cfg.Apps = DefaultApps()
	}

	return cfg, nil
}

// SaveConfig encodes and writes the config struct to a TOML file.
func SaveConfig(path string, cfg *Config) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating config directory %s: %w", dir, err)
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("encoding config to TOML: %w", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing config file %s: %w", path, err)
	}

	return nil
}

// DefaultConfigTemplate represents the initial annotated config.toml content.
const DefaultConfigTemplate = `# Better Fonts CLI Configuration
# Location: $XDG_CONFIG_HOME/better-fonts/config.toml (~/.config/better-fonts/config.toml)

# Default font to apply across applications
font = "Maple Mono Normal NF"

# List of applications to patch by default.
# Use ["*"] for all supported applications, or list specific app IDs (e.g. ["slack", "rekordbox", "telegram"]).
apps = ["*"]

# Automatically restart applications after patching or unpatching (default: true)
restart = true

# Per-application overrides (optional)
# [apps_config.slack]
# font = "JetBrains Mono"
# restart = true

# [apps_config.rekordbox]
# font = "Maple Mono Normal NF"

# User-defined custom applications (optional)
# [[custom_apps]]
# id = "custom-electron"
# name = "Custom Electron"
# path = "/Applications/CustomElectron.app"
# driver = "electron"
# process_name = "CustomElectron"
# asar_path = "app.asar"
# preload_path = "dist/preload.js"

# [[custom_apps]]
# id = "custom-native"
# name = "Custom Native"
# path = "/Applications/CustomNative.app"
# driver = "native-hook"
# process_name = "CustomNative"
`

// EnsureConfigFile loads an existing config or creates an annotated default one if it doesn't exist.
func EnsureConfigFile(path string) (*Config, error) {
	if _, err := os.Stat(path); err == nil {
		return LoadConfig(path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("checking config file %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating config directory %s: %w", dir, err)
	}

	if err := os.WriteFile(path, []byte(DefaultConfigTemplate), 0o644); err != nil {
		return nil, fmt.Errorf("writing default config template to %s: %w", path, err)
	}

	return LoadConfig(path)
}

// MatchesApp reports whether the appID matches the configured app list.
func (c *Config) MatchesApp(appID string) bool {
	normTarget := strings.ToLower(strings.TrimSpace(appID))
	for _, a := range c.Apps {
		norm := strings.ToLower(strings.TrimSpace(a))
		if norm == "*" || norm == "all" {
			return true
		}
		if norm == normTarget {
			return true
		}
	}
	return false
}

// EffectiveFont returns the font to use for an app, accounting for overrides.
func (c *Config) EffectiveFont(appID string) string {
	if c.AppsConfig != nil {
		if appCfg, ok := c.AppsConfig[appID]; ok && strings.TrimSpace(appCfg.Font) != "" {
			return appCfg.Font
		}
	}
	if strings.TrimSpace(c.Font) != "" {
		return c.Font
	}
	return DefaultFont
}

// EffectiveRestart returns whether the app should be restarted after patching.
func (c *Config) EffectiveRestart(appID string) bool {
	if c.AppsConfig != nil {
		if appCfg, ok := c.AppsConfig[appID]; ok && appCfg.Restart != nil {
			return *appCfg.Restart
		}
	}
	return c.Restart
}
