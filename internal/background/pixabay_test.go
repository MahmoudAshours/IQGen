package background

import "testing"

func TestPickPixabayFileSmallest(t *testing.T) {
	files := map[string]pixabayVideo{
		"small":  {URL: "s", Width: 640, Height: 360},
		"medium": {URL: "m", Width: 1280, Height: 720},
		"large":  {URL: "l", Width: 1920, Height: 1080},
	}
	got := pickPixabayFile(files, "smallest", 0, 0, 0)
	if got.URL != "s" {
		t.Fatalf("expected smallest file, got %q", got.URL)
	}
}

func TestPickPixabayFileQuality(t *testing.T) {
	files := map[string]pixabayVideo{
		"small":  {URL: "s", Width: 640, Height: 360},
		"medium": {URL: "m", Width: 1280, Height: 720},
		"large":  {URL: "l", Width: 1920, Height: 1080},
	}
	got := pickPixabayFile(files, "hd", 0, 0, 0)
	if got.URL == "" {
		t.Fatalf("expected a file for hd")
	}
}
