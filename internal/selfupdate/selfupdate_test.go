package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createTestTarGz(t *testing.T, binName, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	body := []byte(content)
	hdr := &tar.Header{
		Name:     binName,
		Mode:     0o755,
		Size:     int64(len(body)),
		Typeflag: tar.TypeReg,
	}

	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("writing tar header: %v", err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatalf("writing tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}

	return buf.Bytes()
}

func TestUpgradeSelfToPath(t *testing.T) {
	tempDir := t.TempDir()
	targetExe := filepath.Join(tempDir, "custom-better-fonts")

	oldContent := "#!/bin/sh\necho old-version\n"
	if err := os.WriteFile(targetExe, []byte(oldContent), 0o755); err != nil {
		t.Fatalf("failed to write initial binary: %v", err)
	}

	newContent := "#!/bin/sh\necho 2.0.0\n"
	tarGzData := createTestTarGz(t, "better-fonts", newContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alexgorbatchev/better-fonts-cli/releases/latest":
			http.Redirect(w, r, "/alexgorbatchev/better-fonts-cli/releases/tag/v2.0.0", http.StatusFound)
		case "/alexgorbatchev/better-fonts-cli/releases/download/v2.0.0/better-fonts_2.0.0_darwin_arm64.tar.gz",
			"/alexgorbatchev/better-fonts-cli/releases/download/v2.0.0/better-fonts_2.0.0_darwin_amd64.tar.gz":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	ctx := context.Background()

	// 1. When current version is older (1.0.0 < 2.0.0), it should upgrade
	updated, newVer, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "1.0.0", targetExe)
	if err != nil {
		t.Fatalf("UpgradeSelfWithBaseURL() error = %v", err)
	}
	if !updated {
		t.Errorf("expected updated = true, got false")
	}
	if newVer != "2.0.0" {
		t.Errorf("expected newVer = '2.0.0', got %q", newVer)
	}

	// Verify target executable was replaced with new content
	data, err := os.ReadFile(targetExe)
	if err != nil {
		t.Fatalf("failed to read upgraded binary: %v", err)
	}
	if string(data) != newContent {
		t.Errorf("binary content = %q, want %q", string(data), newContent)
	}

	// 2. When current version is already 2.0.0, it should report up to date
	updated, sameVer, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "2.0.0", targetExe)
	if err != nil {
		t.Fatalf("UpgradeSelfWithBaseURL() error = %v", err)
	}
	if updated {
		t.Errorf("expected updated = false when already on latest version, got true")
	}
	if sameVer != "2.0.0" {
		t.Errorf("expected sameVer = '2.0.0', got %q", sameVer)
	}
}

func TestUpgradeSelf(t *testing.T) {
	newContent := "#!/bin/sh\necho 2.0.0\n"
	tarGzData := createTestTarGz(t, "better-fonts", newContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alexgorbatchev/better-fonts-cli/releases/latest":
			http.Redirect(w, r, "/alexgorbatchev/better-fonts-cli/releases/tag/v2.0.0", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		}
	}))
	defer ts.Close()

	restore := SetDefaultBaseURL(ts.URL)
	defer restore()

	ctx := context.Background()
	// Test when current version is already 2.0.0 (up to date)
	updated, ver, err := UpgradeSelf(ctx, "2.0.0")
	if err != nil {
		t.Fatalf("UpgradeSelf() error = %v", err)
	}
	if updated {
		t.Errorf("expected updated = false, got true")
	}
	if ver != "2.0.0" {
		t.Errorf("expected ver = '2.0.0', got %q", ver)
	}
}

func TestUpgradeSelf_DevVersion(t *testing.T) {
	tempDir := t.TempDir()
	targetExe := filepath.Join(tempDir, "better-fonts")

	newContent := "#!/bin/sh\necho 2.0.0\n"
	tarGzData := createTestTarGz(t, "better-fonts", newContent)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/alexgorbatchev/better-fonts-cli/releases/latest":
			http.Redirect(w, r, "/alexgorbatchev/better-fonts-cli/releases/tag/v2.0.0", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		}
	}))
	defer ts.Close()

	ctx := context.Background()
	updated, ver, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "dev", targetExe)
	if err != nil {
		t.Fatalf("UpgradeSelfWithBaseURL(dev) error = %v", err)
	}
	if !updated {
		t.Errorf("expected updated = true for 'dev' version, got false")
	}
	if ver != "2.0.0" {
		t.Errorf("expected ver = '2.0.0', got %q", ver)
	}
}

func TestUpgradeSelf_Errors(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	targetExe := filepath.Join(tempDir, "better-fonts")

	t.Run("resolve_tag_error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}))
		defer ts.Close()

		_, _, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "1.0.0", targetExe)
		if err == nil {
			t.Fatal("expected error when resolving tag fails")
		}
	})

	t.Run("download_asset_error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/releases/latest") {
				http.Redirect(w, r, "/alexgorbatchev/better-fonts-cli/releases/tag/v2.0.0", http.StatusFound)
				return
			}
			http.Error(w, "download error", http.StatusInternalServerError)
		}))
		defer ts.Close()

		_, _, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "1.0.0", targetExe)
		if err == nil {
			t.Fatal("expected error when downloading asset fails")
		}
	})

	t.Run("missing_binary_in_archive", func(t *testing.T) {
		tarGzData := createTestTarGz(t, "different-binary-name", "echo hi")
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/releases/latest") {
				http.Redirect(w, r, "/alexgorbatchev/better-fonts-cli/releases/tag/v2.0.0", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		}))
		defer ts.Close()

		_, _, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "1.0.0", targetExe)
		if err == nil {
			t.Fatal("expected error when binary is missing in tar archive")
		}
	})

	t.Run("corrupted_gzip_archive", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/releases/latest") {
				http.Redirect(w, r, "/alexgorbatchev/better-fonts-cli/releases/tag/v2.0.0", http.StatusFound)
				return
			}
			_, _ = w.Write([]byte("not a gzip"))
		}))
		defer ts.Close()

		_, _, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "1.0.0", targetExe)
		if err == nil {
			t.Fatal("expected error on corrupted gzip")
		}
	})

	t.Run("missing_location_header", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusFound) // 302 without Location
		}))
		defer ts.Close()

		_, err := ResolveLatestTagWithBaseURL(ctx, ts.URL, "alexgorbatchev", "better-fonts-cli")
		if err == nil {
			t.Fatal("expected error on missing Location header")
		}
	})

	t.Run("empty_tag_in_location", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "/")
			w.WriteHeader(http.StatusFound)
		}))
		defer ts.Close()

		_, err := ResolveLatestTagWithBaseURL(ctx, ts.URL, "alexgorbatchev", "better-fonts-cli")
		if err == nil {
			t.Fatal("expected error on empty tag in location")
		}
	})

	t.Run("download_to_impossible_destination", func(t *testing.T) {
		newContent := "#!/bin/sh\necho 2.0.0\n"
		tarGzData := createTestTarGz(t, "better-fonts", newContent)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/releases/latest") {
				http.Redirect(w, r, "/alexgorbatchev/better-fonts-cli/releases/tag/v2.0.0", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		}))
		defer ts.Close()

		_, _, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "1.0.0", "/dev/null/impossible/better-fonts")
		if err == nil {
			t.Fatal("expected error when writing to impossible directory")
		}
	})

	t.Run("download_to_readonly_directory", func(t *testing.T) {
		newContent := "#!/bin/sh\necho 2.0.0\n"
		tarGzData := createTestTarGz(t, "better-fonts", newContent)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/releases/latest") {
				http.Redirect(w, r, "/alexgorbatchev/better-fonts-cli/releases/tag/v2.0.0", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		}))
		defer ts.Close()

		roDir := filepath.Join(tempDir, "ro_dest")
		_ = os.MkdirAll(roDir, 0o555)
		_ = downloadAndExtractAsset(ctx, ts.URL+"/releases/download/v2.0.0/better-fonts_2.0.0_darwin_arm64.tar.gz", "better-fonts", roDir)
		_ = os.Chmod(roDir, 0o755)
	})

	t.Run("resolve_latest_tag_get_fallback", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			http.Redirect(w, r, "/alexgorbatchev/better-fonts-cli/releases/tag/v2.0.0", http.StatusFound)
		}))
		defer ts.Close()

		tag, err := ResolveLatestTagWithBaseURL(ctx, ts.URL, "alexgorbatchev", "better-fonts-cli")
		if err != nil || tag != "v2.0.0" {
			t.Fatalf("expected v2.0.0 from GET fallback, got %q, err %v", tag, err)
		}
	})

	t.Run("target_rename_error", func(t *testing.T) {
		newContent := "#!/bin/sh\necho 2.0.0\n"
		tarGzData := createTestTarGz(t, "better-fonts", newContent)
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/releases/latest") {
				http.Redirect(w, r, "/alexgorbatchev/better-fonts-cli/releases/tag/v2.0.0", http.StatusFound)
				return
			}
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarGzData)
		}))
		defer ts.Close()

		destDir := filepath.Join(tempDir, "dest_rename_test")
		_ = os.MkdirAll(destDir, 0o755)
		// targetExePath in a non-existent subfolder
		badTarget := filepath.Join(destDir, "sub_impossible", "target")
		_, _, err := UpgradeSelfWithBaseURL(ctx, ts.URL, "1.0.0", badTarget)
		if err == nil {
			t.Fatal("expected error on target rename failure")
		}
	})
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		actual  string
		min     string
		wantErr bool
	}{
		{"1.0.0", "1.0.0", false},
		{"1.1.0", "1.0.0", false},
		{"2.0.0", "1.9.9", false},
		{"0.9.0", "1.0.0", true},
		{"dev", "1.0.0", false},
		{"v1.2.3", "v1.2.0", false},
		{"invalid", "1.0.0", true},
	}

	for _, tt := range tests {
		err := CompareVersions(tt.actual, tt.min)
		if (err != nil) != tt.wantErr {
			t.Errorf("CompareVersions(%q, %q) err = %v, wantErr = %v", tt.actual, tt.min, err, tt.wantErr)
		}
	}
}
