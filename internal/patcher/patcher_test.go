package patcher

import (
	"strings"
	"testing"
)

func TestBuildInjectionSnippet(t *testing.T) {
	marker := "fonted-signal-patch"
	fontName := "Maple Mono Normal NF"

	snippet := BuildInjectionSnippet(marker, fontName)

	if !strings.Contains(snippet, "/*"+marker+"*/") {
		t.Fatalf("snippet missing marker %q: %s", marker, snippet)
	}
	if !strings.Contains(snippet, fontName) {
		t.Fatalf("snippet missing font name %q: %s", fontName, snippet)
	}
	if !strings.Contains(snippet, "DOMContentLoaded") {
		t.Fatalf("snippet missing DOMContentLoaded listener: %s", snippet)
	}
}

func TestInjectAndStripPatch(t *testing.T) {
	originalPreload := []byte(`
const electron = require('electron');
console.log('Signal preload init');
`)
	marker := "fonted-signal-patch"
	fontName := "Maple Mono Normal NF"

	// Patching
	patched := InjectPatch(originalPreload, marker, fontName)
	if !strings.Contains(string(patched), marker) {
		t.Fatalf("expected patched content to contain marker %q", marker)
	}

	isPatched, detected := DetectPatch(patched, marker)
	if !isPatched {
		t.Fatalf("DetectPatch reported false, expected true")
	}
	if detected != fontName {
		t.Fatalf("DetectPatch returned %q, want %q", detected, fontName)
	}

	// Patching again with different font should replace, not duplicate
	newFont := "JetBrains Mono"
	rePatched := InjectPatch(patched, marker, newFont)
	if strings.Count(string(rePatched), marker) != 1 {
		t.Fatalf("expected exactly 1 marker occurrence, got %d", strings.Count(string(rePatched), marker))
	}
	_, detectedNew := DetectPatch(rePatched, marker)
	if detectedNew != newFont {
		t.Fatalf("DetectPatch returned %q, want %q", detectedNew, newFont)
	}

	// Unpatching
	unpatched := StripPatch(rePatched, marker)
	if strings.Contains(string(unpatched), marker) {
		t.Fatalf("unpatched content still contains marker")
	}
	isPatchedAfter, _ := DetectPatch(unpatched, marker)
	if isPatchedAfter {
		t.Fatalf("DetectPatch reported true after unpatch")
	}

	// DetectPatch on unknown format (marker present without font match)
	unknownPatch := []byte("/*" + marker + "*/ console.log('no font-family');")
	isP, font := DetectPatch(unknownPatch, marker)
	if !isP || font != "unknown" {
		t.Fatalf("expected isP=true, font=unknown, got %v, %q", isP, font)
	}
}

func TestDetectPatch_Unpatched(t *testing.T) {
	content := []byte("console.log('no patch');")
	marker := "fonted-slack-patch"

	isPatched, font := DetectPatch(content, marker)
	if isPatched || font != "" {
		t.Fatalf("expected isPatched=false, font=\"\", got %v, %q", isPatched, font)
	}
}
