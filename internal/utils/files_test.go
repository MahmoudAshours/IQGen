package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDirAndFileExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	if err := EnsureDir(dir); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	path := filepath.Join(dir, "file.txt")
	if FileExists(path) {
		t.Fatalf("expected file to not exist yet")
	}
	if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	if !FileExists(path) {
		t.Fatalf("expected file to exist")
	}
}
