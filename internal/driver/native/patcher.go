package native

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexgorbatchev/better-fonts/internal/sysutil"
)

// GetAppBinaryName extracts the executable binary name from an application's Info.plist.
func GetAppBinaryName(appPath string, r sysutil.Runner) (string, error) {
	if r == nil {
		r = sysutil.DefaultRunner
	}

	plistPath := filepath.Join(appPath, "Contents", "Info.plist")
	if _, err := os.Stat(plistPath); err == nil {
		out, err := r.Output("plutil", "-extract", "CFBundleExecutable", "raw", plistPath)
		if err == nil {
			binName := strings.TrimSpace(string(out))
			if binName != "" {
				return binName, nil
			}
		}
	}

	// Fallback: search Contents/MacOS for the primary binary
	macosDir := filepath.Join(appPath, "Contents", "MacOS")
	entries, readErr := os.ReadDir(macosDir)
	if readErr == nil {
		for _, entry := range entries {
			if !entry.IsDir() && !strings.HasSuffix(entry.Name(), ".orig") && !strings.HasSuffix(entry.Name(), ".bak") && !strings.HasSuffix(entry.Name(), ".dylib") {
				return entry.Name(), nil
			}
		}
	}

	return "", fmt.Errorf("CFBundleExecutable not found in %s", appPath)
}

// IsPatched checks whether a native app bundle currently has the font hook installed.
func IsPatched(appPath string, binaryName string) bool {
	macosDir := filepath.Join(appPath, "Contents", "MacOS")
	dylibPath := filepath.Join(macosDir, "libfonthook.dylib")
	origPath := filepath.Join(macosDir, binaryName+".orig")

	if _, err := os.Stat(dylibPath); err == nil {
		if _, err := os.Stat(origPath); err == nil {
			return true
		}
	}
	return false
}

// DetectFont extracts the patched font name from the installed dylib if available.
func DetectFont(appPath string, defaultFont string) string {
	macosDir := filepath.Join(appPath, "Contents", "MacOS")
	dylibPath := filepath.Join(macosDir, "libfonthook.dylib")

	data, err := os.ReadFile(dylibPath)
	if err != nil {
		return defaultFont
	}

	// Search binary for the null-terminated font string or marker
	idx := strings.Index(string(data), "Maple Mono")
	if idx != -1 {
		end := strings.IndexByte(string(data[idx:]), 0)
		if end != -1 && end < 64 {
			return string(data[idx : idx+end])
		}
	}

	return defaultFont
}

// CheckWritePermission checks if the MacOS directory inside the app bundle is writable.
func CheckWritePermission(macosDir string) error {
	testFile := filepath.Join(macosDir, ".better_fonts_perm_test")
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied") {
			return fmt.Errorf("permission denied writing to %s. Run this command with sudo: 'sudo better-fonts ...'", macosDir)
		}
		return fmt.Errorf("checking write permissions on %s: %w", macosDir, err)
	}
	_ = os.Remove(testFile)
	return nil
}

// PatchNativeApp applies the native CoreText DYLD_INTERPOSE font hook to an application.
func PatchNativeApp(appPath string, targetFont string, restart bool, dryRun bool, cacheDir string, r sysutil.Runner) error {
	if r == nil {
		r = sysutil.DefaultRunner
	}

	if _, err := os.Stat(appPath); os.IsNotExist(err) {
		return fmt.Errorf("app bundle not found at %s", appPath)
	}

	binaryName, err := GetAppBinaryName(appPath, r)
	if err != nil {
		return err
	}

	macosDir := filepath.Join(appPath, "Contents", "MacOS")
	binaryPath := filepath.Join(macosDir, binaryName)
	backupBinaryPath := filepath.Join(macosDir, binaryName+".bak")
	origBinaryPath := filepath.Join(macosDir, binaryName+".orig")
	dylibPath := filepath.Join(macosDir, "libfonthook.dylib")

	if dryRun {
		return nil
	}

	if err := CheckWritePermission(macosDir); err != nil {
		return err
	}

	// Quit app if running and restart is requested
	if restart {
		_ = sysutil.QuitApp(binaryName, r)
	}

	// 1. Ensure clean backup of original binary exists
	if _, err := os.Stat(backupBinaryPath); os.IsNotExist(err) {
		if _, origErr := os.Stat(origBinaryPath); origErr == nil {
			if err := sysutil.CopyFile(origBinaryPath, backupBinaryPath); err != nil {
				return fmt.Errorf("backing up orig binary: %w", err)
			}
		} else {
			if err := sysutil.CopyFile(binaryPath, backupBinaryPath); err != nil {
				return fmt.Errorf("backing up clean binary: %w", err)
			}
		}
	}

	// 2. Ensure origBinaryPath is the clean Mach-O binary
	if err := sysutil.CopyFile(backupBinaryPath, origBinaryPath); err != nil {
		return fmt.Errorf("restoring orig binary from backup: %w", err)
	}
	_ = os.Chmod(origBinaryPath, 0o755)

	// 3. Compile dylib and launcher
	compiledDylib, compiledLauncher, err := BuildArtifacts(targetFont, cacheDir, r)
	if err != nil {
		return fmt.Errorf("compiling native font hook artifacts: %w", err)
	}

	// 4. Install dylib
	if err := sysutil.CopyFile(compiledDylib, dylibPath); err != nil {
		return fmt.Errorf("installing libfonthook.dylib to %s: %w", dylibPath, err)
	}
	_ = os.Chmod(dylibPath, 0o755)

	// 5. Install launcher wrapper
	if err := sysutil.CopyFile(compiledLauncher, binaryPath); err != nil {
		return fmt.Errorf("installing wrapper launcher to %s: %w", binaryPath, err)
	}
	_ = os.Chmod(binaryPath, 0o755)

	// 6. Re-sign application bundle
	if err := sysutil.Codesign(appPath, r); err != nil {
		return fmt.Errorf("codesigning patched app: %w", err)
	}

	// 7. Restart app if requested
	if restart {
		_ = sysutil.StartApp(binaryName, r)
	}

	return nil
}

// UnpatchNativeApp removes the native CoreText font hook and restores the original executable.
func UnpatchNativeApp(appPath string, restart bool, dryRun bool, r sysutil.Runner) error {
	if r == nil {
		r = sysutil.DefaultRunner
	}

	if _, err := os.Stat(appPath); os.IsNotExist(err) {
		return fmt.Errorf("app bundle not found at %s", appPath)
	}

	binaryName, err := GetAppBinaryName(appPath, r)
	if err != nil {
		return err
	}

	macosDir := filepath.Join(appPath, "Contents", "MacOS")
	binaryPath := filepath.Join(macosDir, binaryName)
	backupBinaryPath := filepath.Join(macosDir, binaryName+".bak")
	origBinaryPath := filepath.Join(macosDir, binaryName+".orig")
	dylibPath := filepath.Join(macosDir, "libfonthook.dylib")

	if dryRun {
		return nil
	}

	if err := CheckWritePermission(macosDir); err != nil {
		return err
	}

	// Quit app if running and restart is requested
	if restart {
		_ = sysutil.QuitApp(binaryName, r)
	}

	// Restore original binary
	if _, err := os.Stat(backupBinaryPath); err == nil {
		if err := sysutil.CopyFile(backupBinaryPath, binaryPath); err != nil {
			return fmt.Errorf("restoring original binary from backup: %w", err)
		}
	} else if _, err := os.Stat(origBinaryPath); err == nil {
		if err := sysutil.CopyFile(origBinaryPath, binaryPath); err != nil {
			return fmt.Errorf("restoring original binary from .orig: %w", err)
		}
	}
	_ = os.Chmod(binaryPath, 0o755)

	// Remove .orig and .dylib
	_ = os.Remove(origBinaryPath)
	_ = os.Remove(dylibPath)

	// Re-sign app bundle
	if err := sysutil.Codesign(appPath, r); err != nil {
		return fmt.Errorf("codesigning restored app: %w", err)
	}

	// Restart app if requested
	if restart {
		_ = sysutil.StartApp(binaryName, r)
	}

	return nil
}
