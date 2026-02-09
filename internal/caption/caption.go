package caption

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"qgencodex/internal/render"
	"qgencodex/internal/utils"
)

// WriteSRT writes captions to an .srt file.
func WriteSRT(path string, timings []render.Timing, includeTranslation bool) error {
	if err := utils.EnsureDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	idx := 1
	for _, t := range timings {
		text := t.Verse.Text
		if includeTranslation && t.Verse.Translation != "" {
			text = fmt.Sprintf("%s\n%s", t.Verse.Text, t.Verse.Translation)
		}
		_, err := fmt.Fprintf(f, "%d\n%s --> %s\n%s\n\n", idx, formatTime(t.Start), formatTime(t.End), text)
		if err != nil {
			return err
		}
		idx++
	}
	return nil
}

func formatTime(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	ms := int(d.Milliseconds()) % 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}
