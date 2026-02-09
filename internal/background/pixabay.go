package background

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"qgencodex/internal/retry"
	"qgencodex/internal/utils"
)

type PixabayClient struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

type pixabayResponse struct {
	Hits []pixabayHit `json:"hits"`
}

type pixabayHit struct {
	ID       int                     `json:"id"`
	Duration int                     `json:"duration"`
	Videos   map[string]pixabayVideo `json:"videos"`
}

type pixabayVideo struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int    `json:"size"`
}

func (c *PixabayClient) Search(ctx context.Context, opts SearchOptions) ([]Selection, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("pixabay api key is missing")
	}
	client := utils.HTTPClient(c.Timeout)
	endpoint := strings.TrimSpace(c.BaseURL)
	if endpoint == "" {
		endpoint = "https://pixabay.com/api/videos/"
	}
	q := url.Values{}
	q.Set("key", c.APIKey)
	q.Set("q", opts.Query)
	q.Set("per_page", "10")
	q.Set("safesearch", "true")
	queryURL := endpoint
	if !strings.Contains(queryURL, "?") {
		queryURL = queryURL + "?" + q.Encode()
	} else {
		queryURL = queryURL + "&" + q.Encode()
	}
	var resp pixabayResponse
	err := retry.Do(ctx, 3, 400*time.Millisecond, func() error {
		return utils.GetJSON(ctx, client, queryURL, nil, &resp)
	})
	if err != nil {
		return nil, err
	}
	var selections []Selection
	for _, hit := range resp.Hits {
		if opts.MinDuration > 0 && hit.Duration < opts.MinDuration {
			continue
		}
		best := pickPixabayFile(hit.Videos, opts.Quality, opts.MaxWidth, opts.MaxHeight, opts.MaxPixels)
		if best.URL == "" {
			continue
		}
		selections = append(selections, Selection{
			VideoURL: best.URL,
			Duration: hit.Duration,
			Width:    best.Width,
			Height:   best.Height,
		})
	}
	return selections, nil
}

type pixabayCandidate struct {
	Quality string
	File    pixabayVideo
}

func pickPixabayFile(files map[string]pixabayVideo, quality string, maxWidth, maxHeight, maxPixels int) pixabayVideo {
	if len(files) == 0 {
		return pixabayVideo{}
	}
	candidates := make([]pixabayCandidate, 0, len(files))
	for q, f := range files {
		candidates = append(candidates, pixabayCandidate{Quality: strings.ToLower(q), File: f})
	}
	filtered := filterPixabayBySize(candidates, maxWidth, maxHeight, maxPixels)
	if len(filtered) == 0 {
		filtered = candidates
	}
	q := strings.ToLower(strings.TrimSpace(quality))
	switch q {
	case "sd":
		if file, ok := pickByQuality(filtered, []string{"small", "medium"}); ok {
			return file
		}
	case "hd", "best":
		if file, ok := pickByQuality(filtered, []string{"large", "medium"}); ok {
			return file
		}
	case "smallest":
		sort.Slice(filtered, func(i, j int) bool {
			return (filtered[i].File.Width * filtered[i].File.Height) < (filtered[j].File.Width * filtered[j].File.Height)
		})
		return filtered[0].File
	}
	sort.Slice(filtered, func(i, j int) bool {
		return (filtered[i].File.Width * filtered[i].File.Height) > (filtered[j].File.Width * filtered[j].File.Height)
	})
	return filtered[0].File
}

func pickByQuality(candidates []pixabayCandidate, qualities []string) (pixabayVideo, bool) {
	for _, q := range qualities {
		for _, c := range candidates {
			if c.Quality == q {
				return c.File, true
			}
		}
	}
	return pixabayVideo{}, false
}

func filterPixabayBySize(candidates []pixabayCandidate, maxWidth, maxHeight, maxPixels int) []pixabayCandidate {
	if maxWidth <= 0 && maxHeight <= 0 && maxPixels <= 0 {
		return candidates
	}
	out := make([]pixabayCandidate, 0, len(candidates))
	for _, c := range candidates {
		if maxWidth > 0 && c.File.Width > maxWidth {
			continue
		}
		if maxHeight > 0 && c.File.Height > maxHeight {
			continue
		}
		if maxPixels > 0 && c.File.Width*c.File.Height > maxPixels {
			continue
		}
		out = append(out, c)
	}
	return out
}
