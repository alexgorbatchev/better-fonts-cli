package patcher

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
)

// BuildFontCSS returns the CSS rules applying the font family while preserving font icons.
func BuildFontCSS(fontName string) string {
	return fmt.Sprintf(
		`*:not([data-cds*="Icon"]):not([data-cds*="Icon"] *):not([class*="icon" i]):not([class*="Icon"]):not([data-icon]):not(i):not(svg):not(svg *) { font-family: "%s", monospace !important; } `+
			`[data-cds*="Icon"], [data-cds*="Icon"] * { font-family: var(--font-anthropicons, Anthropicons-Variable), sans-serif !important; }`,
		fontName,
	)
}

// BuildInjectionSnippet creates the self-executing JS function with CSS injection.
func BuildInjectionSnippet(marker string, fontName string) string {
	fontCSS := BuildFontCSS(fontName)
	escapedCSS := strings.ReplaceAll(fontCSS, `\`, `\\`)
	escapedCSS = strings.ReplaceAll(escapedCSS, `'`, `\'`)
	escapedCSS = strings.ReplaceAll(escapedCSS, "\n", `\n`)

	return fmt.Sprintf(
		"\n;/*%s*/(function(){document.addEventListener(\"DOMContentLoaded\",function(){var s=document.createElement(\"style\");s.textContent='%s';document.head.appendChild(s)})})();\n",
		marker,
		escapedCSS,
	)
}

// StripPatch removes any previously injected snippet with the given marker.
func StripPatch(content []byte, marker string) []byte {
	escapedMarker := regexp.QuoteMeta(marker)
	patternStr := `(?m)^\s*;\s*/\*` + escapedMarker + `\*/.*\n?|/\*` + escapedMarker + `\*/[^\n]*`
	pattern := regexp.MustCompile(patternStr)
	cleaned := pattern.ReplaceAll(content, nil)
	return bytes.TrimRight(cleaned, "\n")
}

// InjectPatch strips any previous patch and appends a new marked injection snippet.
func InjectPatch(content []byte, marker string, fontName string) []byte {
	cleaned := StripPatch(content, marker)
	snippet := BuildInjectionSnippet(marker, fontName)
	var buf bytes.Buffer
	buf.Write(cleaned)
	buf.WriteString(snippet)
	return buf.Bytes()
}

var fontRegex = regexp.MustCompile(`font-family:\s*\\?"([^\\"]+)\\?"`)

// DetectPatch checks whether content contains the marker and extracts the patched font.
func DetectPatch(content []byte, marker string) (bool, string) {
	str := string(content)
	if !strings.Contains(str, "/*"+marker+"*/") {
		return false, ""
	}

	matches := fontRegex.FindStringSubmatch(str)
	if len(matches) > 1 {
		return true, matches[1]
	}

	return true, "unknown"
}
