package background

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSelectAndDownload(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search":
			resp := videoSearchResponse{
				Videos: []pexelsVideo{
					{
						ID:       1,
						Width:    1080,
						Height:   1920,
						Duration: 20,
						VideoFiles: []pexelsVideoFile{
							{ID: 1, Quality: "sd", FileType: "video/mp4", Width: 640, Height: 360, Link: server.URL + "/video.mp4"},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case "/video.mp4":
			_, _ = w.Write([]byte("fake-data"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := &PexelsClient{BaseURL: server.URL + "/search", APIKey: "KEY", Timeout: 2 * time.Second}
	selector := Selector{
		Client:        client,
		FallbackQuery: "nature",
		Orientation:   "portrait",
		MinDuration:   5,
		Timeout:       2 * time.Second,
		Quality:       "sd",
		Random:        false,
		ExcludePeople: true,
	}
	dest := filepath.Join(t.TempDir(), "bg.mp4")
	_, err := selector.SelectAndDownload(context.Background(), "any text", dest)
	if err != nil {
		t.Fatalf("SelectAndDownload failed: %v", err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("expected downloaded file: %v", err)
	}
}
