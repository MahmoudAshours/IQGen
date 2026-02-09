package render

import (
	"os"
	"path/filepath"
	"strings"
)

var fontCandidates = []string{
	"Amiri Quran",
	"KFGQPC",
	"NotoNaskhArabic",
	"Noto Naskh Arabic",
	"GeezaPro",
	"DejaVuSans",
	"DejaVu Sans",
}

// ResolveFontFile tries to find a font file path matching the configured family.
func ResolveFontFile(family string) string {
	searchDirs := []string{}
	if home, err := os.UserHomeDir(); err == nil {
		searchDirs = append(searchDirs,
			filepath.Join(home, ".fonts"),
			filepath.Join(home, "Library", "Fonts"),
		)
	}
	searchDirs = append(searchDirs,
		"/usr/share/fonts",
		"/usr/local/share/fonts",
		"/Library/Fonts",
	)

	names := []string{}
	if family != "" {
		names = append(names, family)
	}
	for _, candidate := range fontCandidates {
		if !containsInsensitive(names, candidate) {
			names = append(names, candidate)
		}
	}

	for _, name := range names {
		needle := strings.ToLower(strings.ReplaceAll(name, " ", ""))
		for _, dir := range searchDirs {
			found := ""
			_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil || info == nil || info.IsDir() {
					return nil
				}
				base := strings.ToLower(strings.ReplaceAll(info.Name(), " ", ""))
				if strings.Contains(base, needle) && (strings.HasSuffix(base, ".ttf") || strings.HasSuffix(base, ".otf")) {
					found = path
					return filepath.SkipDir
				}
				return nil
			})
			if found != "" {
				return found
			}
		}
	}
	return ""
}

func containsInsensitive(list []string, value string) bool {
	for _, item := range list {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}
