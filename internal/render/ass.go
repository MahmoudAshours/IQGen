package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"qgencodex/internal/config"
)

type assOptions struct {
	Width              int
	Height             int
	Mode               string
	Timings            []Timing
	Config             config.VideoConfig
	IncludeTranslation bool
}

func writeASSFile(dir, name string, opts assOptions) (string, error) {
	path := filepath.Join(dir, name)
	content := buildASSContent(opts)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func buildASSContent(opts assOptions) string {
	fontSize := opts.Config.Font.Size
	if fontSize <= 0 {
		fontSize = 64
	}
	fontName := assArabicFontName(opts.Config)
	outline := opts.Config.Font.OutlineWidth
	if outline == 0 {
		outline = 3
	}
	shadow := maxInt(absInt(opts.Config.Font.ShadowX), absInt(opts.Config.Font.ShadowY))
	if shadow == 0 {
		shadow = 2
	}
	primary := assColor(opts.Config.Font.Color, "#FFFFFF")
	outlineColor := assColor(opts.Config.Font.OutlineColor, "#000000")
	backColor := "&H64000000"
	borderStyle := 1
	if opts.Config.Glass.Enabled {
		backColor = assColorWithAlpha(opts.Config.Glass.Color, opts.Config.Glass.Alpha, "&H64000000")
		borderStyle = 3
		if opts.Config.Glass.Padding > 0 {
			outline = opts.Config.Glass.Padding
		}
		shadow = 0
	}
	align := assAlignment(opts.Config.TextPosition)
	margins := opts.Config.Margins
	marginL := defaultIfZero(margins.Left, 120)
	marginR := defaultIfZero(margins.Right, 120)
	marginV := defaultIfZero(margins.Bottom, 200)

	header := strings.Builder{}
	header.WriteString("[Script Info]\n")
	header.WriteString("ScriptType: v4.00+\n")
	header.WriteString("Collisions: Normal\n")
	header.WriteString(fmt.Sprintf("PlayResX: %d\n", opts.Width))
	header.WriteString(fmt.Sprintf("PlayResY: %d\n", opts.Height))
	header.WriteString("WrapStyle: 2\n")
	header.WriteString("ScaledBorderAndShadow: yes\n\n")

	header.WriteString("[V4+ Styles]\n")
	header.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	header.WriteString(fmt.Sprintf("Style: Default,%s,%d,%s,%s,%s,%s,0,0,0,0,100,100,0,0,%d,%d,%d,%d,%d,%d,%d,1\n\n",
		fontName, fontSize, primary, primary, outlineColor, backColor, borderStyle, outline, shadow, align, marginL, marginR, marginV))

	header.WriteString("[Events]\n")
	header.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	lines := buildASSLines(opts, fontSize)
	return header.String() + strings.Join(lines, "")
}

func buildASSLines(opts assOptions, fontSize int) []string {
	mode := strings.ToLower(opts.Mode)
	var lines []string
	maxWidth := maxTextWidth(opts.Config, opts.Width)
	switch mode {
	case "sequential", "repeat", "sequential-repeat":
		for _, t := range opts.Timings {
			text := assVerseText(opts.Config, maxWidth, t.Verse.Text, t.Verse.Translation, opts.IncludeTranslation, fontSize)
			lines = append(lines, assDialogue(t.Start, t.End, assFadeOverride(opts.Config), text))
		}
	case "word-by-word", "word", "two-by-two", "two", "pair", "2x2":
		for _, t := range opts.Timings {
			if mode == "two-by-two" || mode == "two" || mode == "pair" || mode == "2x2" {
				for i := 0; i < len(t.WordTimings); i += 2 {
					first := t.WordTimings[i]
					text := escapeASSText(first.Word)
					end := first.End
					if i+1 < len(t.WordTimings) {
						second := t.WordTimings[i+1]
						if second.Word != "" {
							text = text + " " + escapeASSText(second.Word)
						}
						if second.End > end {
							end = second.End
						}
					}
					lines = append(lines, assDialogue(first.Start, end, assFadeOverride(opts.Config), text))
				}
			} else {
				for _, w := range t.WordTimings {
					text := escapeASSText(w.Word)
					lines = append(lines, assDialogue(w.Start, w.End, assFadeOverride(opts.Config), text))
				}
			}
		}
	}
	return lines
}

func assDialogue(start, end time.Duration, override, text string) string {
	return fmt.Sprintf("Dialogue: 0,%s,%s,Default,,0,0,0,,%s%s\n", formatASSTime(start), formatASSTime(end), override, text)
}

func assVerseText(cfg config.VideoConfig, maxWidth int, arabic, translation string, includeTranslation bool, fontSize int) string {
	arabicFont := assArabicFontName(cfg)
	arabicLines := wrapText(arabic, maxWidth, fontSize)
	arabicLines = maybeElongateLines(cfg, arabicLines, maxWidth, fontSize)
	arabicParts := make([]string, 0, len(arabicLines))
	for _, line := range arabicLines {
		arabicParts = append(arabicParts, assFontOverride(arabicFont)+escapeASSText(line))
	}
	text := strings.Join(arabicParts, "\\N")
	if includeTranslation && translation != "" {
		small := fontSize / 2
		if small < 20 {
			small = 20
		}
		spacing := cfg.TranslationSpacing
		if spacing == 0 {
			spacing = 24
		}
		translationFont := strings.TrimSpace(cfg.TranslationFont)
		if translationFont == "" {
			translationFont = "Helvetica"
		}
		translationLines := wrapText(translation, maxWidth, small)
		translationParts := make([]string, 0, len(translationLines))
		for _, line := range translationLines {
			translationParts = append(translationParts, assFontOverride(translationFont)+escapeASSText(line))
		}
		translationText := strings.Join(translationParts, "\\N")
		gap := fmt.Sprintf("\\N{\\fs%d}\\h{\\fs%d}", spacing, small)
		text = fmt.Sprintf("%s%s%s{\\fs%d}", text, gap, translationText, fontSize)
	}
	return text
}

func escapeASSText(text string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "{", "\\{", "}", "\\}")
	return replacer.Replace(text)
}

func formatASSTime(d time.Duration) string {
	cs := int(d.Milliseconds() / 10)
	h := cs / 360000
	m := (cs / 6000) % 60
	s := (cs / 100) % 60
	c := cs % 100
	return fmt.Sprintf("%d:%02d:%02d.%02d", h, m, s, c)
}

func assArabicFontName(cfg config.VideoConfig) string {
	fontName := cfg.Font.Family
	if fontName == "" {
		if cfg.Font.File != "" {
			base := filepath.Base(cfg.Font.File)
			fontName = strings.TrimSuffix(base, filepath.Ext(base))
		} else {
			fontName = "Amiri Quran"
		}
	}
	return fontName
}

func assFontOverride(name string) string {
	name = strings.ReplaceAll(name, "\\", "\\\\")
	name = strings.ReplaceAll(name, "{", "\\{")
	name = strings.ReplaceAll(name, "}", "\\}")
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return "{\\fn" + name + "}"
}

func assFadeOverride(cfg config.VideoConfig) string {
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
	return fmt.Sprintf("{\\fad(%d,%d)}", fi, fo)
}

func assColor(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = fallback
	}
	value = strings.TrimPrefix(value, "#")
	if len(value) != 6 {
		value = strings.TrimPrefix(fallback, "#")
	}
	if len(value) != 6 {
		return "&H00FFFFFF"
	}
	r := value[0:2]
	g := value[2:4]
	b := value[4:6]
	return fmt.Sprintf("&H00%s%s%s", b, g, r)
}

func assColorWithAlpha(value string, opacity float64, fallback string) string {
	base := assColor(value, fallback)
	if opacity < 0 {
		opacity = 0
	}
	if opacity > 1 {
		opacity = 1
	}
	alpha := int((1 - opacity) * 255)
	if len(base) != 10 || !strings.HasPrefix(base, "&H") {
		return base
	}
	return fmt.Sprintf("&H%02X%s", alpha, base[4:])
}

func assAlignment(position string) int {
	switch strings.ToLower(position) {
	case "lower-third", "lower", "bottom":
		return 2
	case "upper", "top":
		return 8
	default:
		return 5
	}
}

func defaultIfZero(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
