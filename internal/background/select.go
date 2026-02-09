package background

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"qgencodex/internal/ai"
	"qgencodex/internal/retry"
	"qgencodex/internal/utils"
)

type VideoClient interface {
	Search(ctx context.Context, opts SearchOptions) ([]Selection, error)
}

type Selector struct {
	Client           VideoClient
	FallbackQuery    string
	Orientation      string
	MinDuration      int
	Timeout          time.Duration
	Quality          string
	MaxWidth         int
	MaxHeight        int
	MaxPixels        int
	UseContext       bool
	Random           bool
	Rand             *rand.Rand
	UseAI            bool
	AIClient         *ai.Client
	AISelect         bool
	ExcludePeople    bool
	ExcludeReligious bool
}

// SelectAndDownload chooses a background video based on verse text and downloads it.
func (s *Selector) SelectAndDownload(ctx context.Context, verseText string, destPath string) (Selection, error) {
	selections, query, err := s.searchForText(ctx, verseText)
	if err != nil {
		return Selection{}, err
	}
	if len(selections) == 0 {
		return Selection{}, fmt.Errorf("no background videos found for query %q", query)
	}
	chosen := s.chooseSelection(ctx, selections, query)
	chosen.Query = query
	client := utils.HTTPClient(s.Timeout)
	err = retry.Do(ctx, 3, 500*time.Millisecond, func() error {
		return utils.DownloadFile(ctx, client, chosen.VideoURL, nil, destPath)
	})
	if err != nil {
		return chosen, err
	}
	return chosen, nil
}

func (s *Selector) SelectFromPool(ctx context.Context, texts []string, destPath string) (Selection, error) {
	if len(texts) == 0 {
		return Selection{}, fmt.Errorf("no texts for selection")
	}
	seen := map[string]Selection{}
	var queries []string
	for _, text := range texts {
		results, query, err := s.searchForText(ctx, text)
		if err != nil {
			continue
		}
		queries = append(queries, query)
		for _, sel := range results {
			seen[sel.VideoURL] = sel
		}
	}
	if len(seen) == 0 {
		return Selection{}, fmt.Errorf("no background videos found for pooled queries")
	}
	var selections []Selection
	for _, sel := range seen {
		selections = append(selections, sel)
	}
	chosen := s.chooseSelection(ctx, selections, strings.Join(queries, " | "))
	chosen.Query = strings.Join(queries, " | ")
	client := utils.HTTPClient(s.Timeout)
	err := retry.Do(ctx, 3, 500*time.Millisecond, func() error {
		return utils.DownloadFile(ctx, client, chosen.VideoURL, nil, destPath)
	})
	if err != nil {
		return chosen, err
	}
	return chosen, nil
}

func (s *Selector) searchForText(ctx context.Context, verseText string) ([]Selection, string, error) {
	query := ""
	if s.UseAI && s.AIClient != nil && s.AIClient.Available() {
		if aiQuery, err := s.AIClient.QueryKeywords(ctx, verseText); err == nil && aiQuery != "" {
			query = aiQuery
		}
	}
	if query == "" && s.UseContext {
		query = DetectTheme(verseText)
	}
	if query == "" {
		query = s.FallbackQuery
	}
	if strings.TrimSpace(query) == "" {
		query = "nature"
	}
	query = sanitizeQuery(query, s.ExcludePeople, s.ExcludeReligious)
	selections, err := s.Client.Search(ctx, SearchOptions{
		Query:       query,
		Orientation: s.Orientation,
		MinDuration: s.MinDuration,
		Quality:     s.Quality,
		MaxWidth:    s.MaxWidth,
		MaxHeight:   s.MaxHeight,
		MaxPixels:   s.MaxPixels,
	})
	if err != nil {
		return nil, query, err
	}
	if len(selections) < 2 {
		fallback := sanitizeQuery(s.FallbackQuery, s.ExcludePeople, s.ExcludeReligious)
		if fallback == "" {
			fallback = "nature landscape no people"
		}
		more, err := s.Client.Search(ctx, SearchOptions{
			Query:       fallback,
			Orientation: s.Orientation,
			MinDuration: s.MinDuration,
			Quality:     s.Quality,
			MaxWidth:    s.MaxWidth,
			MaxHeight:   s.MaxHeight,
			MaxPixels:   s.MaxPixels,
		})
		if err == nil && len(more) > 0 {
			selections = append(selections, more...)
		}
	}
	return selections, query, nil
}

func sanitizeQuery(query string, excludePeople bool, excludeReligious bool) string {
	q := strings.ToLower(strings.TrimSpace(query))
	if excludePeople {
		// Remove explicit people-related tokens if present.
		banned := []string{
			"people", "person", "man", "woman", "women", "men", "girl", "boy", "child", "portrait", "face", "selfie", "model",
		}
		for _, token := range banned {
			q = strings.ReplaceAll(q, token, "")
		}
	}
	if excludeReligious {
		bannedRel := []string{
			"church", "churches", "cathedral", "cross", "christian", "jesus", "christ",
		}
		for _, token := range bannedRel {
			q = strings.ReplaceAll(q, token, "")
		}
	}
	q = strings.Join(strings.Fields(q), " ")
	// Add strong negative intent to nudge search away from people.
	if q == "" {
		q = "nature landscape"
	}
	if !strings.Contains(q, "landscape") {
		q = q + " landscape"
	}
	if !strings.Contains(q, "nature") {
		q = q + " nature"
	}
	if excludePeople && !strings.Contains(q, "no people") {
		q = q + " no people"
	}
	if excludeReligious && !strings.Contains(q, "no church") {
		// q = q + " no church"
	}
	return q
}

func (s *Selector) chooseSelection(ctx context.Context, selections []Selection, query string) Selection {
	if len(selections) == 0 {
		return Selection{}
	}
	if s.AISelect && s.AIClient != nil && s.AIClient.Available() && len(selections) > 1 {
		prompt := buildSelectionPrompt(query, selections, s.ExcludePeople, s.ExcludeReligious)
		if idx, err := s.AIClient.ChooseIndex(ctx, prompt, len(selections)); err == nil && idx >= 0 && idx < len(selections) {
			return selections[idx]
		}
	}
	if s.Random && len(selections) > 1 {
		rng := s.Rand
		if rng == nil {
			rng = rand.New(rand.NewSource(time.Now().UnixNano()))
		}
		return selections[rng.Intn(len(selections))]
	}
	return selections[0]
}

func buildSelectionPrompt(query string, selections []Selection, excludePeople bool, excludeReligious bool) string {
	var b strings.Builder
	b.WriteString("You are selecting a background video for a Quran ayah. Choose the best index. Return only the index number.\n")
	b.WriteString("Query: ")
	b.WriteString(query)
	b.WriteString("\n")
	if excludePeople {
		b.WriteString("Avoid people/faces/portraits.\n")
	}
	if excludeReligious {
		b.WriteString("Avoid churches, crosses, and religious buildings.\n")
	}
	for i, s := range selections {
		fmt.Fprintf(&b, "%d) duration=%ds size=%dx%d\n", i, s.Duration, s.Width, s.Height)
	}
	b.WriteString("Return only the index.")
	return b.String()
}
