package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexgorbatchev/godeps"
)

func TestUpgradeSelfFunctions(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	destPath := filepath.Join(tempDir, "better-fonts")

	latestVer, err := godeps.ResolveLatestTag(ctx, RepoOwner, RepoName)
	if err != nil {
		t.Skipf("skipping live selfupdate check in offline environment: %v", err)
	}

	// 1. Test UpgradeSelf with current latest version
	updated, ver, err := UpgradeSelf(ctx, latestVer)
	if err != nil {
		t.Logf("UpgradeSelf live check returned error: %v", err)
	} else {
		if updated {
			t.Errorf("expected updated=false when on latest %s, got true", latestVer)
		}
		if ver == "" {
			t.Errorf("expected non-empty version returned")
		}
	}

	// 2. Test UpgradeSelfToPath with current version (already up to date)
	updated, ver, err = UpgradeSelfToPath(ctx, latestVer, destPath)
	if err != nil {
		t.Logf("UpgradeSelfToPath live check returned error: %v", err)
	} else {
		if updated {
			t.Errorf("expected updated=false when on latest %s, got true", latestVer)
		}
		if ver == "" {
			t.Errorf("expected non-empty version returned")
		}
	}

	// 3. Test UpgradeSelfToPath with older version
	updated, ver, err = UpgradeSelfToPath(ctx, "0.0.1", destPath)
	if err != nil {
		t.Logf("UpgradeSelfToPath 0.0.1 returned error (network/asset layout dependent): %v", err)
	} else {
		if !updated {
			t.Errorf("expected updated=true for 0.0.1, got false")
		}
		if ver == "" {
			t.Errorf("expected non-empty version returned")
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
