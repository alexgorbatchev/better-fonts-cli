package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionOutputContract(t *testing.T) {
	cmd := NewRootCommand("1.2.3")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	got := buf.String()
	want := "1.2.3\n"
	if got != want {
		t.Fatalf("version output violation: got %q, want %q", got, want)
	}
}

func TestListCommand(t *testing.T) {
	cmd := NewRootCommand("dev")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"list"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute list failed: %v", err)
	}

	out := buf.String()
	for _, appName := range []string{"Paseo", "Signal", "Slack", "Rekordbox", "Engine DJ", "Telegram"} {
		if !strings.Contains(out, appName) {
			t.Errorf("list output missing app %q: %s", appName, out)
		}
	}
}

func TestStatusCommand(t *testing.T) {
	cmd := NewRootCommand("dev")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"status"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute status failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "APP") || !strings.Contains(out, "DRIVER") || !strings.Contains(out, "INSTALLED") {
		t.Fatalf("status output header missing: %s", out)
	}
}

func TestPatchAndUnpatchCommands_DryRun(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")

	// 1. Dry run patch all
	cmd := NewRootCommand("dev")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"patch", "--config", configPath, "--dry-run", "--font", "CustomFont", "--app", "slack,signal", "--restart"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("patch dry-run failed: %v", err)
	}

	// 2. Patch with wildcard argument
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"patch", "--config", configPath, "--dry-run", "*"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("patch * failed: %v", err)
	}

	// 3. Patch targeting specific apps
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"patch", "--config", configPath, "--dry-run", "slack", "telegram"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("patch specific apps failed: %v", err)
	}

	// 4. Patch non-existent target
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"patch", "--config", configPath, "non_existent_app_12345"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("patch non existent app: %v", err)
	}
	if !strings.Contains(buf.String(), "No matching applications found") {
		t.Fatalf("expected 'No matching applications found', got %s", buf.String())
	}

	// 5. Unpatch dry run all
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"unpatch", "--config", configPath, "--dry-run"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unpatch dry-run failed: %v", err)
	}

	// 6. Unpatch with wildcard
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"unpatch", "--config", configPath, "--dry-run", "all"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unpatch all failed: %v", err)
	}

	// 7. Unpatch specific app
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"unpatch", "--config", configPath, "--dry-run", "rekordbox"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unpatch specific app failed: %v", err)
	}

	// 8. Unpatch non-existent app
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"unpatch", "--config", configPath, "non_existent_app_12345"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unpatch non-existent app: %v", err)
	}
	if !strings.Contains(buf.String(), "No matching applications found") {
		t.Fatalf("expected 'No matching applications found', got %s", buf.String())
	}
}

func TestPatchWithCustomAppPath(t *testing.T) {
	tempDir := t.TempDir()
	appPath := filepath.Join(tempDir, "FakeApp.app")
	_ = os.MkdirAll(appPath, 0o755)

	cmd := NewRootCommand("dev")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"patch", "--dry-run", "--driver", "native-hook", appPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("patch custom app path failed: %v", err)
	}

	if !strings.Contains(buf.String(), "FakeApp") {
		t.Fatalf("expected output to mention FakeApp: %s", buf.String())
	}

	// Unpatch custom app path
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"unpatch", "--dry-run", appPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unpatch custom app path failed: %v", err)
	}
	if !strings.Contains(buf.String(), "FakeApp") {
		t.Fatalf("expected output to mention FakeApp: %s", buf.String())
	}
}

func TestConfigSubcommands(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	configPath := filepath.Join(tempDir, "better-fonts", "config.toml")

	// 1. config path
	cmd := NewRootCommand("dev")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"config", "path"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute config path failed: %v", err)
	}
	if !strings.Contains(buf.String(), configPath) {
		t.Fatalf("config path output = %q, want it to contain %q", buf.String(), configPath)
	}

	// 2. config show (auto-creates default template)
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"config", "show"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute config show failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Maple Mono Normal NF") {
		t.Fatalf("config show missing font: %s", buf.String())
	}

	// 3. config init when file already exists
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"config", "init"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute config init existing failed: %v", err)
	}
	if !strings.Contains(buf.String(), "already exists") {
		t.Fatalf("expected 'already exists', got %s", buf.String())
	}

	// 4. config init on fresh path
	freshConfig := filepath.Join(tempDir, "fresh", "config.toml")
	cmd = NewRootCommand("dev")
	buf.Reset()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"config", "init", "--config", freshConfig})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute config init fresh failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Created default config file") {
		t.Fatalf("expected 'Created default config file', got %s", buf.String())
	}
}

func TestUpgradeCommand(t *testing.T) {
	cmd := NewRootCommand("1.0.0")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"upgrade"})

	if err := cmd.Execute(); err != nil {
		t.Logf("upgrade returned error (network dependent): %v", err)
	} else {
		if !strings.Contains(buf.String(), "up to date") && !strings.Contains(buf.String(), "upgraded") {
			t.Fatalf("unexpected upgrade output: %s", buf.String())
		}
	}
}

func TestDetectAppDriver(t *testing.T) {
	tempDir := t.TempDir()

	// Forced electron
	if d := detectAppDriver(tempDir, "electron"); d != "electron" {
		t.Fatalf("expected electron, got %s", d)
	}
	// Forced native-hook
	if d := detectAppDriver(tempDir, "native-hook"); d != "native-hook" {
		t.Fatalf("expected native-hook, got %s", d)
	}

	// App with asar in Contents/Resources
	resDir := filepath.Join(tempDir, "Contents", "Resources")
	_ = os.MkdirAll(resDir, 0o755)
	_ = os.WriteFile(filepath.Join(resDir, "app.asar"), []byte("fake asar"), 0o644)
	if d := detectAppDriver(tempDir, ""); d != "electron" {
		t.Fatalf("expected auto-detected electron, got %s", d)
	}

	// App without asar
	emptyDir := t.TempDir()
	if d := detectAppDriver(emptyDir, ""); d != "native-hook" {
		t.Fatalf("expected auto-detected native-hook, got %s", d)
	}
}
