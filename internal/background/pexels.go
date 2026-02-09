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

type PexelsClient struct {
	BaseURL string
	APIKey  string
	Timeout time.Duration
}

type videoSearchResponse struct {
	Videos []pexelsVideo `json:"videos"`
}

type pexelsVideo struct {
	ID         int               `json:"id"`
	Width      int               `json:"width"`
	Height     int               `json:"height"`
	Duration   int               `json:"duration"`
	VideoFiles []pexelsVideoFile `json:"video_files"`
}

type pexelsVideoFile struct {
	ID       int    `json:"id"`
	Quality  string `json:"quality"`
	FileType string `json:"file_type"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Link     string `json:"link"`
}

type Selection struct {
	VideoURL string
	Duration int
	Width    int
	Height   int
	Query    string
}

type SearchOptions struct {
	Query       string
	Orientation string
	MinDuration int
	Quality     string
	MaxWidth    int
	MaxHeight   int
	MaxPixels   int
}

func (c *PexelsClient) Search(ctx context.Context, opts SearchOptions) ([]Selection, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("pexels api key is missing")
	}
	client := utils.HTTPClient(c.Timeout)
	q := url.Values{}
	q.Set("query", opts.Query)
	if opts.Orientation != "" {
		q.Set("orientation", opts.Orientation)
	}
	q.Set("per_page", "10")
	endpoint := c.BaseURL
	if !strings.Contains(endpoint, "?") {
		endpoint = endpoint + "?" + q.Encode()
	} else {
		endpoint = endpoint + "&" + q.Encode()
	}
	var resp videoSearchResponse
	err := retry.Do(ctx, 3, 400*time.Millisecond, func() error {
		return utils.GetJSON(ctx, client, endpoint, map[string]string{"Authorization": c.APIKey}, &resp)
	})
	if err != nil {
		return nil, err
	}
	var selections []Selection
	for _, video := range resp.Videos {
		if opts.MinDuration > 0 && video.Duration < opts.MinDuration {
			continue
		}
		best := pickBestFile(video.VideoFiles, opts.Quality, opts.MaxWidth, opts.MaxHeight, opts.MaxPixels)
		if best.Link == "" {
			continue
		}
		selections = append(selections, Selection{
			VideoURL: best.Link,
			Duration: video.Duration,
			Width:    best.Width,
			Height:   best.Height,
		})
	}
	return selections, nil
}

func pickBestFile(files []pexelsVideoFile, quality string, maxWidth, maxHeight, maxPixels int) pexelsVideoFile {
	if len(files) == 0 {
		return pexelsVideoFile{}
	}
	filtered := filterBySize(files, maxWidth, maxHeight, maxPixels)
	if len(filtered) == 0 {
		filtered = files
	}
	q := strings.ToLower(strings.TrimSpace(quality))
	switch q {
	case "sd", "hd":
		matches := filterByQuality(filtered, q)
		if len(matches) > 0 {
			filtered = matches
		}
	case "smallest":
		sort.Slice(filtered, func(i, j int) bool {
			return (filtered[i].Width * filtered[i].Height) < (filtered[j].Width * filtered[j].Height)
		})
		return filtered[0]
	}
	sort.Slice(filtered, func(i, j int) bool {
		return (filtered[i].Width * filtered[i].Height) > (filtered[j].Width * filtered[j].Height)
	})
	return filtered[0]
}

func filterByQuality(files []pexelsVideoFile, quality string) []pexelsVideoFile {
	var out []pexelsVideoFile
	for _, f := range files {
		if strings.EqualFold(f.Quality, quality) {
			out = append(out, f)
		}
	}
	return out
}

func filterBySize(files []pexelsVideoFile, maxWidth, maxHeight, maxPixels int) []pexelsVideoFile {
	if maxWidth <= 0 && maxHeight <= 0 && maxPixels <= 0 {
		return files
	}
	var out []pexelsVideoFile
	for _, f := range files {
		if maxWidth > 0 && f.Width > maxWidth {
			continue
		}
		if maxHeight > 0 && f.Height > maxHeight {
			continue
		}
		if maxPixels > 0 && f.Width*f.Height > maxPixels {
			continue
		}
		out = append(out, f)
	}
	return out
}
