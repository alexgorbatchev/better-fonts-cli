package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// resolveRekordboxPath locates the installed Rekordbox app bundle.
func resolveRekordboxPath() string {
	candidates := []string{
		"/Applications/rekordbox 7/rekordbox.app",
		"/Applications/rekordbox 6/rekordbox.app",
		"/Applications/rekordbox.app",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
}

// resolveChatGPTPath locates the installed ChatGPT app bundle.
func resolveChatGPTPath() string {
	candidates := []string{
		"/Applications/ChatGPT.app",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "Applications", "ChatGPT.app"))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
}

// resolveClaudePath locates the installed Claude app bundle.
func resolveClaudePath() string {
	candidates := []string{
		"/Applications/Claude.app",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "Applications", "Claude.app"))
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
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
			ID:             "chatgpt",
			Name:           "ChatGPT",
			AppPath:        resolveChatGPTPath(),
			ProcessName:    "ChatGPT",
			Driver:         DriverElectron,
			PatchMarker:    "fonted-chatgpt-patch",
			PreloadRelPath: ".vite/build/preload.js",
			ResolveAsarPath: func(appPath string) (string, error) {
				return filepath.Join(appPath, "Contents", "Resources", "app.asar"), nil
			},
			DisableFuses:  true,
			NeedsCodesign: true,
		},
		{
			ID:             "claude",
			Name:           "Claude",
			AppPath:        resolveClaudePath(),
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
