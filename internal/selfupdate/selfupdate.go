package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var defaultBaseURL = "https://github.com"

// SetDefaultBaseURL sets the base URL used by UpgradeSelf for testing.
func SetDefaultBaseURL(url string) func() {
	orig := defaultBaseURL
	defaultBaseURL = url
	return func() {
		defaultBaseURL = orig
	}
}

const (
	RepoOwner  = "alexgorbatchev"
	RepoName   = "better-fonts-cli"
	BinaryName = "better-fonts"
)

// UpgradeSelf checks for a newer release of better-fonts and replaces the running binary.
func UpgradeSelf(ctx context.Context, currentVersion string) (bool, string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return false, "", fmt.Errorf("locating current executable: %w", err)
	}

	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return false, "", fmt.Errorf("evaluating executable symlinks %q: %w", exePath, err)
	}

	return UpgradeSelfWithBaseURL(ctx, defaultBaseURL, currentVersion, exePath)
}

// UpgradeSelfWithBaseURL checks and upgrades the executable at targetExePath using a custom base URL.
func UpgradeSelfWithBaseURL(ctx context.Context, baseURL, currentVersion, targetExePath string) (bool, string, error) {
	tag, err := ResolveLatestTagWithBaseURL(ctx, baseURL, RepoOwner, RepoName)
	if err != nil {
		return false, "", fmt.Errorf("resolving latest release for %s/%s: %w", RepoOwner, RepoName, err)
	}

	latestVer := strings.TrimPrefix(tag, "v")
	cleanCurrentVer := strings.TrimPrefix(strings.TrimSpace(currentVersion), "v")

	// If running a release version and current >= latest, we are already up to date
	if cleanCurrentVer != "dev" && cleanCurrentVer != "" {
		if compErr := CompareVersions(cleanCurrentVer, latestVer); compErr == nil {
			return false, latestVer, nil
		}
	}

	targetDir := filepath.Dir(targetExePath)
	osName := runtime.GOOS
	archName := runtime.GOARCH

	assetName := fmt.Sprintf("%s_%s_%s_%s.tar.gz", BinaryName, latestVer, osName, archName)
	assetURL := fmt.Sprintf("%s/%s/%s/releases/download/%s/%s", strings.TrimRight(baseURL, "/"), RepoOwner, RepoName, tag, assetName)

	if err := downloadAndExtractAsset(ctx, assetURL, BinaryName, targetDir); err != nil {
		return false, "", fmt.Errorf("downloading and extracting upgrade asset: %w", err)
	}

	downloadedBin := filepath.Join(targetDir, BinaryName)

	// If targetExePath differs from downloadedBin (e.g. custom name or path in test), rename it
	if downloadedBin != targetExePath {
		if err := os.Rename(downloadedBin, targetExePath); err != nil {
			_ = os.Remove(downloadedBin)
			return false, "", fmt.Errorf("replacing binary at %q: %w", targetExePath, err)
		}
	}

	return true, latestVer, nil
}

// ResolveLatestTagWithBaseURL queries the latest release tag for a repository from a custom base URL.
func ResolveLatestTagWithBaseURL(ctx context.Context, baseURL, owner, repo string) (string, error) {
	targetURL := fmt.Sprintf("%s/%s/%s/releases/latest", strings.TrimRight(baseURL, "/"), owner, repo)

	client := &http.Client{
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating request for latest release: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil || (resp != nil && resp.StatusCode == http.StatusMethodNotAllowed) {
		reqGet, getErr := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if getErr == nil {
			if resp != nil {
				_ = resp.Body.Close()
			}
			resp, err = client.Do(reqGet)
		}
		if err != nil {
			return "", fmt.Errorf("fetching latest release redirect from %s: %w", targetURL, err)
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		return "", fmt.Errorf("unexpected status %d from %s", resp.StatusCode, targetURL)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no Location header returned from %s", targetURL)
	}

	parts := strings.Split(strings.TrimRight(location, "/"), "/")
	tag := parts[len(parts)-1]
	if tag == "" {
		return "", fmt.Errorf("failed to extract tag from Location %q", location)
	}

	return tag, nil
}

func downloadAndExtractAsset(ctx context.Context, assetURL, binName, targetDir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return fmt.Errorf("creating asset download request: %w", err)
	}

	client := &http.Client{
		Timeout: 3 * time.Minute,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading asset from %s: %w", assetURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("asset download failed with HTTP %d from %s", resp.StatusCode, assetURL)
	}

	destPath := filepath.Join(targetDir, binName)
	tmpPath := destPath + ".tmp"
	_ = os.Remove(tmpPath)

	gzReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("initializing gzip reader: %w", err)
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	found := false

	for {
		hdr, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar archive: %w", err)
		}

		base := filepath.Base(hdr.Name)
		if (hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA) && (base == binName || hdr.Name == binName) {
			outFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return fmt.Errorf("creating temporary binary %q: %w", tmpPath, err)
			}

			if _, err := io.Copy(outFile, tarReader); err != nil {
				_ = outFile.Close()
				_ = os.Remove(tmpPath)
				return fmt.Errorf("writing extracted binary: %w", err)
			}
			_ = outFile.Close()
			found = true
			break
		}
	}

	if !found {
		return fmt.Errorf("executable %q not found in tar archive", binName)
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("setting executable permissions: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("moving binary into place at %q: %w", destPath, err)
	}

	return nil
}

// CompareVersions checks if actual version meets or exceeds minVersion.
func CompareVersions(actualStr, minStr string) error {
	if actualStr == "dev" || strings.HasPrefix(actualStr, "N-") || strings.HasPrefix(actualStr, "git-") || strings.HasPrefix(actualStr, "DEV-") {
		return nil
	}

	actualParts := parseVersionParts(actualStr)
	minParts := parseVersionParts(minStr)

	if len(actualParts) == 0 {
		return fmt.Errorf("invalid version string %q", actualStr)
	}

	for i := 0; i < len(minParts); i++ {
		actVal := 0
		if i < len(actualParts) {
			actVal = actualParts[i]
		}
		minVal := minParts[i]

		if actVal > minVal {
			return nil
		}
		if actVal < minVal {
			return fmt.Errorf("version below minimum requirement")
		}
	}

	return nil
}

func parseVersionParts(v string) []int {
	v = strings.TrimPrefix(v, "v")
	if idx := strings.Index(v, "-"); idx != -1 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	var result []int
	for _, p := range parts {
		num, err := strconv.Atoi(p)
		if err == nil {
			result = append(result, num)
		}
	}
	return result
}
