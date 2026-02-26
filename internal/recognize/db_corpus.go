package recognize

import (
	"context"
	"time"

	"qgencodex/internal/quran"
)

// DBCorpus reads ayahs from the local Quran SQLite DB.
type DBCorpus struct {
	DBPath string
}

func (c *DBCorpus) FetchSurah(ctx context.Context, surah int) ([]Ayah, error) {
	client := quran.NewDBClient(c.DBPath)
	ayahs, err := client.FetchSurahAyahs(ctx, surah)
	if err != nil {
		return nil, err
	}
	out := make([]Ayah, 0, len(ayahs))
	for _, a := range ayahs {
		out = append(out, Ayah{
			NumberInSurah: a.NumberInSurah,
			Text:          a.Text,
		})
	}
	return out, nil
}

// NewCorpusFromConfig returns the requested corpus source.
func NewCorpusFromConfig(baseURL, edition string, timeout time.Duration, useLocalDB bool, localDBPath string) QuranCorpus {
	if useLocalDB {
		return &DBCorpus{DBPath: localDBPath}
	}
	return &APICorpus{
		BaseURL: baseURL,
		Edition: edition,
		Timeout: timeout,
	}
}
