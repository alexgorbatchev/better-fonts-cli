package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/alexgorbatchev/better-fonts/internal/asar"
	"github.com/alexgorbatchev/better-fonts/internal/config"
	"github.com/alexgorbatchev/better-fonts/internal/driver/native"
	"github.com/alexgorbatchev/better-fonts/internal/patcher"
	"github.com/alexgorbatchev/better-fonts/internal/sysutil"
)

// DriverType defines the patching mechanism used for an application.
type DriverType string

const (
	DriverElectron   DriverType = "electron"
	DriverNativeHook DriverType = "native-hook"
)

// App defines a supported macOS application.
type App struct {
	ID              string
	Name            string
	AppPath         string
	ProcessName     string
	Driver          DriverType
	BinaryName      string
	PatchMarker     string
	PreloadRelPath  string
	ResolveAsarPath func(appPath string) (string, error)
	DisableFuses    bool
	NeedsCodesign   bool
}

// PatchOptions configures the patch or unpatch execution.
type PatchOptions struct {
	Font        string
	Restart     bool
	DryRun      bool
	Runner      sysutil.Runner
	TempBaseDir string
}

// AppStatus captures the current installation and patch state of an app.
type AppStatus struct {
	ID          string
	Name        string
	AppPath     string
	Driver      DriverType
	Installed   bool
	Patched     bool
	CurrentFont string
	AsarPath    string
	Error       error
}

// Status inspects the app bundle and returns its current state.
func (a *App) Status() AppStatus {
	st := AppStatus{
		ID:      a.ID,
		Name:    a.Name,
		AppPath: a.AppPath,
		Driver:  a.Driver,
	}

	if _, err := os.Stat(a.AppPath); os.IsNotExist(err) {
		st.Installed = false
		return st
	}
	st.Installed = true

	if a.Driver == DriverNativeHook {
		binName := a.BinaryName
		if binName == "" {
			var err error
			binName, err = native.GetAppBinaryName(a.AppPath, sysutil.DefaultRunner)
			if err != nil {
				st.Error = fmt.Errorf("resolving binary name: %w", err)
				return st
			}
		}

		st.Patched = native.IsPatched(a.AppPath, binName)
		if st.Patched {
			st.CurrentFont = native.DetectFont(a.AppPath, config.DefaultFont)
		}
		return st
	}

	// Electron status check
	asarPath, err := a.ResolveAsarPath(a.AppPath)
	if err != nil {
		st.Error = fmt.Errorf("resolving asar path: %w", err)
		return st
	}
	st.AsarPath = asarPath

	if _, err := os.Stat(asarPath); os.IsNotExist(err) {
		st.Error = fmt.Errorf("asar file not found at %s", asarPath)
		return st
	}

	arch, err := asar.OpenArchive(asarPath)
	if err != nil {
		st.Error = fmt.Errorf("opening asar archive %s: %w", asarPath, err)
		return st
	}
	defer arch.Close()

	if !arch.HasFile(a.PreloadRelPath) {
		st.Error = fmt.Errorf("preload %s not found inside asar", a.PreloadRelPath)
		return st
	}

	preloadBytes, err := arch.ExtractFile(a.PreloadRelPath)
	if err != nil {
		st.Error = fmt.Errorf("extracting preload %s: %w", a.PreloadRelPath, err)
		return st
	}

	isPatched, fontName := patcher.DetectPatch(preloadBytes, a.PatchMarker)
	st.Patched = isPatched
	st.CurrentFont = fontName

	return st
}

// Patch applies the font patch to the target application.
func (a *App) Patch(opts PatchOptions) error {
	if a.Driver == DriverNativeHook {
		cacheDir := opts.TempBaseDir
		if cacheDir == "" {
			var err error
			cacheDir, err = config.GetCacheDir()
			if err != nil {
				cacheDir = filepath.Join(os.TempDir(), "better-fonts")
			}
		}
		return native.PatchNativeApp(a.AppPath, opts.Font, opts.Restart, opts.DryRun, cacheDir, opts.Runner)
	}

	return a.applyTransformation(opts, func(oldPreload []byte) ([]byte, error) {
		return patcher.InjectPatch(oldPreload, a.PatchMarker, opts.Font), nil
	})
}

// Unpatch removes the font patch from the target application.
func (a *App) Unpatch(opts PatchOptions) error {
	if a.Driver == DriverNativeHook {
		return native.UnpatchNativeApp(a.AppPath, opts.Restart, opts.DryRun, opts.Runner)
	}

	return a.applyTransformation(opts, func(oldPreload []byte) ([]byte, error) {
		return patcher.StripPatch(oldPreload, a.PatchMarker), nil
	})
}

func (a *App) applyTransformation(opts PatchOptions, transform func([]byte) ([]byte, error)) error {
	runner := opts.Runner
	if runner == nil {
		runner = sysutil.DefaultRunner
	}

	if _, err := os.Stat(a.AppPath); os.IsNotExist(err) {
		return fmt.Errorf("%s not found at %s", a.Name, a.AppPath)
	}

	if opts.DryRun {
		return nil
	}

	tempBase := opts.TempBaseDir
	if tempBase == "" {
		cacheDir, err := config.GetCacheDir()
		if err != nil {
			tempBase = filepath.Join(os.TempDir(), "better-fonts")
		} else {
			tempBase = filepath.Join(cacheDir, "work")
		}
	}

	workDir := filepath.Join(tempBase, a.ID+"-work")
	_ = os.RemoveAll(workDir)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("creating work directory %s: %w", workDir, err)
	}
	defer os.RemoveAll(workDir)

	workAppPath := filepath.Join(workDir, filepath.Base(a.AppPath))

	// Copy app to temporary workspace
	if err := sysutil.CopyDir(a.AppPath, workAppPath); err != nil {
		return fmt.Errorf("copying %s to workspace: %w", a.AppPath, err)
	}

	// Disable fuses if requested
	if a.DisableFuses {
		if err := sysutil.DisableIntegrityValidation(workAppPath, runner); err != nil {
			return fmt.Errorf("disabling integrity validation: %w", err)
		}
	}

	// Resolve asar file inside workspace
	asarPath, err := a.ResolveAsarPath(workAppPath)
	if err != nil {
		return fmt.Errorf("resolving asar in workspace: %w", err)
	}

	// Patch the asar archive
	tmpPatchedAsar := asarPath + ".tmp"
	if err := asar.PatchAsarFile(asarPath, a.PreloadRelPath, tmpPatchedAsar, transform); err != nil {
		return fmt.Errorf("patching asar archive %s: %w", asarPath, err)
	}

	if err := os.Rename(tmpPatchedAsar, asarPath); err != nil {
		return fmt.Errorf("replacing asar with patched version: %w", err)
	}

	// Codesign the bundle
	if a.NeedsCodesign {
		if err := sysutil.Codesign(workAppPath, runner); err != nil {
			return fmt.Errorf("codesigning patched app bundle: %w", err)
		}
	}

	// Quit app if restart is enabled
	if opts.Restart && a.ProcessName != "" {
		_ = sysutil.QuitApp(a.ProcessName, runner)
	}

	// Replace original app bundle safely
	backupPath := a.AppPath + ".better-fonts-backup"
	_ = os.RemoveAll(backupPath)
	if err := os.Rename(a.AppPath, backupPath); err != nil {
		return fmt.Errorf("backing up %s: %w", a.AppPath, err)
	}

	if err := sysutil.CopyDir(workAppPath, a.AppPath); err != nil {
		_ = os.Rename(backupPath, a.AppPath)
		return fmt.Errorf("copying patched app to %s: %w", a.AppPath, err)
	}
	_ = os.RemoveAll(backupPath)

	// Launch app if restart is enabled
	if opts.Restart && a.ProcessName != "" {
		_ = sysutil.StartApp(a.ProcessName, runner)
	}

	return nil
}

// GetAllApps returns builtin apps combined with any custom apps configured in config.toml.
func GetAllApps(cfg *config.Config) []App {
	apps := append([]App{}, GetBuiltinApps()...)
	if cfg == nil {
		return apps
	}

	for _, custom := range cfg.CustomApps {
		customDef := custom
		driver := DriverElectron
		if strings.ToLower(customDef.Driver) == string(DriverNativeHook) {
			driver = DriverNativeHook
		}

		apps = append(apps, App{
			ID:             customDef.ID,
			Name:           customDef.Name,
			AppPath:        customDef.Path,
			ProcessName:    customDef.ProcessName,
			Driver:         driver,
			PatchMarker:    fmt.Sprintf("fonted-%s-patch", customDef.ID),
			PreloadRelPath: customDef.PreloadPath,
			ResolveAsarPath: func(p string) (string, error) {
				asarName := customDef.AsarPath
				if asarName == "" {
					asarName = "app.asar"
				}
				if filepath.IsAbs(asarName) {
					return asarName, nil
				}
				return filepath.Join(p, "Contents", "Resources", asarName), nil
			},
			DisableFuses:  true,
			NeedsCodesign: true,
		})
	}

	return apps
}

// FindApp locates an app by ID, Name, or AppPath (case-insensitive).
func FindApp(apps []App, query string) (*App, bool) {
	norm := strings.ToLower(strings.TrimSpace(query))
	for _, a := range apps {
		if strings.ToLower(a.ID) == norm || strings.ToLower(a.Name) == norm || strings.ToLower(a.AppPath) == norm {
			appCopy := a
			return &appCopy, true
		}
	}
	return nil, false
}
