package sysutil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner abstracts external command execution for easy testing and isolation.
type Runner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
}

type defaultRunner struct{}

func (d defaultRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v failed: %w (stderr: %s)", name, args, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (d defaultRunner) Output(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %v failed: %w (output: %s)", name, args, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

var DefaultRunner Runner = defaultRunner{}

// FindNodeOrBunRunner returns a command prefix (e.g. ["bun", "x"] or ["npx"]) for running npm tools.
func FindNodeOrBunRunner() ([]string, error) {
	if _, err := exec.LookPath("bun"); err == nil {
		return []string{"bun", "x"}, nil
	}
	if _, err := exec.LookPath("npx"); err == nil {
		return []string{"npx", "--yes"}, nil
	}
	return nil, errors.New("neither 'bun' nor 'npx' was found in PATH to execute @electron/fuses")
}

// DisableIntegrityValidation disables Electron's embedded asar integrity validation fuse on an app bundle.
func DisableIntegrityValidation(appPath string, r Runner) error {
	if err := DisableIntegrityValidationNative(appPath); err == nil {
		return nil
	}

	if prefix, err := FindNodeOrBunRunner(); err == nil {
		args := append(prefix, "@electron/fuses", "write", "--app", appPath, "EnableEmbeddedAsarIntegrityValidation=off")
		return r.Run(args[0], args[1:]...)
	}

	return fmt.Errorf("unable to disable integrity validation on %s", appPath)
}

// Codesign re-signs the macOS app bundle with ad-hoc signature.
func Codesign(appPath string, r Runner) error {
	if err := r.Run("codesign", "--force", "--deep", "--sign", "-", appPath); err != nil {
		return fmt.Errorf("codesign failed on %s: %w", appPath, err)
	}
	return nil
}

// QuitApp requests an app to quit gracefully via osascript.
func QuitApp(processName string, r Runner) error {
	script := fmt.Sprintf(`quit app "%s"`, processName)
	_ = r.Run("osascript", "-e", script)
	time.Sleep(500 * time.Millisecond)
	return nil
}

// StartApp launches an application via macOS open command.
func StartApp(processName string, r Runner) error {
	if err := r.Run("open", "-a", processName); err != nil {
		return fmt.Errorf("launching app %s: %w", processName, err)
	}
	return nil
}

// CopyDir recursively copies a directory tree from src to dst.
func CopyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat src %s: %w", src, err)
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("creating dst %s: %w", dst, err)
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("reading src dir %s: %w", src, err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(srcPath)
			if err != nil {
				return fmt.Errorf("reading symlink %s: %w", srcPath, err)
			}
			if err := os.Symlink(target, dstPath); err != nil {
				return fmt.Errorf("creating symlink %s: %w", dstPath, err)
			}
			continue
		}

		if entry.IsDir() {
			if err := CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := CopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// CopyFile copies a single file from src to dst preserving permissions.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening src file %s: %w", src, err)
	}
	defer in.Close()

	srcStat, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat src file %s: %w", src, err)
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcStat.Mode())
	if err != nil {
		return fmt.Errorf("creating dst file %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying %s to %s: %w", src, dst, err)
	}

	return nil
}
