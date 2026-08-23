package sysutil

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type mockRunner struct {
	calls    [][]string
	failNext bool
}

func (m *mockRunner) Run(name string, args ...string) error {
	call := append([]string{name}, args...)
	m.calls = append(m.calls, call)
	if m.failNext {
		return errors.New("mock runner failure")
	}
	return nil
}

func (m *mockRunner) Output(name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	m.calls = append(m.calls, call)
	if m.failNext {
		return nil, errors.New("mock runner failure")
	}
	return []byte("mock output"), nil
}

func TestCopyDirAndFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := filepath.Join(t.TempDir(), "copied")

	subDir := filepath.Join(srcDir, "subdir")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	file1 := filepath.Join(srcDir, "file1.txt")
	file2 := filepath.Join(subDir, "file2.txt")
	if err := os.WriteFile(file1, []byte("hello 1"), 0o644); err != nil {
		t.Fatalf("write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("hello 2"), 0o644); err != nil {
		t.Fatalf("write file2: %v", err)
	}

	symlinkPath := filepath.Join(srcDir, "link_to_file1")
	if err := os.Symlink(file1, symlinkPath); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if err := CopyDir(srcDir, dstDir); err != nil {
		t.Fatalf("CopyDir failed: %v", err)
	}

	got1, err := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	if err != nil || string(got1) != "hello 1" {
		t.Fatalf("file1 mismatch: got %q, err %v", string(got1), err)
	}

	got2, err := os.ReadFile(filepath.Join(dstDir, "subdir", "file2.txt"))
	if err != nil || string(got2) != "hello 2" {
		t.Fatalf("file2 mismatch: got %q, err %v", string(got2), err)
	}

	// Verify symlink was copied
	linkTarget, err := os.Readlink(filepath.Join(dstDir, "link_to_file1"))
	if err != nil || linkTarget != file1 {
		t.Fatalf("symlink mismatch: got %q, err %v", linkTarget, err)
	}

	// Nested dir copy
	deepDir := filepath.Join(subDir, "deep")
	_ = os.MkdirAll(deepDir, 0o755)
	_ = os.WriteFile(filepath.Join(deepDir, "deep.txt"), []byte("deep"), 0o644)
	dstDir2 := filepath.Join(t.TempDir(), "copied2")
	if err := CopyDir(srcDir, dstDir2); err != nil {
		t.Fatalf("CopyDir deep failed: %v", err)
	}

	// Symlink creation error on existing destination
	badDst := filepath.Join(t.TempDir(), "badDst")
	_ = os.MkdirAll(badDst, 0o755)
	_ = os.WriteFile(filepath.Join(badDst, "link_to_file1"), []byte("exists"), 0o644)
	if err := CopyDir(srcDir, badDst); err == nil {
		t.Fatalf("expected error when symlink destination already exists")
	}

	// Copy non-existent dir/file error handling
	if err := CopyDir(filepath.Join(srcDir, "missing"), dstDir); err == nil {
		t.Fatalf("expected error copying missing dir")
	}
	if err := CopyFile(filepath.Join(srcDir, "missing.txt"), filepath.Join(dstDir, "x.txt")); err == nil {
		t.Fatalf("expected error copying missing file")
	}
	if err := CopyFile(file1, "/dev/null/impossible/x.txt"); err == nil {
		t.Fatalf("expected error writing file to impossible destination")
	}
}

func TestCodesignAndFusesWithMock(t *testing.T) {
	runner := &mockRunner{}

	if err := Codesign("/Applications/Test.app", runner); err != nil {
		t.Fatalf("Codesign failed: %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "codesign" {
		t.Fatalf("expected codesign call, got %v", runner.calls)
	}

	runner.failNext = true
	if err := Codesign("/Applications/Test.app", runner); err == nil {
		t.Fatalf("expected codesign to fail when runner fails")
	}
	runner.failNext = false

	runner.calls = nil
	if err := QuitApp("TestApp", runner); err != nil {
		t.Fatalf("QuitApp failed: %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "osascript" {
		t.Fatalf("expected osascript quit call, got %v", runner.calls)
	}

	runner.calls = nil
	if err := StartApp("TestApp", runner); err != nil {
		t.Fatalf("StartApp failed: %v", err)
	}
	if len(runner.calls) != 1 || runner.calls[0][0] != "open" {
		t.Fatalf("expected open call, got %v", runner.calls)
	}

	runner.failNext = true
	if err := StartApp("TestApp", runner); err == nil {
		t.Fatalf("expected StartApp to fail when runner fails")
	}
	runner.failNext = false

	// Test DisableIntegrityValidation wrapper
	tempDir := t.TempDir()
	frameworkDir := filepath.Join(tempDir, "Contents", "Frameworks", "Electron Framework.framework")
	_ = os.MkdirAll(frameworkDir, 0o755)
	binPath := filepath.Join(frameworkDir, "Electron Framework")
	var fakeBinary bytes.Buffer
	fakeBinary.WriteString(FuseSentinel)
	fakeBinary.WriteByte(1)
	fakeBinary.WriteByte(8)
	fakeBinary.Write([]byte{'0', '1', '0', '0', '1', '1', '0', '1'})
	_ = os.WriteFile(binPath, fakeBinary.Bytes(), 0o755)

	if err := DisableIntegrityValidation(tempDir, runner); err != nil {
		t.Fatalf("DisableIntegrityValidation failed: %v", err)
	}

	// DisableIntegrityValidation with non-existent electron framework calls fallback
	emptyDir := t.TempDir()
	_ = DisableIntegrityValidation(emptyDir, runner)

	// DisableIntegrityValidation when native fails and PATH is empty
	t.Setenv("PATH", "")
	if err := DisableIntegrityValidation(emptyDir, runner); err == nil {
		t.Fatalf("expected error when native fails and PATH is empty")
	}
}

func TestDefaultRunner(t *testing.T) {
	// Test defaultRunner with simple echo command
	runner := DefaultRunner
	if err := runner.Run("echo", "hello"); err != nil {
		t.Fatalf("Run echo failed: %v", err)
	}

	out, err := runner.Output("echo", "world")
	if err != nil {
		t.Fatalf("Output echo failed: %v", err)
	}
	if string(out) != "world\n" {
		t.Fatalf("got %q, want %q", string(out), "world\n")
	}

	// Error handling
	if err := runner.Run("non_existent_command_12345"); err == nil {
		t.Fatalf("expected error running non-existent command")
	}
	if _, err := runner.Output("non_existent_command_12345"); err == nil {
		t.Fatalf("expected error output on non-existent command")
	}
}

func TestFindNodeOrBunRunner(t *testing.T) {
	prefix, err := FindNodeOrBunRunner()
	if err != nil {
		t.Logf("no node/bun found: %v", err)
	} else if len(prefix) == 0 {
		t.Fatalf("empty runner prefix returned")
	}

	// Test with dummy npx in custom PATH (without bun)
	tempBin := t.TempDir()
	dummyNpx := filepath.Join(tempBin, "npx")
	_ = os.WriteFile(dummyNpx, []byte("#!/bin/sh\nexit 0\n"), 0o755)
	t.Setenv("PATH", tempBin)
	pfx, err := FindNodeOrBunRunner()
	if err != nil || len(pfx) == 0 || pfx[0] != "npx" {
		t.Fatalf("expected npx runner prefix: got %v, err %v", pfx, err)
	}

	// Test with empty PATH -> returns error
	t.Setenv("PATH", "")
	if _, err := FindNodeOrBunRunner(); err == nil {
		t.Fatalf("expected error when PATH is empty")
	}
}
