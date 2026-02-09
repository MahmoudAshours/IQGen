package audio

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConcatListContent(t *testing.T) {
	segments := []Segment{
		{Path: filepath.Join("tmp", "a'b.mp3")},
	}
	content, err := concatListContent(segments)
	if err != nil {
		t.Fatalf("concatListContent failed: %v", err)
	}
	if !strings.Contains(content, "file '") {
		t.Fatalf("expected file line")
	}
	if !strings.Contains(content, "a'\\''b.mp3") {
		t.Fatalf("expected escaped apostrophe, got %s", content)
	}
	if !filepath.IsAbs(strings.TrimSpace(strings.TrimPrefix(content, "file '"))) {
		// best-effort: ensure absolute path appears in content
		if !strings.HasPrefix(strings.TrimSpace(content), "file '/") {
			t.Fatalf("expected absolute path in content")
		}
	}
}
