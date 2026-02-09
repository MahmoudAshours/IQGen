package render

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"qgencodex/internal/config"
	"qgencodex/internal/ffmpeg"
	"qgencodex/internal/utils"
)

type RenderInput struct {
	Timings            []Timing
	AudioPath          string
	BackgroundPath     string
	OutputPath         string
	TempDir            string
	Mode               string
	VideoConfig        config.VideoConfig
	IncludeTranslation bool
}

func Render(ctx context.Context, input RenderInput) error {
	if len(input.Timings) == 0 {
		return fmt.Errorf("no timings provided")
	}
	if input.VideoConfig.Font.File == "" && input.VideoConfig.Font.Family != "" {
		if resolved := ResolveFontFile(input.VideoConfig.Font.Family); resolved != "" {
			input.VideoConfig.Font.File = resolved
		}
	}
	if err := utils.EnsureDir(filepath.Dir(input.OutputPath)); err != nil {
		return err
	}
	if err := utils.EnsureDir(input.TempDir); err != nil {
		return err
	}

	width, height, err := parseResolution(input.VideoConfig.Resolution)
	if err != nil {
		return err
	}
	duration := input.Timings[len(input.Timings)-1].End
	durationSec := fmt.Sprintf("%.3f", duration.Seconds())

	filters, err := buildFilters(input, width, height)
	if err != nil {
		return err
	}

	args := []string{"-y"}
	if input.BackgroundPath == "" {
		color := input.VideoConfig.Background.Color
		if color == "" {
			color = "#000000"
		}
		colorSrc := fmt.Sprintf("color=c=%s:s=%dx%d:d=%s", color, width, height, durationSec)
		args = append(args, "-f", "lavfi", "-i", colorSrc)
	} else {
		args = append(args, "-stream_loop", "-1", "-i", input.BackgroundPath)
	}
	args = append(args, "-i", input.AudioPath)
	args = append(args,
		"-filter_complex", filters,
		"-map", "[v]",
		"-map", "1:a",
		"-t", durationSec,
		"-r", "30",
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "18",
		"-c:a", "aac",
		"-b:a", "192k",
		"-pix_fmt", "yuv420p",
		input.OutputPath,
	)
	return ffmpeg.Run(ctx, args...)
}

func buildFilters(input RenderInput, width, height int) (string, error) {
	renderer := strings.ToLower(input.VideoConfig.Renderer)
	if renderer == "" {
		renderer = "drawtext"
	}
	switch renderer {
	case "ass", "subtitles":
		return buildSubtitleFilters(input, width, height)
	default:
		return buildDrawtextFilters(input, width, height)
	}
}

func buildDrawtextFilters(input RenderInput, width, height int) (string, error) {
	mode := strings.ToLower(input.Mode)
	filters := []string{fmt.Sprintf("[0:v]scale=%d:%d:force_original_aspect_ratio=increase", width, height),
		fmt.Sprintf("crop=%d:%d", width, height)}

	fontSize := input.VideoConfig.Font.Size
	if fontSize <= 0 {
		fontSize = 64
	}
	maxWidth := maxTextWidth(input.VideoConfig, width)
	refSize := input.VideoConfig.Reference.Size
	if refSize <= 0 {
		refSize = 28
	}
	refYOffset := input.VideoConfig.Reference.YOffset
	if refYOffset == 0 {
		refYOffset = 80
	}
	refColor := input.VideoConfig.Reference.Color
	if refColor == "" {
		refColor = "#FFFFFF"
	}

	textY := textYExpr(input.VideoConfig)

	switch mode {
	case "sequential", "repeat", "sequential-repeat":
		for idx, t := range input.Timings {
			arabicLines := wrapText(t.Verse.Text, maxWidth, fontSize)
			arabicLines = maybeElongateLines(input.VideoConfig, arabicLines, maxWidth, fontSize)
			textFile, err := writeTextFile(input.TempDir, fmt.Sprintf("ayah_%d.txt", idx), strings.Join(arabicLines, "\n"))
			if err != nil {
				return "", err
			}
			enable := fmt.Sprintf("between(t,%.3f,%.3f)", t.Start.Seconds(), t.End.Seconds())
			fade := fadeAlphaExpr(input.VideoConfig, t.Start, t.End)
			filters = append(filters, DrawtextArgs(textFile, enable, input.VideoConfig, fontSize, input.VideoConfig.Font.Color, textY, fade))
			if input.IncludeTranslation && t.Verse.Translation != "" {
				transLines := wrapText(t.Verse.Translation, maxWidth, fontSize/2)
				transFile, err := writeTextFile(input.TempDir, fmt.Sprintf("translation_%d.txt", idx), strings.Join(transLines, "\n"))
				if err != nil {
					return "", err
				}
				spacing := input.VideoConfig.TranslationSpacing
				if spacing == 0 {
					spacing = 24
				}
				filters = append(filters, DrawtextArgs(transFile, enable, input.VideoConfig, fontSize/2, "#FFFFFF", fmt.Sprintf("%s+%d", textY, fontSize+spacing), fade))
			}
			if input.VideoConfig.Reference.Enabled {
				refText := fmt.Sprintf("%s • %d", t.Verse.SurahMeta.EnglishName, t.Verse.NumberInSurah)
				refFile, err := writeTextFile(input.TempDir, fmt.Sprintf("ref_%d.txt", idx), refText)
				if err != nil {
					return "", err
				}
				filters = append(filters, DrawtextArgs(refFile, enable, input.VideoConfig, refSize, refColor, fmt.Sprintf("%s+%d", textY, refYOffset), fade))
			}
		}
	case "word-by-word", "word", "two-by-two", "two", "pair", "2x2":
		for idx, t := range input.Timings {
			if mode == "two-by-two" || mode == "two" || mode == "pair" || mode == "2x2" {
				pairs := buildWordPairs(t)
				for widx, pair := range pairs {
					if pair.End <= pair.Start || strings.TrimSpace(pair.Text) == "" {
						continue
					}
					text := sanitizeText(pair.Text)
					if input.VideoConfig.Elongate {
						text = elongateText(text)
					}
					textFile, err := writeTextFile(input.TempDir, fmt.Sprintf("ayah_%d_pair_%d.txt", idx, widx), text)
					if err != nil {
						return "", err
					}
					enable := fmt.Sprintf("between(t,%.3f,%.3f)", pair.Start.Seconds(), pair.End.Seconds())
					fade := fadeAlphaExpr(input.VideoConfig, pair.Start, pair.End)
					filters = append(filters, DrawtextArgs(textFile, enable, input.VideoConfig, fontSize, input.VideoConfig.Font.Color, textY, fade))
				}
			} else {
				for widx, w := range t.WordTimings {
					word := sanitizeText(w.Word)
					if input.VideoConfig.Elongate {
						word = elongateText(word)
					}
					textFile, err := writeTextFile(input.TempDir, fmt.Sprintf("ayah_%d_word_%d.txt", idx, widx), word)
					if err != nil {
						return "", err
					}
					enable := fmt.Sprintf("between(t,%.3f,%.3f)", w.Start.Seconds(), w.End.Seconds())
					fade := fadeAlphaExpr(input.VideoConfig, w.Start, w.End)
					filters = append(filters, DrawtextArgs(textFile, enable, input.VideoConfig, fontSize, input.VideoConfig.Font.Color, textY, fade))
				}
			}
		}
	default:
		return "", fmt.Errorf("unsupported display mode: %s", input.Mode)
	}

	if len(filters) == 0 {
		return "", fmt.Errorf("no filters built")
	}
	filters[len(filters)-1] = filters[len(filters)-1] + "[v]"
	return strings.Join(filters, ","), nil
}

func buildSubtitleFilters(input RenderInput, width, height int) (string, error) {
	assPath, err := writeASSFile(input.TempDir, "captions.ass", assOptions{
		Width:              width,
		Height:             height,
		Mode:               input.Mode,
		Timings:            input.Timings,
		Config:             input.VideoConfig,
		IncludeTranslation: input.IncludeTranslation,
	})
	if err != nil {
		return "", err
	}
	fontsDir := ""
	if input.VideoConfig.Font.File != "" {
		fontsDir = filepath.Dir(input.VideoConfig.Font.File)
	}
	filters := []string{fmt.Sprintf("[0:v]scale=%d:%d:force_original_aspect_ratio=increase", width, height),
		fmt.Sprintf("crop=%d:%d", width, height),
		subtitlesFilter(assPath, fontsDir),
	}
	filters[len(filters)-1] = filters[len(filters)-1] + "[v]"
	return strings.Join(filters, ","), nil
}

func subtitlesFilter(assPath, fontsDir string) string {
	args := []string{fmt.Sprintf("subtitles='%s'", escapeValue(assPath))}
	if fontsDir != "" {
		args = append(args, fmt.Sprintf("fontsdir='%s'", escapeValue(fontsDir)))
	}
	return strings.Join(args, ":")
}

func writeTextFile(dir, name, content string) (string, error) {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func parseResolution(res string) (int, int, error) {
	parts := strings.Split(res, "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid resolution: %s", res)
	}
	var w, h int
	_, err := fmt.Sscanf(parts[0], "%d", &w)
	if err != nil {
		return 0, 0, err
	}
	_, err = fmt.Sscanf(parts[1], "%d", &h)
	if err != nil {
		return 0, 0, err
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("invalid resolution: %s", res)
	}
	return w, h, nil
}

// ParseResolution exposes resolution parsing for other packages.
func ParseResolution(res string) (int, int, error) {
	return parseResolution(res)
}

func textYExpr(cfg config.VideoConfig) string {
	position := strings.ToLower(cfg.TextPosition)
	switch position {
	case "lower-third", "lower", "bottom":
		return "(h*0.65)-(text_h/2)"
	case "upper", "top":
		return "(h*0.25)-(text_h/2)"
	default:
		return "(h-text_h)/2"
	}
}

func fadeAlphaExpr(cfg config.VideoConfig, start, end time.Duration) string {
	fi := cfg.FadeInMs
	fo := cfg.FadeOutMs
	if fi <= 0 && fo <= 0 {
		return ""
	}
	if fi < 0 {
		fi = 0
	}
	if fo < 0 {
		fo = 0
	}
	st := start.Seconds()
	et := end.Seconds()
	fiSec := float64(fi) / 1000.0
	foSec := float64(fo) / 1000.0
	// alpha: 1=transparent, 0=opaque
	if fiSec == 0 && foSec == 0 {
		return "0"
	}
	return fmt.Sprintf("if(lt(t,%.6f),1,if(lt(t,%.6f),1-(t-%.6f)/%.6f,if(lt(t,%.6f),0,if(lt(t,%.6f),(t-%.6f)/%.6f,1))))",
		st, st+fiSec, st, fiSec, et-foSec, et, et-foSec, foSec)
}
