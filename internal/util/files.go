package util

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func EnsureDir(dir string) error {
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	return nil
}

// WriteFile writes data to the given path atomically.
// To avoid unnecessary churn and ping-pong syncs, it becomes a no-op if the
// file already exists with identical content. It writes to a temp file in the
// same directory, fsyncs it, then renames over the destination and fsyncs the
// directory to ensure durability.
func WriteFile(path string, data []byte) error {
	// If file exists and content is identical, skip writing
	if b, err := os.ReadFile(path); err == nil {
		if bytes.Equal(b, data) {
			return nil
		}
	}

	dir := filepath.Dir(path)
	// Ensure directory exists
	if err := EnsureDir(dir); err != nil {
		return err
	}

	// Create temp file in the same directory for atomic rename
	tmp, err := os.CreateTemp(dir, ".syncai-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()

	// Ensure cleanup on error
	defer func() {
		_ = os.Remove(tmpName)
	}()

	// Write data and fsync temp file
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	// Ensure consistent file permissions
	_ = os.Chmod(tmpName, 0o644)

	// Rename atomically over the destination
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	// Fsync the directory to persist the rename on crash-prone filesystems
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum), nil
}

// PruneEmptyDirs removes startDir and each ancestor up to (but not including)
// stopDir as long as the directory is empty. It is a no-op if startDir does
// not exist, is not empty, or escapes stopDir. Errors other than not-exist
// are ignored — best-effort cleanup.
func PruneEmptyDirs(startDir, stopDir string) {
	if startDir == "" {
		return
	}
	cur := filepath.Clean(startDir)
	stop := filepath.Clean(stopDir)
	for cur != "" && cur != "." && cur != "/" && cur != stop {
		entries, err := os.ReadDir(cur)
		if err != nil {
			return
		}
		if len(entries) > 0 {
			return
		}
		if err := os.Remove(cur); err != nil {
			return
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return
		}
		cur = parent
	}
}

func IsFileExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false // file does not exist or error occurred
	}
	return !info.IsDir() // ensure it's a file, not a directory
}
