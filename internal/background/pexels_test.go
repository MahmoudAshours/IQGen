package background

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPexelsSearchSelectsBestFile(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "KEY" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		resp := videoSearchResponse{
			Videos: []pexelsVideo{
				{
					ID:       1,
					Width:    1920,
					Height:   1080,
					Duration: 15,
					VideoFiles: []pexelsVideoFile{
						{ID: 1, Quality: "sd", FileType: "video/mp4", Width: 640, Height: 360, Link: server.URL + "/sd.mp4"},
						{ID: 2, Quality: "hd", FileType: "video/mp4", Width: 1920, Height: 1080, Link: server.URL + "/hd.mp4"},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &PexelsClient{BaseURL: server.URL, APIKey: "KEY", Timeout: 2 * time.Second}
	results, err := client.Search(context.Background(), SearchOptions{
		Query:       "nature",
		Orientation: "portrait",
		MinDuration: 0,
		Quality:     "sd",
	})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].VideoURL != server.URL+"/sd.mp4" {
		t.Fatalf("expected best file to be sd, got %s", results[0].VideoURL)
	}
}
