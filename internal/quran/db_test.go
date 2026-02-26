package quran

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDBClientFetchVerses_LocalFile(t *testing.T) {
	dbPath := filepath.Join("..", "..", "The_Holy_Quran.db")
	client := NewDBClient(dbPath)
	verses, err := client.FetchVerses(context.Background(), 1, 1, 2, "quran-uthmani", "en.sahih")
	if err != nil {
		t.Skipf("skipping local DB test: %v", err)
	}
	if len(verses) != 2 {
		t.Fatalf("expected 2 verses, got %d", len(verses))
	}
	if verses[0].Text == "" {
		t.Fatalf("expected arabic verse text")
	}
	if verses[0].Translation == "" {
		t.Fatalf("expected translation text")
	}
	if verses[0].SurahMeta.Number != 1 {
		t.Fatalf("expected surah meta number 1, got %d", verses[0].SurahMeta.Number)
	}
}

func TestSurahAyahToGlobal(t *testing.T) {
	if got := surahAyahToGlobal(1, 1); got != 1 {
		t.Fatalf("expected 1, got %d", got)
	}
	// Surah 2 ayah 10 => 7 (surah 1) + 10 = 17
	if got := surahAyahToGlobal(2, 10); got != 17 {
		t.Fatalf("expected 17, got %d", got)
	}
}
