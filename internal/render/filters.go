package render

import (
	"fmt"
	"strings"

	"qgencodex/internal/config"
)

// DrawtextArgs builds a drawtext filter for a text file.
func DrawtextArgs(textFile string, enable string, cfg config.VideoConfig, fontSize int, color string, yExpr string, alphaExpr string) string {
	font := ""
	if cfg.Font.File != "" {
		font = fmt.Sprintf("fontfile='%s'", escapeValue(cfg.Font.File))
	} else if cfg.Font.Family != "" {
		font = fmt.Sprintf("font='%s'", escapeValue(cfg.Font.Family))
	}
	if color == "" {
		color = "#FFFFFF"
	}
	borderColor := cfg.Font.OutlineColor
	if borderColor == "" {
		borderColor = "#000000"
	}
	shadowColor := cfg.Font.ShadowColor
	if shadowColor == "" {
		shadowColor = "#000000"
	}
	boxArgs := []string{}
	if cfg.Glass.Enabled {
		boxColor := cfg.Glass.Color
		if boxColor == "" {
			boxColor = "#000000"
		}
		alpha := cfg.Glass.Alpha
		if alpha <= 0 || alpha > 1 {
			alpha = 0.35
		}
		padding := cfg.Glass.Padding
		if padding <= 0 {
			padding = 12
		}
		boxArgs = []string{
			"box=1",
			fmt.Sprintf("boxcolor=%s@%.2f", boxColor, alpha),
			fmt.Sprintf("boxborderw=%d", padding),
		}
	}
	lineSpacing := cfg.LineSpacing
	if lineSpacing == 0 {
		lineSpacing = 10
	}
	args := []string{
		fmt.Sprintf("textfile='%s'", escapeValue(textFile)),
		font,
		fmt.Sprintf("fontsize=%d", fontSize),
		fmt.Sprintf("fontcolor=%s", color),
		fmt.Sprintf("bordercolor=%s", borderColor),
		fmt.Sprintf("borderw=%d", cfg.Font.OutlineWidth),
		fmt.Sprintf("shadowcolor=%s", shadowColor),
		fmt.Sprintf("shadowx=%d", cfg.Font.ShadowX),
		fmt.Sprintf("shadowy=%d", cfg.Font.ShadowY),
		strings.Join(boxArgs, ":"),
		fmt.Sprintf("line_spacing=%d", lineSpacing),
		"x=(w-text_w)/2",
		// "text_shaping=1",
		fmt.Sprintf("y=%s", yExpr),
	}
	if alphaExpr != "" {
		args = append(args, fmt.Sprintf("alpha=%s", alphaExpr))
	}
	if enable != "" {
		args = append(args, fmt.Sprintf("enable='%s'", enable))
	}
	return "drawtext=" + strings.Join(filterEmpty(args), ":")
}

func filterEmpty(parts []string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

func escapeValue(value string) string {
	// Escape single quotes and colons for ffmpeg drawtext.
	replacer := strings.NewReplacer("\\", "\\\\", ":", "\\:", "'", "\\'")
	return replacer.Replace(value)
}
