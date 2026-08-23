package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexgorbatchev/better-fonts/internal/asar"
	"github.com/alexgorbatchev/better-fonts/internal/config"
	"github.com/alexgorbatchev/better-fonts/internal/sysutil"
)

type mockRunner struct {
	commands [][]string
	failNext bool
}

func (m *mockRunner) Run(name string, args ...string) error {
	m.commands = append(m.commands, append([]string{name}, args...))
	if m.failNext {
		return errors.New("mock runner error")
	}
	return nil
}

func (m *mockRunner) Output(name string, args ...string) ([]byte, error) {
	m.commands = append(m.commands, append([]string{name}, args...))
	if m.failNext {
		return nil, errors.New("mock runner error")
	}
	if name == "plutil" {
		return []byte("SampleApp\n"), nil
	}
	return []byte("mock output"), nil
}

func createFakeAppBundle(t *testing.T, baseDir, appName, asarName, preloadRelPath string, preloadContent []byte) string {
	t.Helper()
	appPath := filepath.Join(baseDir, appName+".app")
	resDir := filepath.Join(appPath, "Contents", "Resources")
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		t.Fatalf("create fake app res dir: %v", err)
	}

	// Add fake electron framework binary so DisableFuses succeeds
	fwDir := filepath.Join(appPath, "Contents", "Frameworks", "Electron Framework.framework")
	_ = os.MkdirAll(fwDir, 0o755)
	var fwBin bytes.Buffer
	fwBin.WriteString(sysutil.FuseSentinel)
	fwBin.WriteByte(1)
	fwBin.WriteByte(8)
	fwBin.Write([]byte{'0', '1', '0', '0', '1', '1', '0', '1'})
	_ = os.WriteFile(filepath.Join(fwDir, "Electron Framework"), fwBin.Bytes(), 0o755)

	files := map[string][]byte{
		preloadRelPath: preloadContent,
		"index.js":      []byte("console.log('app index');"),
	}
	arch, err := asar.CreateArchive(files)
	if err != nil {
		t.Fatalf("create archive: %v", err)
	}

	asarFullPath := filepath.Join(resDir, asarName)
	if err := arch.Save(asarFullPath); err != nil {
		t.Fatalf("save fake asar: %v", err)
	}

	return appPath
}

func TestBuiltinsRegistry(t *testing.T) {
	builtins := GetBuiltinApps()
	if len(builtins) < 6 {
		t.Fatalf("expected at least 6 builtin apps, got %d", len(builtins))
	}

	ids := make(map[string]bool)
	for _, a := range builtins {
		ids[a.ID] = true

		// Test ResolveAsarPath if present
		if a.ResolveAsarPath != nil {
			path, err := a.ResolveAsarPath(a.AppPath)
			if err != nil || path == "" {
				t.Errorf("ResolveAsarPath failed for %s: %v, path=%q", a.ID, err, path)
			}
		}
	}

	for _, expectedID := range []string{"paseo", "signal", "slack", "rekordbox", "engine-dj", "telegram"} {
		if !ids[expectedID] {
			t.Errorf("missing expected builtin app %q", expectedID)
		}
	}

	// Test resolveRekordboxPath
	rPath := resolveRekordboxPath()
	if rPath == "" {
		t.Fatalf("resolveRekordboxPath returned empty string")
	}
}

func TestSlackAsarResolution(t *testing.T) {
	tempDir := t.TempDir()
	slackApp := filepath.Join(tempDir, "Slack.app")
	resDir := filepath.Join(slackApp, "Contents", "Resources")
	_ = os.MkdirAll(resDir, 0o755)

	var slackBuiltin *App
	for _, a := range GetBuiltinApps() {
		if a.ID == "slack" {
			appCopy := a
			slackBuiltin = &appCopy
			break
		}
	}
	if slackBuiltin == nil {
		t.Fatalf("slack builtin not found")
	}

	// 1. With standard app.asar
	_ = os.WriteFile(filepath.Join(resDir, "app.asar"), []byte("standard"), 0o644)
	res, err := slackBuiltin.ResolveAsarPath(slackApp)
	if err != nil || !strings.HasSuffix(res, "app.asar") {
		t.Fatalf("expected app.asar, got %s, err %v", res, err)
	}

	// 2. With arch-specific app-arm64.asar or app-x64.asar
	_ = os.WriteFile(filepath.Join(resDir, "app-arm64.asar"), []byte("arm64"), 0o644)
	_ = os.WriteFile(filepath.Join(resDir, "app-x64.asar"), []byte("x64"), 0o644)
	res, err = slackBuiltin.ResolveAsarPath(slackApp)
	if err != nil || (!strings.HasSuffix(res, "app-arm64.asar") && !strings.HasSuffix(res, "app-x64.asar")) {
		t.Fatalf("expected arch asar, got %s, err %v", res, err)
	}

	// 3. When neither exists (fallback)
	_ = os.Remove(filepath.Join(resDir, "app.asar"))
	_ = os.Remove(filepath.Join(resDir, "app-arm64.asar"))
	_ = os.Remove(filepath.Join(resDir, "app-x64.asar"))
	res, err = slackBuiltin.ResolveAsarPath(slackApp)
	if err != nil || (!strings.HasSuffix(res, "app-arm64.asar") && !strings.HasSuffix(res, "app-x64.asar")) {
		t.Fatalf("expected arch fallback, got %s, err %v", res, err)
	}
}

func TestAppPatchAndUnpatchElectron(t *testing.T) {
	tempDir := t.TempDir()
	originalPreload := []byte("console.log('original preload');\n")
	appPath := createFakeAppBundle(t, tempDir, "TestApp", "app.asar", "preload.bundle.js", originalPreload)

	testApp := App{
		ID:             "testapp",
		Name:           "TestApp",
		AppPath:        appPath,
		ProcessName:    "TestApp",
		Driver:         DriverElectron,
		PatchMarker:    "fonted-test-patch",
		PreloadRelPath: "preload.bundle.js",
		ResolveAsarPath: func(p string) (string, error) {
			return filepath.Join(p, "Contents", "Resources", "app.asar"), nil
		},
		DisableFuses:  true,
		NeedsCodesign: true,
	}

	opts := PatchOptions{
		Font:        "Maple Mono Normal NF",
		Restart:     true,
		DryRun:      false,
		Runner:      &mockRunner{},
		TempBaseDir: filepath.Join(tempDir, "work"),
	}

	// 1. Initial status -> unpatched
	st := testApp.Status()
	if !st.Installed {
		t.Fatalf("expected app to be installed")
	}
	if st.Patched {
		t.Fatalf("expected app to NOT be patched initially")
	}

	// 2. Dry run
	opts.DryRun = true
	if err := testApp.Patch(opts); err != nil {
		t.Fatalf("Patch dry-run failed: %v", err)
	}
	if testApp.Status().Patched {
		t.Fatalf("expected app not to be patched after dry run")
	}
	opts.DryRun = false

	// 3. Real Patch app
	if err := testApp.Patch(opts); err != nil {
		t.Fatalf("Patch failed: %v", err)
	}

	// Verify status after patch
	st = testApp.Status()
	if !st.Patched {
		t.Fatalf("expected app to be patched after Patch()")
	}
	if st.CurrentFont != "Maple Mono Normal NF" {
		t.Fatalf("expected font to be %q, got %q", "Maple Mono Normal NF", st.CurrentFont)
	}

	// 4. Re-patch with different font
	opts.Font = "JetBrains Mono"
	if err := testApp.Patch(opts); err != nil {
		t.Fatalf("Re-patch failed: %v", err)
	}
	st = testApp.Status()
	if st.CurrentFont != "JetBrains Mono" {
		t.Fatalf("expected font to be %q, got %q", "JetBrains Mono", st.CurrentFont)
	}

	// 5. Unpatch dry run
	opts.DryRun = true
	if err := testApp.Unpatch(opts); err != nil {
		t.Fatalf("Unpatch dry-run failed: %v", err)
	}
	opts.DryRun = false

	// 6. Real Unpatch app
	opts.Restart = true
	if err := testApp.Unpatch(opts); err != nil {
		t.Fatalf("Unpatch failed: %v", err)
	}

	st = testApp.Status()
	if st.Patched {
		t.Fatalf("expected app to NOT be patched after Unpatch()")
	}

	// Verify content inside asar is back to original
	asarFile, _ := testApp.ResolveAsarPath(appPath)
	arch, err := asar.OpenArchive(asarFile)
	if err != nil {
		t.Fatalf("open asar after unpatch: %v", err)
	}
	defer arch.Close()

	content, err := arch.ExtractFile("preload.bundle.js")
	if err != nil {
		t.Fatalf("extract preload: %v", err)
	}
	if strings.Contains(string(content), testApp.PatchMarker) {
		t.Fatalf("preload still contains patch marker after unpatch")
	}

	// 7. Test Patch with default TempBaseDir = ""
	opts.TempBaseDir = ""
	if err := testApp.Patch(opts); err != nil {
		t.Fatalf("Patch with empty TempBaseDir failed: %v", err)
	}
}

func TestAppPatchAndUnpatchNative(t *testing.T) {
	tempDir := t.TempDir()
	appPath := filepath.Join(tempDir, "SampleNative.app")
	macosDir := filepath.Join(appPath, "Contents", "MacOS")
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		t.Fatalf("mkdir macosDir: %v", err)
	}

	plistPath := filepath.Join(appPath, "Contents", "Info.plist")
	if err := os.WriteFile(plistPath, []byte("fake plist"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	binPath := filepath.Join(macosDir, "SampleNative")
	originalBinaryContent := []byte("original mach-o fake binary")
	if err := os.WriteFile(binPath, originalBinaryContent, 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	testApp := App{
		ID:          "samplenative",
		Name:        "SampleNative",
		AppPath:     appPath,
		ProcessName: "SampleNative",
		BinaryName:  "SampleNative",
		Driver:      DriverNativeHook,
	}

	cacheDir := filepath.Join(tempDir, "cache")
	opts := PatchOptions{
		Font:        "Maple Mono Normal NF",
		Restart:     false,
		DryRun:      false,
		Runner:      sysutil.DefaultRunner,
		TempBaseDir: cacheDir,
	}

	// 1. Initial status
	st := testApp.Status()
	if !st.Installed || st.Patched {
		t.Fatalf("initial native status unexpected: %+v", st)
	}

	// 2. Patch with empty TempBaseDir
	opts.TempBaseDir = ""
	if err := testApp.Patch(opts); err != nil {
		t.Fatalf("Patch native with empty TempBaseDir failed: %v", err)
	}

	st = testApp.Status()
	if !st.Patched {
		t.Fatalf("expected native app to be patched")
	}

	// Status when BinaryName is empty (resolves dynamically)
	testApp.BinaryName = ""
	stDynamic := testApp.Status()
	if !stDynamic.Patched {
		t.Fatalf("expected dynamic binary name status to be patched")
	}

	// 3. Unpatch with nil runner
	opts.Runner = nil
	if err := testApp.Unpatch(opts); err != nil {
		t.Fatalf("Unpatch native failed: %v", err)
	}

	st = testApp.Status()
	if st.Patched {
		t.Fatalf("expected native app to be unpatched")
	}
}

func TestAppStatus_ErrorCases(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Missing app path
	missingApp := App{
		ID:      "missing",
		Name:    "Missing",
		AppPath: filepath.Join(tempDir, "NonExistent.app"),
		Driver:  DriverElectron,
	}
	st := missingApp.Status()
	if st.Installed {
		t.Fatalf("expected Installed=false for missing app")
	}

	// 2. Missing asar file in app bundle
	appDir := filepath.Join(tempDir, "NoAsar.app")
	_ = os.MkdirAll(appDir, 0o755)
	noAsarApp := App{
		ID:      "noasar",
		Name:    "NoAsar",
		AppPath: appDir,
		Driver:  DriverElectron,
		ResolveAsarPath: func(p string) (string, error) {
			return filepath.Join(p, "Contents", "Resources", "missing.asar"), nil
		},
	}
	st = noAsarApp.Status()
	if !st.Installed || st.Error == nil {
		t.Fatalf("expected error for missing asar file: %+v", st)
	}

	// Error resolving asar path
	errResolveApp := App{
		ID:      "errresolve",
		Name:    "ErrResolve",
		AppPath: appDir,
		Driver:  DriverElectron,
		ResolveAsarPath: func(p string) (string, error) {
			return "", errors.New("asar resolve error")
		},
	}
	if st := errResolveApp.Status(); st.Error == nil {
		t.Fatalf("expected error when ResolveAsarPath fails in Status")
	}
	if err := errResolveApp.Patch(PatchOptions{Font: "F", Runner: &mockRunner{}}); err == nil {
		t.Fatalf("expected error when ResolveAsarPath fails in Patch")
	}

	// Corrupt asar in Status
	corruptAsarAppDir := filepath.Join(tempDir, "CorruptAsar.app")
	corruptRes := filepath.Join(corruptAsarAppDir, "Contents", "Resources")
	_ = os.MkdirAll(corruptRes, 0o755)
	_ = os.WriteFile(filepath.Join(corruptRes, "app.asar"), []byte("corrupted asar"), 0o644)
	corruptApp := App{
		ID:             "corrupt",
		Name:           "Corrupt",
		AppPath:        corruptAsarAppDir,
		Driver:         DriverElectron,
		PreloadRelPath: "preload.js",
		ResolveAsarPath: func(p string) (string, error) {
			return filepath.Join(p, "Contents", "Resources", "app.asar"), nil
		},
	}
	if stCorrupt := corruptApp.Status(); stCorrupt.Error == nil {
		t.Fatalf("expected error in Status for corrupted asar")
	}

	// 3. Asar missing preload file
	preloadMissingAppPath := createFakeAppBundle(t, tempDir, "MissingPreload", "app.asar", "other.js", []byte("xyz"))
	missingPreloadApp := App{
		ID:             "missingpreload",
		Name:           "MissingPreload",
		AppPath:        preloadMissingAppPath,
		Driver:         DriverElectron,
		PreloadRelPath: "preload.js",
		ResolveAsarPath: func(p string) (string, error) {
			return filepath.Join(p, "Contents", "Resources", "app.asar"), nil
		},
	}
	st = missingPreloadApp.Status()
	if st.Error == nil {
		t.Fatalf("expected error when preload is missing inside asar")
	}

	// Patching bad asar returns error
	if err := missingPreloadApp.Patch(PatchOptions{Font: "F", Runner: &mockRunner{}}); err == nil {
		t.Fatalf("expected error when preload is missing during Patch")
	}

	// 4. Patch error on missing app
	opts := PatchOptions{Font: "Test", Runner: &mockRunner{}}
	if err := missingApp.Patch(opts); err == nil {
		t.Fatalf("expected Patch on missing app to return error")
	}

	// 5. Native app status error when binary cannot be resolved
	nativeEmptyDir := filepath.Join(tempDir, "NativeEmpty.app")
	_ = os.MkdirAll(nativeEmptyDir, 0o755)
	nativeBadApp := App{
		ID:      "nativebad",
		Name:    "NativeBad",
		AppPath: nativeEmptyDir,
		Driver:  DriverNativeHook,
	}
	stNativeBad := nativeBadApp.Status()
	if stNativeBad.Error == nil {
		t.Fatalf("expected error resolving binary for empty native app")
	}

	// 6. Codesign error during applyTransformation
	validAppPath := createFakeAppBundle(t, tempDir, "FailSignApp", "app.asar", "preload.js", []byte("test"))
	failSignApp := App{
		ID:             "failsign",
		Name:           "FailSign",
		AppPath:        validAppPath,
		Driver:         DriverElectron,
		PreloadRelPath: "preload.js",
		ResolveAsarPath: func(p string) (string, error) {
			return filepath.Join(p, "Contents", "Resources", "app.asar"), nil
		},
		NeedsCodesign: true,
	}
	failRunner := &mockRunner{failNext: true}
	if err := failSignApp.Patch(PatchOptions{Font: "F", Runner: failRunner}); err == nil {
		t.Fatalf("expected error when codesign runner fails in applyTransformation")
	}

	// 7. Error resolving asar in workspace during patch
	failWorkspaceResolveApp := App{
		ID:             "failworkspaceresolve",
		Name:           "FailWorkspaceResolve",
		AppPath:        validAppPath,
		Driver:         DriverElectron,
		PreloadRelPath: "preload.js",
		ResolveAsarPath: func(p string) (string, error) {
			if strings.Contains(p, "-work") {
				return "", errors.New("workspace asar error")
			}
			return filepath.Join(p, "Contents", "Resources", "app.asar"), nil
		},
	}
	if err := failWorkspaceResolveApp.Patch(PatchOptions{Font: "F", Runner: &mockRunner{}}); err == nil {
		t.Fatalf("expected error when ResolveAsarPath fails in workspace")
	}

	// 8. Patching bad asar during applyTransformation
	badAsarAppPath := createFakeAppBundle(t, tempDir, "BadAsarApp", "app.asar", "other.js", []byte("xyz"))
	badAsarApp := App{
		ID:             "badasarapp",
		Name:           "BadAsarApp",
		AppPath:        badAsarAppPath,
		Driver:         DriverElectron,
		PreloadRelPath: "missing_preload.js",
		ResolveAsarPath: func(p string) (string, error) {
			return filepath.Join(p, "Contents", "Resources", "app.asar"), nil
		},
	}
	if err := badAsarApp.Patch(PatchOptions{Font: "F", Runner: &mockRunner{}}); err == nil {
		t.Fatalf("expected error during Patch when preload is missing in workspace")
	}
}

func TestGetAllAppsAndFindApp(t *testing.T) {
	// Nil config -> returns builtins
	apps := GetAllApps(nil)
	if len(apps) == 0 {
		t.Fatalf("expected builtins from GetAllApps(nil)")
	}

	// Config with custom apps
	cfg := &config.Config{
		Font: "Default Font",
		Apps: []string{"*"},
		CustomApps: []config.CustomAppDef{
			{
				ID:          "custom-electron",
				Name:        "Custom Electron",
				Path:        "/Applications/CustomElectron.app",
				Driver:      "electron",
				ProcessName: "CustomElectron",
				AsarPath:    "", // default "app.asar"
				PreloadPath: "preload.js",
			},
			{
				ID:          "custom-native",
				Name:        "Custom Native",
				Path:        "/Applications/CustomNative.app",
				Driver:      "native-hook",
				ProcessName: "CustomNative",
			},
		},
	}

	all := GetAllApps(cfg)

	// Test default asar name resolution on custom-electron
	for _, a := range all {
		if a.ID == "custom-electron" {
			res, _ := a.ResolveAsarPath(a.AppPath)
			if !strings.HasSuffix(res, "app.asar") {
				t.Fatalf("expected default app.asar, got %s", res)
			}
		}
	}

	// Test custom app with absolute asar path
	cfgAbs := &config.Config{
		CustomApps: []config.CustomAppDef{
			{
				ID:       "abs-asar",
				Name:     "Abs Asar",
				Path:     "/Applications/Abs.app",
				AsarPath: "/tmp/abs.asar",
			},
		},
	}
	allAbs := GetAllApps(cfgAbs)
	for _, a := range allAbs {
		if a.ID == "abs-asar" {
			res, _ := a.ResolveAsarPath(a.AppPath)
			if res != "/tmp/abs.asar" {
				t.Fatalf("expected /tmp/abs.asar, got %s", res)
			}
		}
	}

	// Test FindApp
	if a, ok := FindApp(all, "custom-electron"); !ok || a.ID != "custom-electron" {
		t.Fatalf("FindApp custom-electron failed: %v", a)
	}
	if a, ok := FindApp(all, "Custom Native"); !ok || a.ID != "custom-native" {
		t.Fatalf("FindApp Custom Native by Name failed: %v", a)
	}
	if a, ok := FindApp(all, "/Applications/CustomNative.app"); !ok || a.ID != "custom-native" {
		t.Fatalf("FindApp Custom Native by Path failed: %v", a)
	}
	if _, ok := FindApp(all, "non-existent-app-xyz"); ok {
		t.Fatalf("expected FindApp to return false for non-existent query")
	}
}
