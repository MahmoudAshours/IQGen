package batch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	data := `jobs:
  - surah: 1
    start_ayah: 1
    end_ayah: 7
    mode: sequential
    output_name: fatiha.mp4
`
	path := filepath.Join(t.TempDir(), "batch.yaml")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	b, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if len(b.Jobs) != 1 {
		t.Fatalf("expected 1 job")
	}
	if b.Jobs[0].Surah != 1 || b.Jobs[0].EndAyah != 7 {
		t.Fatalf("unexpected job data")
	}
}
