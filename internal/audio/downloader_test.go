package audio

import "testing"

func TestGlobalAyahToSurahAyah(t *testing.T) {
	tests := []struct {
		global int
		surah  int
		ayah   int
		ok     bool
	}{
		{global: 1, surah: 1, ayah: 1, ok: true},
		{global: 7, surah: 1, ayah: 7, ok: true},
		{global: 8, surah: 2, ayah: 1, ok: true},
		{global: 220, surah: 2, ayah: 213, ok: true},
		{global: 6236, surah: 114, ayah: 6, ok: true},
		{global: 0, ok: false},
		{global: 6237, ok: false},
	}
	for _, tc := range tests {
		surah, ayah, ok := globalAyahToSurahAyah(tc.global)
		if ok != tc.ok {
			t.Fatalf("global=%d: got ok=%v want %v", tc.global, ok, tc.ok)
		}
		if !ok {
			continue
		}
		if surah != tc.surah || ayah != tc.ayah {
			t.Fatalf("global=%d: got %d:%d want %d:%d", tc.global, surah, ayah, tc.surah, tc.ayah)
		}
	}
}

func TestEveryAyahURLForReciter(t *testing.T) {
	got := everyAyahURLForReciter("ar.alafasy", 2, 1)
	want := "https://everyayah.com/data/Alafasy_128kbps/002001.mp3"
	if got != want {
		t.Fatalf("unexpected URL: got %q want %q", got, want)
	}
	got = everyAyahURLForReciter("ar.yasseraldosari", 20, 43)
	want = "https://everyayah.com/data/Yasser_Ad-Dussary_128kbps/020043.mp3"
	if got != want {
		t.Fatalf("unexpected Yasser Al-Dosari URL: got %q want %q", got, want)
	}
	if everyAyahURLForReciter("ar.unknown", 2, 1) != "" {
		t.Fatalf("unknown reciter should return empty URL")
	}
}

func TestPickPreferredQuranAPIAudio(t *testing.T) {
	entries := map[string]quranAPIAudioEntry{
		"1": {Reciter: "Mishary Rashid Al Afasy", URL: "https://example.com/a.mp3", OriginalURL: "https://example.com/oa.mp3"},
		"2": {Reciter: "Abu Bakr Al Shatri", URL: "https://example.com/s.mp3", OriginalURL: "https://example.com/os.mp3"},
	}

	picked, ok := pickPreferredQuranAPIAudio(entries, "ar.shaatree")
	if !ok {
		t.Fatalf("expected a picked entry")
	}
	if picked.Reciter != "Abu Bakr Al Shatri" {
		t.Fatalf("picked wrong reciter: %q", picked.Reciter)
	}

	picked, ok = pickPreferredQuranAPIAudio(entries, "ar.someunknown")
	if !ok {
		t.Fatalf("expected fallback entry")
	}
	if picked.Reciter != "Mishary Rashid Al Afasy" {
		t.Fatalf("expected key 1 fallback, got %q", picked.Reciter)
	}
}
