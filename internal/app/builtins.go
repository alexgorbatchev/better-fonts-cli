package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// firstExistingPath returns the first candidate that exists, or the first candidate when none do.
func firstExistingPath(candidates []string) string {
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
}

// resolveRekordboxPath locates the installed Rekordbox app bundle.
func resolveRekordboxPath() string {
	return firstExistingPath([]string{
		"/Applications/rekordbox 7/rekordbox.app",
		"/Applications/rekordbox 6/rekordbox.app",
		"/Applications/rekordbox.app",
	})
}

// resolveApplicationsPath locates an app bundle in /Applications, falling back to ~/Applications.
func resolveApplicationsPath(bundleName string) string {
	candidates := []string{filepath.Join("/Applications", bundleName)}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "Applications", bundleName))
	}
	return firstExistingPath(candidates)
}

// GetBuiltinApps returns the list of standard supported Electron and Native applications.
func GetBuiltinApps() []App {
	return []App{
		{
			ID:             "paseo",
			Name:           "Paseo",
			AppPath:        "/Applications/Paseo.app",
			ProcessName:    "Paseo",
			Driver:         DriverElectron,
			PatchMarker:    "fonted-paseo-patch",
			PreloadRelPath: "dist/preload.js",
			ResolveAsarPath: func(appPath string) (string, error) {
				return filepath.Join(appPath, "Contents", "Resources", "app.asar"), nil
			},
			DisableFuses:  true,
			NeedsCodesign: true,
		},
		{
			ID:             "signal",
			Name:           "Signal",
			AppPath:        "/Applications/Signal.app",
			ProcessName:    "Signal",
			Driver:         DriverElectron,
			PatchMarker:    "fonted-signal-patch",
			PreloadRelPath: "preload.bundle.js",
			ResolveAsarPath: func(appPath string) (string, error) {
				return filepath.Join(appPath, "Contents", "Resources", "app.asar"), nil
			},
			DisableFuses:  true,
			NeedsCodesign: true,
		},
		{
			ID:             "slack",
			Name:           "Slack",
			AppPath:        "/Applications/Slack.app",
			ProcessName:    "Slack",
			Driver:         DriverElectron,
			PatchMarker:    "fonted-slack-patch",
			PreloadRelPath: "dist/preload.bundle.js",
			ResolveAsarPath: func(appPath string) (string, error) {
				resDir := filepath.Join(appPath, "Contents", "Resources")

				archName := "arm64"
				if runtime.GOARCH == "amd64" {
					archName = "x64"
				}

				archAsar := filepath.Join(resDir, fmt.Sprintf("app-%s.asar", archName))
				if _, err := os.Stat(archAsar); err == nil {
					return archAsar, nil
				}

				standardAsar := filepath.Join(resDir, "app.asar")
				if _, err := os.Stat(standardAsar); err == nil {
					return standardAsar, nil
				}

				return archAsar, nil
			},
			DisableFuses:  true,
			NeedsCodesign: true,
		},
		{
			ID:            "rekordbox",
			Name:          "Rekordbox",
			AppPath:       resolveRekordboxPath(),
			ProcessName:   "rekordbox",
			Driver:        DriverNativeHook,
			NeedsCodesign: true,
		},
		{
			ID:            "engine-dj",
			Name:          "Engine DJ",
			AppPath:       "/Applications/Engine DJ.app",
			ProcessName:   "Engine DJ",
			Driver:        DriverNativeHook,
			NeedsCodesign: true,
		},
		{
			ID:            "telegram",
			Name:          "Telegram",
			AppPath:       "/Applications/Telegram.app",
			ProcessName:   "Telegram",
			Driver:        DriverNativeHook,
			NeedsCodesign: true,
		},
		{
			ID:            "chatgpt",
			Name:          "ChatGPT",
			AppPath:       resolveApplicationsPath("ChatGPT.app"),
			ProcessName:   "ChatGPT",
			Driver:        DriverNativeHook,
			NeedsCodesign: true,
		},
		{
			ID:            "amp",
			Name:          "Amp",
			AppPath:       resolveApplicationsPath("Amp.app"),
			ProcessName:   "Amp",
			Driver:        DriverNativeHook,
			NeedsCodesign: true,
		},
		{
			ID:            "orca",
			Name:          "Orca",
			AppPath:       resolveApplicationsPath("Orca.app"),
			ProcessName:   "Orca",
			Driver:        DriverNativeHook,
			NeedsCodesign: true,
		},
		{
			ID:             "claude",
			Name:           "Claude",
			AppPath:        resolveApplicationsPath("Claude.app"),
			ProcessName:    "Claude",
			Driver:         DriverElectron,
			PatchMarker:    "fonted-claude-patch",
			PreloadRelPath: ".vite/build/mainView.js",
			ResolveAsarPath: func(appPath string) (string, error) {
				return filepath.Join(appPath, "Contents", "Resources", "app.asar"), nil
			},
			DisableFuses:  true,
			NeedsCodesign: true,
		},
	}
}
