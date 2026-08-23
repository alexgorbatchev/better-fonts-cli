package asar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAsarCreateExtractReplace(t *testing.T) {
	tempDir := t.TempDir()
	asarPath := filepath.Join(tempDir, "test.asar")

	files := map[string][]byte{
		"index.js":        []byte("console.log('hello world');"),
		"dist/preload.js": []byte("/* original preload */\nconst x = 1;\n"),
		"assets/info.txt": []byte("some info data"),
	}

	// Create an archive from scratch
	arch, err := CreateArchive(files)
	if err != nil {
		t.Fatalf("CreateArchive failed: %v", err)
	}

	// Test ExtractFile directly on in-memory archive
	extractedMem, err := arch.ExtractFile("index.js")
	if err != nil || string(extractedMem) != string(files["index.js"]) {
		t.Fatalf("ExtractFile on in-memory arch failed: %v, %s", err, string(extractedMem))
	}

	if err := arch.Save(asarPath); err != nil {
		t.Fatalf("arch.Save failed: %v", err)
	}

	// Read archive back
	loaded, err := OpenArchive(asarPath)
	if err != nil {
		t.Fatalf("OpenArchive failed: %v", err)
	}
	defer loaded.Close()

	// Verify file list
	list := loaded.ListFiles()
	expectedFiles := []string{"assets/info.txt", "dist/preload.js", "index.js"}
	if len(list) != len(expectedFiles) {
		t.Fatalf("ListFiles returned %v, expected %v", list, expectedFiles)
	}

	// Verify extraction
	for name, wantContent := range files {
		got, err := loaded.ExtractFile(name)
		if err != nil {
			t.Fatalf("ExtractFile(%q) failed: %v", name, err)
		}
		if !bytes.Equal(got, wantContent) {
			t.Fatalf("ExtractFile(%q) = %q, want %q", name, string(got), string(wantContent))
		}
	}

	// Test replacing a file
	newPreload := []byte("/* patched preload */\nconst x = 2;\n")
	if err := loaded.ReplaceFile("dist/preload.js", newPreload); err != nil {
		t.Fatalf("ReplaceFile failed: %v", err)
	}

	replacedAsarPath := filepath.Join(tempDir, "replaced.asar")
	if err := loaded.Save(replacedAsarPath); err != nil {
		t.Fatalf("Save replaced asar failed: %v", err)
	}

	// Read replaced archive back
	loadedReplaced, err := OpenArchive(replacedAsarPath)
	if err != nil {
		t.Fatalf("OpenArchive replaced failed: %v", err)
	}
	defer loadedReplaced.Close()

	gotPreload, err := loadedReplaced.ExtractFile("dist/preload.js")
	if err != nil {
		t.Fatalf("ExtractFile replaced preload failed: %v", err)
	}
	if !bytes.Equal(gotPreload, newPreload) {
		t.Fatalf("ExtractFile replaced preload = %q, want %q", string(gotPreload), string(newPreload))
	}

	// Verify other files were not corrupted
	gotIndex, err := loadedReplaced.ExtractFile("index.js")
	if err != nil {
		t.Fatalf("ExtractFile index.js failed: %v", err)
	}
	if !bytes.Equal(gotIndex, files["index.js"]) {
		t.Fatalf("ExtractFile index.js after replace = %q, want %q", string(gotIndex), string(files["index.js"]))
	}
}

func TestPatchAsarFile(t *testing.T) {
	tempDir := t.TempDir()
	originalPath := filepath.Join(tempDir, "orig.asar")
	patchedPath := filepath.Join(tempDir, "patched.asar")

	files := map[string][]byte{
		"preload.js": []byte("console.log('original');"),
		"main.js":    []byte("console.log('main');"),
	}

	arch, err := CreateArchive(files)
	if err != nil {
		t.Fatalf("CreateArchive failed: %v", err)
	}
	if err := arch.Save(originalPath); err != nil {
		t.Fatalf("Save original failed: %v", err)
	}

	err = PatchAsarFile(originalPath, "preload.js", patchedPath, func(old []byte) ([]byte, error) {
		return append(old, []byte("\n/* injected */")...), nil
	})
	if err != nil {
		t.Fatalf("PatchAsarFile failed: %v", err)
	}

	loaded, err := OpenArchive(patchedPath)
	if err != nil {
		t.Fatalf("OpenArchive patched failed: %v", err)
	}
	defer loaded.Close()

	content, err := loaded.ExtractFile("preload.js")
	if err != nil {
		t.Fatalf("ExtractFile failed: %v", err)
	}
	want := "console.log('original');\n/* injected */"
	if string(content) != want {
		t.Fatalf("got %q, want %q", string(content), want)
	}
}

func TestAsar_ErrorPaths(t *testing.T) {
	tempDir := t.TempDir()

	// 1. OpenArchive non-existent file
	if _, err := OpenArchive(filepath.Join(tempDir, "missing.asar")); err == nil {
		t.Fatalf("expected error opening missing asar")
	}

	// 2. OpenArchive file too short
	shortFile := filepath.Join(tempDir, "short.asar")
	_ = os.WriteFile(shortFile, []byte("too short"), 0o644)
	if _, err := OpenArchive(shortFile); err == nil {
		t.Fatalf("expected error opening short asar")
	}

	// 3. OpenArchive invalid magic
	badMagicFile := filepath.Join(tempDir, "badmagic.asar")
	badMagic := make([]byte, 16)
	_ = os.WriteFile(badMagicFile, badMagic, 0o644)
	if _, err := OpenArchive(badMagicFile); err == nil {
		t.Fatalf("expected error opening bad magic asar")
	}

	// 4. Bad JSON header
	badJsonFile := filepath.Join(tempDir, "badjson.asar")
	var badHdr bytes.Buffer
	_ = binary.Write(&badHdr, binary.LittleEndian, uint32(4))
	_ = binary.Write(&badHdr, binary.LittleEndian, uint32(16))
	_ = binary.Write(&badHdr, binary.LittleEndian, uint32(12))
	_ = binary.Write(&badHdr, binary.LittleEndian, uint32(8))
	badHdr.WriteString("{invalid")
	_ = os.WriteFile(badJsonFile, badHdr.Bytes(), 0o644)
	if _, err := OpenArchive(badJsonFile); err == nil {
		t.Fatalf("expected error decoding bad JSON header")
	}

	// 5. Create valid asar with subdirs and test errors
	asarPath := filepath.Join(tempDir, "valid.asar")
	arch, err := CreateArchive(map[string][]byte{
		"sub/dir/file.txt": []byte("content"),
	})
	if err != nil {
		t.Fatalf("CreateArchive: %v", err)
	}
	if err := arch.Save(asarPath); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := OpenArchive(asarPath)
	if err != nil {
		t.Fatalf("OpenArchive: %v", err)
	}
	defer loaded.Close()

	// Extract non-existent file
	if _, err := loaded.ExtractFile("missing.txt"); err == nil {
		t.Fatalf("expected error extracting missing file")
	}

	// Extract directory as file
	if _, err := loaded.ExtractFile("sub/dir"); err == nil {
		t.Fatalf("expected error extracting directory")
	}

	// Replace non-existent file
	if err := loaded.ReplaceFile("missing.txt", []byte("xyz")); err == nil {
		t.Fatalf("expected error replacing missing file")
	}

	// Test PatchAsarFile error on missing file
	err = PatchAsarFile(filepath.Join(tempDir, "missing.asar"), "file.txt", filepath.Join(tempDir, "out.asar"), func(old []byte) ([]byte, error) {
		return old, nil
	})
	if err == nil {
		t.Fatalf("expected PatchAsarFile to fail on missing asar")
	}

	// Test PatchAsarFile transform error
	err = PatchAsarFile(asarPath, "sub/dir/file.txt", filepath.Join(tempDir, "out.asar"), func(old []byte) ([]byte, error) {
		return nil, errors.New("transform error")
	})
	if err == nil {
		t.Fatalf("expected PatchAsarFile to fail on transform error")
	}

	// Close on empty archive
	var emptyArch Archive
	if err := emptyArch.Close(); err != nil {
		t.Fatalf("emptyArch.Close: %v", err)
	}
}

func TestElectronAsarInteroperability(t *testing.T) {
	if _, err := exec.LookPath("bun"); err != nil {
		t.Skip("bun not found, skipping interoperability test")
	}

	tempDir := t.TempDir()
	asarPath := filepath.Join(tempDir, "go-created.asar")
	extractedDir := filepath.Join(tempDir, "electron-extracted")

	files := map[string][]byte{
		"index.js":        []byte("const msg = 'hello';\n"),
		"sub/deep/doc.md": []byte("# Deep Markdown\n"),
	}

	arch, err := CreateArchive(files)
	if err != nil {
		t.Fatalf("CreateArchive failed: %v", err)
	}
	if err := arch.Save(asarPath); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Use bun x @electron/asar extract to verify @electron/asar can unpack our asar
	cmd := exec.Command("bun", "x", "@electron/asar", "extract", asarPath, extractedDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bun x @electron/asar extract failed: %v, output: %s", err, string(out))
	}

	// Verify extracted files match
	docContent, err := exec.Command("cat", filepath.Join(extractedDir, "sub/deep/doc.md")).Output()
	if err != nil {
		t.Fatalf("reading extracted sub/deep/doc.md: %v", err)
	}
	if string(docContent) != string(files["sub/deep/doc.md"]) {
		t.Fatalf("content mismatch: got %q, want %q", string(docContent), string(files["sub/deep/doc.md"]))
	}
}
