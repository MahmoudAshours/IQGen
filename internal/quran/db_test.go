package quran

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDBClientFetchVerses_LocalFile(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 is required for local DB tests")
	}

	dbPath := filepath.Join(t.TempDir(), "quran.db")
	schema := `
CREATE TABLE TB_Surah (ID INTEGER, name TEXT, englishName TEXT, englishNameTranslation TEXT, revelationType TEXT);
CREATE TABLE TB_Quran (TranslationID INTEGER, SuraID INTEGER, AyahID INTEGER, AyahText TEXT);
INSERT INTO TB_Surah VALUES (1, 'Al-Fatiha', 'The Opening', 'The Opening', 'Meccan');
INSERT INTO TB_Quran VALUES (1, 1, 1, 'Arabic verse one');
INSERT INTO TB_Quran VALUES (1, 1, 2, 'Arabic verse two');
INSERT INTO TB_Quran VALUES (17, 1, 1, 'English verse one');
INSERT INTO TB_Quran VALUES (17, 1, 2, 'English verse two');
`
	if out, err := exec.Command("sqlite3", dbPath, schema).CombinedOutput(); err != nil {
		t.Fatalf("create test database: %v: %s", err, out)
	}

	client := NewDBClient(dbPath)
	verses, err := client.FetchVerses(context.Background(), 1, 1, 2, "quran-uthmani", "en.sahih")
	if err != nil {
		t.Fatalf("fetch local verses: %v", err)
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
