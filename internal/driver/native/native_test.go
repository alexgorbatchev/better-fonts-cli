package native

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexgorbatchev/better-fonts/internal/sysutil"
)

type mockRunner struct {
	commands  [][]string
	failNext  bool
	failIndex int
	callCount int
	customOut []byte
}

func (m *mockRunner) Run(name string, args ...string) error {
	call := append([]string{name}, args...)
	m.commands = append(m.commands, call)
	m.callCount++
	if m.failNext || (m.failIndex > 0 && m.callCount == m.failIndex) {
		return errors.New("mock runner failure")
	}
	return nil
}

func (m *mockRunner) Output(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	m.commands = append(m.commands, call)
	if m.failNext {
		return nil, errors.New("mock runner failure")
	}
	if len(m.customOut) > 0 {
		return m.customOut, nil
	}
	if name == "plutil" {
		return []byte("SampleApp\n"), nil
	}
	return []byte("mock output"), nil
}

func TestBuildArtifacts(t *testing.T) {
	tempDir := t.TempDir()
	dylibPath, launcherPath, err := BuildArtifacts("Maple Mono Normal NF", tempDir, sysutil.DefaultRunner)
	if err != nil {
		t.Fatalf("BuildArtifacts failed: %v", err)
	}

	if _, err := os.Stat(dylibPath); err != nil {
		t.Fatalf("libfonthook.dylib does not exist: %v", err)
	}
	if _, err := os.Stat(launcherPath); err != nil {
		t.Fatalf("wrapper_launcher does not exist: %v", err)
	}

	// Calling again should return cached artifacts immediately
	d2, l2, err := BuildArtifacts("Maple Mono Normal NF", tempDir, nil)
	if err != nil || d2 != dylibPath || l2 != launcherPath {
		t.Fatalf("cached BuildArtifacts mismatch: %v, %s, %s", err, d2, l2)
	}

	// Test clang failure on dylib
	mockR := &mockRunner{failNext: true}
	if _, _, err := BuildArtifacts("NewFont1", filepath.Join(tempDir, "fail1"), mockR); err == nil {
		t.Fatalf("expected error when compiler runner fails on dylib")
	}

	// Test BuildArtifacts error writing launcher source file
	badLauncherSrcDir := filepath.Join(tempDir, "bad_launcher_src", "build", "a78e7bb6")
	_ = os.MkdirAll(filepath.Join(badLauncherSrcDir, "wrapper_launcher.c"), 0o755)
	_, _, _ = BuildArtifacts("NewFontWriteFail2", filepath.Dir(filepath.Dir(badLauncherSrcDir)), sysutil.DefaultRunner)

	// Test clang failure on launcher (2nd compile call)
	mockRLauncherFail := &mockRunner{failIndex: 2}
	if _, _, err := BuildArtifacts("NewFontLauncherFail", filepath.Join(tempDir, "launcher_fail"), mockRLauncherFail); err == nil {
		t.Fatalf("expected error when wrapper_launcher compile fails")
	}

	// Test BuildArtifacts error creating directory
	if _, _, err := BuildArtifacts("NewFont", "/dev/null/impossible", sysutil.DefaultRunner); err == nil {
		t.Fatalf("expected error on impossible cache dir in BuildArtifacts")
	}

	// Test BuildArtifacts error writing source file in read-only dir
	roBuildParent := filepath.Join(tempDir, "ro_build")
	hashBuildDir := filepath.Join(roBuildParent, "build", "a78e7bb6")
	_ = os.MkdirAll(hashBuildDir, 0o555)
	_ = os.Chmod(filepath.Dir(hashBuildDir), 0o555)
	_ = os.Chmod(hashBuildDir, 0o555)
	_, _, _ = BuildArtifacts("NewFontWriteFail", roBuildParent, sysutil.DefaultRunner)
	_ = os.Chmod(hashBuildDir, 0o755)
	_ = os.Chmod(filepath.Dir(hashBuildDir), 0o755)
}

func TestPatchAndUnpatchNativeApp(t *testing.T) {
	tempDir := t.TempDir()
	appPath := filepath.Join(tempDir, "Sample.app")
	macosDir := filepath.Join(appPath, "Contents", "MacOS")
	if err := os.MkdirAll(macosDir, 0o755); err != nil {
		t.Fatalf("mkdir macosDir: %v", err)
	}

	plistPath := filepath.Join(appPath, "Contents", "Info.plist")
	if err := os.WriteFile(plistPath, []byte("fake plist"), 0o644); err != nil {
		t.Fatalf("write plist: %v", err)
	}

	binPath := filepath.Join(macosDir, "SampleApp")
	originalBinaryContent := []byte("original mach-o fake binary")
	if err := os.WriteFile(binPath, originalBinaryContent, 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	cacheDir := filepath.Join(tempDir, "cache")

	// 1. Dry run test with nil runner
	if err := PatchNativeApp(appPath, "Maple Mono Normal NF", false, true, cacheDir, nil); err != nil {
		t.Fatalf("PatchNativeApp dry run failed: %v", err)
	}
	if IsPatched(appPath, "SampleApp") {
		t.Fatalf("expected app not to be patched after dry-run")
	}

	// 2. Real Patch test with restart
	err := PatchNativeApp(appPath, "Maple Mono Normal NF", true, false, cacheDir, sysutil.DefaultRunner)
	if err != nil {
		t.Fatalf("PatchNativeApp failed: %v", err)
	}

	if !IsPatched(appPath, "SampleApp") {
		t.Fatalf("expected IsPatched to return true after patch")
	}

	// 3. Detect font test with embedded string
	_ = os.WriteFile(filepath.Join(macosDir, "libfonthook.dylib"), []byte("prefix Maple Mono Normal NF\x00suffix"), 0o755)
	detected := DetectFont(appPath, "DefaultFont")
	if detected != "Maple Mono Normal NF" {
		t.Fatalf("DetectFont returned %q, want 'Maple Mono Normal NF'", detected)
	}

	// Verify original binary was preserved in .orig
	origContent, err := os.ReadFile(filepath.Join(macosDir, "SampleApp.orig"))
	if err != nil || string(origContent) != string(originalBinaryContent) {
		t.Fatalf("SampleApp.orig mismatch: got %q, want %q", string(origContent), string(originalBinaryContent))
	}

	// Re-patching when .orig exists but .bak doesn't
	_ = os.Remove(filepath.Join(macosDir, "SampleApp.bak"))
	if err := PatchNativeApp(appPath, "Maple Mono Normal NF", false, false, cacheDir, sysutil.DefaultRunner); err != nil {
		t.Fatalf("Re-patching when .orig exists failed: %v", err)
	}

	// 4. Unpatch dry run with nil runner
	if err := UnpatchNativeApp(appPath, false, true, nil); err != nil {
		t.Fatalf("UnpatchNativeApp dry run failed: %v", err)
	}

	// 5. Real Unpatch with restart
	err = UnpatchNativeApp(appPath, true, false, sysutil.DefaultRunner)
	if err != nil {
		t.Fatalf("UnpatchNativeApp failed: %v", err)
	}

	if IsPatched(appPath, "SampleApp") {
		t.Fatalf("expected IsPatched to return false after unpatch")
	}

	// Verify binary is restored
	restoredContent, err := os.ReadFile(binPath)
	if err != nil || string(restoredContent) != string(originalBinaryContent) {
		t.Fatalf("SampleApp restored mismatch: got %q, want %q", string(restoredContent), string(originalBinaryContent))
	}

	// 6. Test Unpatch when only .orig exists (no .bak)
	_ = os.Remove(filepath.Join(macosDir, "SampleApp.bak"))
	_ = os.WriteFile(filepath.Join(macosDir, "SampleApp.orig"), originalBinaryContent, 0o755)
	_ = os.WriteFile(filepath.Join(macosDir, "libfonthook.dylib"), []byte("dylib"), 0o755)
	if err := UnpatchNativeApp(appPath, false, false, sysutil.DefaultRunner); err != nil {
		t.Fatalf("Unpatch from orig failed: %v", err)
	}

	// UnpatchNativeApp when codesign fails
	failCodesignRunner := &mockRunner{failNext: true}
	_ = os.WriteFile(filepath.Join(macosDir, "SampleApp.bak"), originalBinaryContent, 0o755)
	if err := UnpatchNativeApp(appPath, false, false, failCodesignRunner); err == nil {
		t.Fatalf("expected error when codesign fails in UnpatchNativeApp")
	}
}

func TestNative_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()

	// Unpatch when neither .bak nor .orig exists
	cleanAppPath := filepath.Join(tempDir, "CleanApp.app")
	cleanMacos := filepath.Join(cleanAppPath, "Contents", "MacOS")
	_ = os.MkdirAll(cleanMacos, 0o755)
	_ = os.WriteFile(filepath.Join(cleanMacos, "CleanApp"), []byte("bin"), 0o755)
	if err := UnpatchNativeApp(cleanAppPath, false, false, sysutil.DefaultRunner); err != nil {
		t.Fatalf("Unpatch on clean app failed: %v", err)
	}

	// Missing app path
	if err := PatchNativeApp(filepath.Join(tempDir, "Missing.app"), "Font", false, false, tempDir, sysutil.DefaultRunner); err == nil {
		t.Fatalf("expected error on missing app path")
	}
	if err := UnpatchNativeApp(filepath.Join(tempDir, "Missing.app"), false, false, sysutil.DefaultRunner); err == nil {
		t.Fatalf("expected error on missing app path")
	}

	// Detect font on missing dylib
	if f := DetectFont(filepath.Join(tempDir, "Missing.app"), "FallbackFont"); f != "FallbackFont" {
		t.Fatalf("expected FallbackFont, got %q", f)
	}

	// Check write permission on valid dir
	macosDir := filepath.Join(tempDir, "MacOS")
	_ = os.MkdirAll(macosDir, 0o755)
	if err := CheckWritePermission(macosDir); err != nil {
		t.Fatalf("CheckWritePermission failed on writable dir: %v", err)
	}

	// Check write permission on read-only dir
	roDir := filepath.Join(tempDir, "ReadOnlyDir")
	_ = os.MkdirAll(roDir, 0o555)
	_ = CheckWritePermission(roDir)
	_ = os.Chmod(roDir, 0o755) // restore for cleanup

	// Check write permission on impossible directory path
	if err := CheckWritePermission("/dev/null/impossible/subdir"); err == nil {
		t.Fatalf("expected error on impossible directory path in CheckWritePermission")
	}

	// GetAppBinaryName without Info.plist fallback to scanning Contents/MacOS
	appDir := filepath.Join(tempDir, "Fallback.app")
	fbMacos := filepath.Join(appDir, "Contents", "MacOS")
	_ = os.MkdirAll(fbMacos, 0o755)
	_ = os.WriteFile(filepath.Join(fbMacos, "MyBinary"), []byte("bin"), 0o755)
	runner := &mockRunner{}
	name, err := GetAppBinaryName(appDir, runner)
	if err != nil || name != "MyBinary" {
		t.Fatalf("fallback GetAppBinaryName failed: got %q, err %v", name, err)
	}

	// GetAppBinaryName with Info.plist but plutil returns empty string -> falls back to Contents/MacOS
	plistPath := filepath.Join(appDir, "Contents", "Info.plist")
	_ = os.WriteFile(plistPath, []byte("valid plist"), 0o644)
	emptyPlutilRunner := &mockRunner{customOut: []byte("   \n")}
	name, err = GetAppBinaryName(appDir, emptyPlutilRunner)
	if err != nil || name != "MyBinary" {
		t.Fatalf("GetAppBinaryName with empty plutil output failed: got %q, err %v", name, err)
	}

	// GetAppBinaryName with Info.plist but plutil fails -> falls back to Contents/MacOS
	failPlutilRunner := &mockRunner{failNext: true}
	name, err = GetAppBinaryName(appDir, failPlutilRunner)
	if err != nil || name != "MyBinary" {
		t.Fatalf("GetAppBinaryName with failed plutil fallback failed: got %q, err %v", name, err)
	}

	// GetAppBinaryName empty dir error
	emptyApp := filepath.Join(tempDir, "Empty.app")
	_ = os.MkdirAll(filepath.Join(emptyApp, "Contents", "MacOS"), 0o755)
	if _, err := GetAppBinaryName(emptyApp, runner); err == nil {
		t.Fatalf("expected error on empty app binary search")
	}

	// PatchNativeApp and UnpatchNativeApp on empty app (binary name error)
	if err := PatchNativeApp(emptyApp, "Font", false, false, tempDir, runner); err == nil {
		t.Fatalf("expected error when binary name fails in PatchNativeApp")
	}
	if err := UnpatchNativeApp(emptyApp, false, false, runner); err == nil {
		t.Fatalf("expected error when binary name fails in UnpatchNativeApp")
	}

	// PatchNativeApp and UnpatchNativeApp permission check failure
	permAppPath := filepath.Join(tempDir, "PermFail.app")
	permMacos := filepath.Join(permAppPath, "Contents", "MacOS")
	_ = os.MkdirAll(permMacos, 0o555)
	_ = os.WriteFile(filepath.Join(permMacos, "Bin"), []byte("bin"), 0o555)
	if err := PatchNativeApp(permAppPath, "Font", false, false, tempDir, runner); err == nil {
		t.Fatalf("expected permission error in PatchNativeApp")
	}
	if err := UnpatchNativeApp(permAppPath, false, false, runner); err == nil {
		t.Fatalf("expected permission error in UnpatchNativeApp")
	}
	_ = os.Chmod(permMacos, 0o755)

	// Failure during codesign in PatchNativeApp
	failAppPath := filepath.Join(tempDir, "FailCodesign.app")
	_ = os.MkdirAll(filepath.Join(failAppPath, "Contents", "MacOS"), 0o755)
	_ = os.WriteFile(filepath.Join(failAppPath, "Contents", "MacOS", "FailApp"), []byte("bin"), 0o755)
	failRunner := &mockRunner{failNext: true}
	if err := PatchNativeApp(failAppPath, "Font", false, false, tempDir, failRunner); err == nil {
		t.Fatalf("expected error when runner fails in PatchNativeApp")
	}
	if err := UnpatchNativeApp(failAppPath, false, false, failRunner); err == nil {
		t.Fatalf("expected error when runner fails in UnpatchNativeApp")
	}

	// PatchNativeApp when BuildArtifacts fails
	failBuildRunner := &mockRunner{failNext: true}
	validAppPath := filepath.Join(tempDir, "ValidApp.app")
	_ = os.MkdirAll(filepath.Join(validAppPath, "Contents", "MacOS"), 0o755)
	_ = os.WriteFile(filepath.Join(validAppPath, "Contents", "MacOS", "ValidApp"), []byte("bin"), 0o755)
	if err := PatchNativeApp(validAppPath, "Font", false, false, filepath.Join(tempDir, "badcache"), failBuildRunner); err == nil {
		t.Fatalf("expected error when BuildArtifacts fails during PatchNativeApp")
	}

	// PatchNativeApp when launcher copy fails
	badLauncherApp := filepath.Join(tempDir, "BadLauncher.app")
	_ = os.MkdirAll(filepath.Join(badLauncherApp, "Contents", "MacOS", "BadBin"), 0o755)
	_ = os.WriteFile(filepath.Join(badLauncherApp, "Contents", "Info.plist"), []byte("plist"), 0o644)
	plutilBadRunner := &mockRunner{customOut: []byte("BadBin\n")}
	_ = PatchNativeApp(badLauncherApp, "Font", false, false, tempDir, plutilBadRunner)

	// PatchNativeApp when dylib copy fails
	badDylibApp := filepath.Join(tempDir, "BadDylib.app")
	badDylibMacos := filepath.Join(badDylibApp, "Contents", "MacOS")
	_ = os.MkdirAll(filepath.Join(badDylibMacos, "libfonthook.dylib"), 0o755) // directory, cannot overwrite with file
	_ = os.WriteFile(filepath.Join(badDylibMacos, "BadDylib"), []byte("bin"), 0o755)
	_ = PatchNativeApp(badDylibApp, "Font", false, false, tempDir, runner)
}
