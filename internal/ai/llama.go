package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Model   string
	Timeout time.Duration
}

type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

type generateResponse struct {
	Response string `json:"response"`
}

func (c *Client) Available() bool {
	return c != nil && c.BaseURL != "" && c.Model != ""
}

func (c *Client) QueryKeywords(ctx context.Context, text string) (string, error) {
	prompt := fmt.Sprintf("Return 1 to 3 short English keywords for a background video that matches this Quran ayah. Only output comma-separated keywords, no extra text. Ayah: %s", text)
	result, err := c.generate(ctx, prompt)
	if err != nil {
		return "", err
	}
	return result, nil
}

func (c *Client) ChooseIndex(ctx context.Context, prompt string, max int) (int, error) {
	if max <= 0 {
		return -1, fmt.Errorf("invalid max index")
	}
	result, err := c.generate(ctx, prompt)
	if err != nil {
		return -1, err
	}
	fields := strings.FieldsFunc(result, func(r rune) bool {
		return r < '0' || r > '9'
	})
	if len(fields) == 0 {
		return -1, fmt.Errorf("ai returned no index")
	}
	var idx int
	if _, err := fmt.Sscanf(fields[0], "%d", &idx); err != nil {
		return -1, fmt.Errorf("ai returned invalid index: %s", result)
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= max {
		idx = max - 1
	}
	return idx, nil
}

func (c *Client) generate(ctx context.Context, prompt string) (string, error) {
	if !c.Available() {
		return "", fmt.Errorf("ai client not configured")
	}
	payload, err := json.Marshal(generateRequest{
		Model:  c.Model,
		Prompt: prompt,
		Stream: false,
	})
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: c.Timeout}
	endpoint := strings.TrimSuffix(c.BaseURL, "/") + "/api/generate"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ai http %d", resp.StatusCode)
	}
	var out generateResponse
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return "", err
	}
	result := strings.TrimSpace(out.Response)
	result = strings.Trim(result, "\"'")
	result = strings.Split(result, "\n")[0]
	return strings.TrimSpace(result), nil
}
