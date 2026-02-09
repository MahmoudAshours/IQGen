package background

import "strings"

// ThemeMapping maps keywords to Pexels queries.
var ThemeMapping = map[string]string{
	"جنة":   "sky clouds",
	"جنات":  "sky clouds",
	"بحر":   "ocean waves",
	"البحر": "ocean waves",
	"ليل":   "night sky stars",
	"الليل": "night sky stars",
	"صلاة":  "mosque peaceful",
	"صلاه":  "mosque peaceful",
	"جنةً":  "sky clouds",
}

// DetectTheme returns a theme query based on verse text.
func DetectTheme(text string) string {
	for keyword, theme := range ThemeMapping {
		if strings.Contains(text, keyword) {
			return theme
		}
	}
	return ""
}
