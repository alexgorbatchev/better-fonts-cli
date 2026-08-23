package selfupdate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexgorbatchev/godeps"
)

const (
	RepoOwner  = "alexgorbatchev"
	RepoName   = "better-fonts-cli"
	BinaryName = "better-fonts"
)

// UpgradeSelf checks for a newer release of better-fonts and replaces the running binary using godeps.
func UpgradeSelf(ctx context.Context, currentVersion string) (bool, string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return false, "", fmt.Errorf("resolving running executable path: %w", err)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return false, "", fmt.Errorf("resolving executable symlinks: %w", err)
	}

	return UpgradeSelfToPath(ctx, currentVersion, exePath)
}

// UpgradeSelfToPath upgrades the executable at destPath using godeps.
func UpgradeSelfToPath(ctx context.Context, currentVersion, destPath string) (bool, string, error) {
	newVer, err := godeps.UpgradeSelfToPath(ctx, RepoOwner, RepoName, currentVersion, destPath)
	if err != nil {
		if strings.Contains(err.Error(), "already at the latest version") {
			return false, currentVersion, nil
		}
		return false, "", fmt.Errorf("upgrading better-fonts at %s: %w", destPath, err)
	}

	return true, newVer, nil
}
