package sysutil

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	FuseSentinel                                 = "dL7pKGdnNz796PbbjQWNKmHXBZaB9tsX"
	FuseV1EnableEmbeddedAsarIntegrityValidation = 4
	FuseStateDisable                             = byte('0')
	FuseStateEnable                              = byte('1')
)

var (
	ErrSentinelNotFound = errors.New("electron fuse sentinel not found in binary")
	ErrBinaryNotFound   = errors.New("electron framework binary not found")
)

// FindElectronFrameworkBinary resolves the path to the Electron Framework Mach-O binary.
func FindElectronFrameworkBinary(appPath string) (string, error) {
	candidates := []string{
		filepath.Join(appPath, "Contents", "Frameworks", "Electron Framework.framework", "Electron Framework"),
		filepath.Join(appPath, "Contents", "Frameworks", "Electron Framework.framework", "Versions", "A", "Electron Framework"),
		filepath.Join(appPath, "Contents", "Frameworks", "Electron Framework.framework", "Versions", "Current", "Electron Framework"),
	}

	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}

	// Try finding any binary inside Frameworks matching Electron Framework
	frameworksDir := filepath.Join(appPath, "Contents", "Frameworks")
	if entries, err := os.ReadDir(frameworksDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() && filepath.Ext(entry.Name()) == ".framework" {
				candidate := filepath.Join(frameworksDir, entry.Name(), filepath.Base(entry.Name()[:len(entry.Name())-len(".framework")]))
				if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
					return candidate, nil
				}
			}
		}
	}

	return "", fmt.Errorf("%w in %s", ErrBinaryNotFound, appPath)
}

// IsAsarIntegrityDisabled checks if the EnableEmbeddedAsarIntegrityValidation fuse is disabled ('0').
func IsAsarIntegrityDisabled(appPath string) (bool, error) {
	binaryPath, err := FindElectronFrameworkBinary(appPath)
	if err != nil {
		return false, err
	}

	f, err := os.Open(binaryPath)
	if err != nil {
		return false, fmt.Errorf("opening binary %s: %w", binaryPath, err)
	}
	defer f.Close()

	offset, err := findSentinelOffset(f)
	if err != nil {
		return false, err
	}

	// Read wire header: 1 byte version, 1 byte wire length
	wireHeader := make([]byte, 2)
	if _, err := f.ReadAt(wireHeader, offset+int64(len(FuseSentinel))); err != nil {
		return false, fmt.Errorf("reading fuse header: %w", err)
	}

	wireLen := int(wireHeader[1])
	if wireLen <= FuseV1EnableEmbeddedAsarIntegrityValidation {
		return false, fmt.Errorf("fuse wire length %d too short", wireLen)
	}

	fuseByte := make([]byte, 1)
	fuseOffset := offset + int64(len(FuseSentinel)) + 2 + int64(FuseV1EnableEmbeddedAsarIntegrityValidation)
	if _, err := f.ReadAt(fuseByte, fuseOffset); err != nil {
		return false, fmt.Errorf("reading fuse byte: %w", err)
	}

	return fuseByte[0] == FuseStateDisable, nil
}

// DisableIntegrityValidationNative disables Electron's embedded ASAR integrity validation fuse in-place in pure Go.
func DisableIntegrityValidationNative(appPath string) error {
	binaryPath, err := FindElectronFrameworkBinary(appPath)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(binaryPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("opening binary for writing %s: %w", binaryPath, err)
	}
	defer f.Close()

	offset, err := findSentinelOffset(f)
	if err != nil {
		return err
	}

	wireHeader := make([]byte, 2)
	if _, err := f.ReadAt(wireHeader, offset+int64(len(FuseSentinel))); err != nil {
		return fmt.Errorf("reading fuse header: %w", err)
	}

	wireLen := int(wireHeader[1])
	if wireLen <= FuseV1EnableEmbeddedAsarIntegrityValidation {
		return fmt.Errorf("fuse wire length %d too short for integrity validation fuse", wireLen)
	}

	fuseOffset := offset + int64(len(FuseSentinel)) + 2 + int64(FuseV1EnableEmbeddedAsarIntegrityValidation)
	if _, err := f.WriteAt([]byte{FuseStateDisable}, fuseOffset); err != nil {
		return fmt.Errorf("writing disabled fuse byte: %w", err)
	}

	return nil
}

func findSentinelOffset(r io.ReaderAt) (int64, error) {
	sentinelBytes := []byte(FuseSentinel)
	const chunkSize = 64 * 1024
	buf := make([]byte, chunkSize+len(sentinelBytes))
	var offset int64

	for {
		n, err := r.ReadAt(buf, offset)
		if n == 0 {
			if err == io.EOF {
				break
			}
			return 0, err
		}

		if idx := bytes.Index(buf[:n], sentinelBytes); idx != -1 {
			return offset + int64(idx), nil
		}

		if n < len(buf) {
			break
		}

		// Advance by chunkSize to ensure overlapping boundary
		offset += chunkSize
	}

	return 0, ErrSentinelNotFound
}
