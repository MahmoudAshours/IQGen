package quran

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"qgencodex/internal/retry"
	"qgencodex/internal/utils"
)

type Client struct {
	BaseURL string
	Timeout time.Duration
}

type Surah struct {
	Number                 int    `json:"number"`
	Name                   string `json:"name"`
	EnglishName            string `json:"englishName"`
	EnglishNameTranslation string `json:"englishNameTranslation"`
	RevelationType         string `json:"revelationType"`
	Ayahs                  []Ayah `json:"ayahs"`
}

type Ayah struct {
	Number        int    `json:"number"`
	Text          string `json:"text"`
	NumberInSurah int    `json:"numberInSurah"`
	Juz           int    `json:"juz"`
	Manzil        int    `json:"manzil"`
	Ruku          int    `json:"ruku"`
	HizbQuarter   int    `json:"hizbQuarter"`
}

type surahResponse struct {
	Data Surah `json:"data"`
}

type Verse struct {
	Number        int
	NumberInSurah int
	Text          string
	Translation   string
	SurahMeta     SurahMeta
}

type SurahMeta struct {
	Number                 int
	Name                   string
	EnglishName            string
	EnglishNameTranslation string
	RevelationType         string
}

func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{BaseURL: baseURL, Timeout: timeout}
}

func (c *Client) FetchSurah(ctx context.Context, surahNumber int, edition string) (Surah, error) {
	client := utils.HTTPClient(c.Timeout)
	endpoint := fmt.Sprintf("%s/surah/%d/%s", c.BaseURL, surahNumber, url.PathEscape(edition))
	var resp surahResponse
	err := retry.Do(ctx, 3, 300*time.Millisecond, func() error {
		return utils.GetJSON(ctx, client, endpoint, nil, &resp)
	})
	if err != nil {
		return Surah{}, err
	}
	return resp.Data, nil
}

func (c *Client) FetchVerses(ctx context.Context, surahNumber, startAyah, endAyah int, edition string, translationEdition string) ([]Verse, error) {
	arabicSurah, err := c.FetchSurah(ctx, surahNumber, edition)
	if err != nil {
		return nil, err
	}
	translationMap := map[int]string{}
	if translationEdition != "" {
		transSurah, err := c.FetchSurah(ctx, surahNumber, translationEdition)
		if err != nil {
			return nil, err
		}
		for _, ayah := range transSurah.Ayahs {
			translationMap[ayah.NumberInSurah] = ayah.Text
		}
	}
	meta := SurahMeta{
		Number:                 arabicSurah.Number,
		Name:                   arabicSurah.Name,
		EnglishName:            arabicSurah.EnglishName,
		EnglishNameTranslation: arabicSurah.EnglishNameTranslation,
		RevelationType:         arabicSurah.RevelationType,
	}
	var verses []Verse
	for _, ayah := range arabicSurah.Ayahs {
		if ayah.NumberInSurah < startAyah || ayah.NumberInSurah > endAyah {
			continue
		}
		verses = append(verses, Verse{
			Number:        ayah.Number,
			NumberInSurah: ayah.NumberInSurah,
			Text:          ayah.Text,
			Translation:   translationMap[ayah.NumberInSurah],
			SurahMeta:     meta,
		})
	}
	if len(verses) == 0 {
		return nil, fmt.Errorf("no verses found for surah %d range %d-%d", surahNumber, startAyah, endAyah)
	}
	return verses, nil
}
