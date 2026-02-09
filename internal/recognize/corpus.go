package recognize

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type QuranCorpus interface {
	FetchSurah(ctx context.Context, surah int) ([]Ayah, error)
}

type Ayah struct {
	NumberInSurah int
	Text          string
}

type APICorpus struct {
	BaseURL string
	Edition string
	Timeout time.Duration
}

type apiResponse struct {
	Data apiSurah `json:"data"`
}

type apiSurah struct {
	Ayahs []apiAyah `json:"ayahs"`
}

type apiAyah struct {
	NumberInSurah int    `json:"numberInSurah"`
	Text          string `json:"text"`
}

func (c *APICorpus) FetchSurah(ctx context.Context, surah int) ([]Ayah, error) {
	endpoint := fmt.Sprintf("%s/surah/%d/%s", strings.TrimSuffix(c.BaseURL, "/"), surah, c.Edition)
	client := &http.Client{Timeout: c.Timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	var decoded apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	ayahs := make([]Ayah, 0, len(decoded.Data.Ayahs))
	for _, a := range decoded.Data.Ayahs {
		ayahs = append(ayahs, Ayah{NumberInSurah: a.NumberInSurah, Text: a.Text})
	}
	return ayahs, nil
}
