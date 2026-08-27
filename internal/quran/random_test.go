package quran

import "testing"

func TestRandomAyahReference(t *testing.T) {
	for i := 0; i < 100; i++ {
		surah, ayah, err := RandomAyahReference()
		if err != nil {
			t.Fatalf("select random ayah: %v", err)
		}
		if surah < 1 || surah > len(surahAyahCounts) {
			t.Fatalf("surah out of range: %d", surah)
		}
		if ayah < 1 || ayah > surahAyahCounts[surah-1] {
			t.Fatalf("ayah out of range: %d:%d", surah, ayah)
		}
	}
}

func TestRandomAyahRange(t *testing.T) {
	for i := 0; i < 100; i++ {
		surah, start, end, err := RandomAyahRange(3)
		if err != nil {
			t.Fatalf("select random ayah range: %v", err)
		}
		if surah < 1 || surah > len(surahAyahCounts) {
			t.Fatalf("surah out of range: %d", surah)
		}
		if start < 1 || end-start+1 != 3 || end > surahAyahCounts[surah-1] {
			t.Fatalf("invalid ayah range: %d:%d-%d", surah, start, end)
		}
	}
}

func TestRandomAyahRangeRejectsInvalidCount(t *testing.T) {
	if _, _, _, err := RandomAyahRange(0); err == nil {
		t.Fatal("expected an error for zero ayahs")
	}
	if _, _, _, err := RandomAyahRange(287); err == nil {
		t.Fatal("expected an error for too many ayahs")
	}
}
