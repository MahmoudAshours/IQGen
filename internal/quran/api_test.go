package quran

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchVerses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/surah/1/") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.HasSuffix(r.URL.Path, "quran-uthmani") {
			_ = json.NewEncoder(w).Encode(surahResponse{Data: Surah{
				Number:                 1,
				Name:                   "الفاتحة",
				EnglishName:            "Al-Faatiha",
				EnglishNameTranslation: "The Opening",
				RevelationType:         "Meccan",
				Ayahs: []Ayah{
					{Number: 1, Text: "بِسْمِ", NumberInSurah: 1},
					{Number: 2, Text: "الْحَمْدُ", NumberInSurah: 2},
				},
			}})
			return
		}
		if strings.HasSuffix(r.URL.Path, "en.sahih") {
			_ = json.NewEncoder(w).Encode(surahResponse{Data: Surah{
				Ayahs: []Ayah{
					{Number: 1, Text: "In the name", NumberInSurah: 1},
					{Number: 2, Text: "All praise", NumberInSurah: 2},
				},
			}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL, 2*time.Second)
	verses, err := client.FetchVerses(context.Background(), 1, 1, 2, "quran-uthmani", "en.sahih")
	if err != nil {
		t.Fatalf("FetchVerses failed: %v", err)
	}
	if len(verses) != 2 {
		t.Fatalf("expected 2 verses, got %d", len(verses))
	}
	if verses[0].Translation == "" || verses[1].Translation == "" {
		t.Fatalf("expected translations")
	}
	if verses[0].SurahMeta.EnglishName != "Al-Faatiha" {
		t.Fatalf("unexpected surah meta")
	}
}
