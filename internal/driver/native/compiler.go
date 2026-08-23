package native

import (
	_ "embed"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/alexgorbatchev/better-fonts/internal/sysutil"
)

//go:embed font_interpose.m.src
var fontInterposeSource []byte

//go:embed wrapper_launcher.c.src
var wrapperLauncherSource []byte

// BuildArtifacts compiles or returns cached libfonthook.dylib and wrapper_launcher for the given target font.
func BuildArtifacts(targetFont string, baseCacheDir string, r sysutil.Runner) (string, string, error) {
	if r == nil {
		r = sysutil.DefaultRunner
	}

	hash := sha256.Sum256([]byte(targetFont + "\n" + string(fontInterposeSource) + "\n" + string(wrapperLauncherSource)))
	buildDir := filepath.Join(baseCacheDir, "build", hex.EncodeToString(hash[:8]))

	dylibPath := filepath.Join(buildDir, "libfonthook.dylib")
	launcherPath := filepath.Join(buildDir, "wrapper_launcher")

	// Check if already compiled
	if _, err := os.Stat(dylibPath); err == nil {
		if _, err := os.Stat(launcherPath); err == nil {
			return dylibPath, launcherPath, nil
		}
	}

	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return "", "", fmt.Errorf("creating native build dir %s: %w", buildDir, err)
	}

	interposeSrcPath := filepath.Join(buildDir, "font_interpose.m")
	if err := os.WriteFile(interposeSrcPath, fontInterposeSource, 0o644); err != nil {
		return "", "", fmt.Errorf("writing font_interpose.m: %w", err)
	}

	launcherSrcPath := filepath.Join(buildDir, "wrapper_launcher.c")
	if err := os.WriteFile(launcherSrcPath, wrapperLauncherSource, 0o644); err != nil {
		return "", "", fmt.Errorf("writing wrapper_launcher.c: %w", err)
	}

	// Compile libfonthook.dylib
	fontDefine := fmt.Sprintf(`-DTARGET_FONT_NAME="%s"`, targetFont)
	dylibArgs := []string{
		"-dynamiclib",
		"-framework", "CoreText",
		"-framework", "Foundation",
		fontDefine,
		"-o", dylibPath,
		interposeSrcPath,
	}

	if err := r.Run("clang", dylibArgs...); err != nil {
		return "", "", fmt.Errorf("compiling libfonthook.dylib: %w", err)
	}

	// Compile wrapper_launcher
	launcherArgs := []string{
		"-O3",
		"-o", launcherPath,
		launcherSrcPath,
	}

	if err := r.Run("clang", launcherArgs...); err != nil {
		return "", "", fmt.Errorf("compiling wrapper_launcher: %w", err)
	}

	return dylibPath, launcherPath, nil
}
