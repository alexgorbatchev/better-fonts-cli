package sysutil

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type errorReaderAt struct{}

func (e errorReaderAt) ReadAt(p []byte, off int64) (int, error) {
	return 0, errors.New("disk read error")
}

func TestNativeElectronFuseDisable(t *testing.T) {
	tempDir := t.TempDir()
	frameworkDir := filepath.Join(tempDir, "Contents", "Frameworks", "Electron Framework.framework")
	if err := os.MkdirAll(frameworkDir, 0o755); err != nil {
		t.Fatalf("mkdir framework: %v", err)
	}

	binaryPath := filepath.Join(frameworkDir, "Electron Framework")

	// Create fake binary with sentinel and fuse wire
	var fakeBinary bytes.Buffer
	fakeBinary.Write(make([]byte, 1024)) // padding
	fakeBinary.WriteString(FuseSentinel)
	fakeBinary.WriteByte(1) // version V1
	fakeBinary.WriteByte(8) // wire length 8
	// Fuses: [0, 1, 2, 3, 4(EnableEmbeddedAsarIntegrityValidation), 5, 6, 7]
	// Initial state: EnableEmbeddedAsarIntegrityValidation is '1' (ENABLE)
	fakeBinary.Write([]byte{'0', '1', '0', '0', '1', '1', '0', '1'})
	fakeBinary.Write(make([]byte, 512)) // trailing padding

	if err := os.WriteFile(binaryPath, fakeBinary.Bytes(), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}

	// 1. Check current fuse state
	disabled, err := IsAsarIntegrityDisabled(tempDir)
	if err != nil {
		t.Fatalf("IsAsarIntegrityDisabled failed: %v", err)
	}
	if disabled {
		t.Fatalf("expected initial state to be enabled (disabled=false)")
	}

	// 2. Disable fuse natively in Go
	if err := DisableIntegrityValidationNative(tempDir); err != nil {
		t.Fatalf("DisableIntegrityValidationNative failed: %v", err)
	}

	// 3. Check fuse state after disable
	disabledAfter, err := IsAsarIntegrityDisabled(tempDir)
	if err != nil {
		t.Fatalf("IsAsarIntegrityDisabled after disable failed: %v", err)
	}
	if !disabledAfter {
		t.Fatalf("expected state to be disabled (disabled=true)")
	}

	// Verify byte 4 is now '0'
	content, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("reading binary: %v", err)
	}
	sentinelIdx := bytes.Index(content, []byte(FuseSentinel))
	if sentinelIdx == -1 {
		t.Fatalf("sentinel not found")
	}
	fuseByte := content[sentinelIdx+len(FuseSentinel)+2+4]
	if fuseByte != '0' {
		t.Fatalf("expected fuse byte '0', got %c (0x%02x)", fuseByte, fuseByte)
	}
}

func TestFuse_LargeBinarySearch(t *testing.T) {
	tempDir := t.TempDir()
	frameworkDir := filepath.Join(tempDir, "Contents", "Frameworks", "Electron Framework.framework")
	_ = os.MkdirAll(frameworkDir, 0o755)
	binPath := filepath.Join(frameworkDir, "Electron Framework")

	// Create binary > 128KB where sentinel is placed past the first 64KB chunk
	var largeBinary bytes.Buffer
	largeBinary.Write(make([]byte, 80*1024)) // 80KB offset
	largeBinary.WriteString(FuseSentinel)
	largeBinary.WriteByte(1)
	largeBinary.WriteByte(8)
	largeBinary.Write([]byte{'0', '1', '0', '0', '1', '1', '0', '1'})
	largeBinary.Write(make([]byte, 10*1024))

	if err := os.WriteFile(binPath, largeBinary.Bytes(), 0o755); err != nil {
		t.Fatalf("write large binary: %v", err)
	}

	disabled, err := IsAsarIntegrityDisabled(tempDir)
	if err != nil || disabled {
		t.Fatalf("IsAsarIntegrityDisabled on large binary failed: %v, disabled=%v", err, disabled)
	}
}

func TestFuse_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Missing framework binary
	if _, err := FindElectronFrameworkBinary(tempDir); err == nil {
		t.Fatalf("expected error finding missing framework binary")
	}
	if _, err := IsAsarIntegrityDisabled(tempDir); err == nil {
		t.Fatalf("expected error checking fuse on missing framework")
	}
	if err := DisableIntegrityValidationNative(tempDir); err == nil {
		t.Fatalf("expected error disabling fuse on missing framework")
	}

	// 2. Binary without sentinel in Versions/Current
	fwDir := filepath.Join(tempDir, "Contents", "Frameworks", "Electron Framework.framework", "Versions", "Current")
	_ = os.MkdirAll(fwDir, 0o755)
	binPath := filepath.Join(fwDir, "Electron Framework")
	_ = os.WriteFile(binPath, []byte("no sentinel in this binary"), 0o755)

	foundPath, err := FindElectronFrameworkBinary(tempDir)
	if err != nil || foundPath != binPath {
		t.Fatalf("FindElectronFrameworkBinary Versions/Current failed: %v, %s", err, foundPath)
	}

	if _, err := IsAsarIntegrityDisabled(tempDir); err == nil {
		t.Fatalf("expected sentinel not found error")
	}
	if err := DisableIntegrityValidationNative(tempDir); err == nil {
		t.Fatalf("expected sentinel not found error on disable")
	}

	// 3. Truncated binary immediately after sentinel (header read error)
	_ = os.WriteFile(binPath, []byte(FuseSentinel), 0o755)
	if _, err := IsAsarIntegrityDisabled(tempDir); err == nil {
		t.Fatalf("expected error on truncated fuse binary")
	}
	if err := DisableIntegrityValidationNative(tempDir); err == nil {
		t.Fatalf("expected error on truncated fuse binary disable")
	}

	// 4. Binary with short fuse wire (< 5 bytes)
	var shortWire bytes.Buffer
	shortWire.WriteString(FuseSentinel)
	shortWire.WriteByte(1) // version
	shortWire.WriteByte(2) // wire length 2 (too short for index 4)
	shortWire.Write([]byte{'0', '1'})
	_ = os.WriteFile(binPath, shortWire.Bytes(), 0o755)

	if _, err := IsAsarIntegrityDisabled(tempDir); err == nil {
		t.Fatalf("expected wire length too short error")
	}
	if err := DisableIntegrityValidationNative(tempDir); err == nil {
		t.Fatalf("expected wire length too short error on disable")
	}

	// 5. Test candidate search in Frameworks directory with other .framework folder
	customAppDir := filepath.Join(tempDir, "Custom.app")
	customFw := filepath.Join(customAppDir, "Contents", "Frameworks", "CustomHelper.framework")
	_ = os.MkdirAll(customFw, 0o755)
	_ = os.WriteFile(filepath.Join(customFw, "CustomHelper"), []byte("bin"), 0o755)

	foundCustom, err := FindElectronFrameworkBinary(customAppDir)
	if err != nil || !filepath.IsAbs(foundCustom) {
		t.Fatalf("expected to find framework candidate: %v, %s", err, foundCustom)
	}

	// 6. findSentinelOffset with ReaderAt error
	if _, err := findSentinelOffset(errorReaderAt{}); err == nil {
		t.Fatalf("expected error from errorReaderAt in findSentinelOffset")
	}
}
