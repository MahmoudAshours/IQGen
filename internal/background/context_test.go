package background

import (
	"strings"
	"testing"
)

func TestDetectTheme(t *testing.T) {
	if got := DetectTheme("هذا بحر واسع"); got == "" {
		t.Fatalf("expected theme for بحر")
	}
	if got := DetectTheme("no keyword"); got != "" {
		t.Fatalf("expected empty theme, got %q", got)
	}
}

func TestSanitizeQueryExcludesPeopleAndChurches(t *testing.T) {
	q := sanitizeQuery("church people portrait lake", true, true)
	if q == "" {
		t.Fatalf("expected query")
	}
	parts := strings.Fields(q)
	for i, part := range parts {
		if part == "people" || part == "church" {
			if i == 0 || parts[i-1] != "no" {
				t.Fatalf("expected banned token to be prefixed with no, got %q", q)
			}
		}
	}
}
