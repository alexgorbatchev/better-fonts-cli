package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradeSelfFunctions(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	destPath := filepath.Join(tempDir, "better-fonts")

	// 1. Test UpgradeSelf with current version
	updated, ver, err := UpgradeSelf(ctx, "1.0.1")
	if err != nil {
		t.Logf("UpgradeSelf live check returned error (acceptable in offline CI): %v", err)
	} else {
		if updated {
			t.Errorf("expected updated=false when on latest 1.0.1, got true")
		}
		if ver == "" {
			t.Errorf("expected non-empty version returned")
		}
	}

	// 2. Test UpgradeSelfToPath with current version (already up to date)
	updated, ver, err = UpgradeSelfToPath(ctx, "1.0.1", destPath)
	if err != nil {
		t.Logf("UpgradeSelfToPath live check returned error: %v", err)
	} else {
		if updated {
			t.Errorf("expected updated=false when on latest 1.0.1, got true")
		}
		if ver == "" {
			t.Errorf("expected non-empty version returned")
		}
	}

	// 3. Test UpgradeSelfToPath with older version (0.0.1 < 1.0.1) -> downloads and upgrades!
	updated, ver, err = UpgradeSelfToPath(ctx, "0.0.1", destPath)
	if err != nil {
		t.Logf("UpgradeSelfToPath 0.0.1 returned error (network dependent): %v", err)
	} else {
		if !updated {
			t.Errorf("expected updated=true for 0.0.1, got false")
		}
		if ver != "1.0.1" {
			t.Errorf("expected upgraded version '1.0.1', got %q", ver)
		}
		if _, err := os.Stat(destPath); err != nil {
			t.Fatalf("expected upgraded binary at destPath: %v", err)
		}
	}

	// 4. Test error handling on impossible destination
	_, _, err = UpgradeSelfToPath(ctx, "0.0.1", "/dev/null/impossible/better-fonts")
	if err == nil {
		t.Fatalf("expected error on impossible destination")
	}
}
