package utils

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

type testResp struct {
	Name string `json:"name"`
}

func TestGetJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(testResp{Name: "ok"})
	}))
	defer server.Close()

	var resp testResp
	err := GetJSON(context.Background(), HTTPClient(2*time.Second), server.URL, nil, &resp)
	if err != nil {
		t.Fatalf("GetJSON failed: %v", err)
	}
	if resp.Name != "ok" {
		t.Fatalf("unexpected resp: %v", resp.Name)
	}
}

func TestDownloadFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "file.txt")
	err := DownloadFile(context.Background(), HTTPClient(2*time.Second), server.URL, nil, path)
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("unexpected file contents")
	}
}
