package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	DefaultAppDirName = ".quranvideo"
	DefaultConfigName = "config.yaml"
)

// Config represents the full application configuration.
type Config struct {
	QuranAPI   QuranAPIConfig   `yaml:"quran_api"`
	Audio      AudioConfig      `yaml:"audio"`
	Background BackgroundConfig `yaml:"background"`
	Video      VideoConfig      `yaml:"video"`
	AI         AIConfig         `yaml:"ai"`
	Social     SocialConfig     `yaml:"social"`
	Output     OutputConfig     `yaml:"output"`
	Logging    LoggingConfig    `yaml:"logging"`
}

type QuranAPIConfig struct {
	BaseURL     string `yaml:"base_url"`
	Edition     string `yaml:"edition"`
	Translation string `yaml:"translation"`
	Reciter     string `yaml:"reciter"`
	TimeoutSec  int    `yaml:"timeout_sec"`
}

type AudioConfig struct {
	CDNBaseURL             string  `yaml:"cdn_base_url"`
	BitrateKbps            int     `yaml:"bitrate_kbps"`
	MaxConcurrent          int     `yaml:"max_concurrent"`
	WordTiming             string  `yaml:"word_timing"`
	WordOffsetMs           int     `yaml:"word_offset_ms"`
	AutoWordOffset         bool    `yaml:"auto_word_offset"`
	AutoWordOffsetWindowMs int     `yaml:"auto_word_offset_window_ms"`
	PauseSensitive         bool    `yaml:"pause_sensitive"`
	PauseDB                int     `yaml:"pause_db"`
	PauseSec               float64 `yaml:"pause_sec"`
	WhisperCmd             string  `yaml:"whisper_cmd"`
	Language               string  `yaml:"language"`
	TrimSilence            bool    `yaml:"trim_silence"`
	SilenceDB              int     `yaml:"silence_db"`
	SilenceSec             float64 `yaml:"silence_sec"`
}

type BackgroundConfig struct {
	Provider           string `yaml:"provider"`
	PexelsAPIKey       string `yaml:"pexels_api_key"`
	PexelsBaseURL      string `yaml:"pexels_base_url"`
	PixabayAPIKey      string `yaml:"pixabay_api_key"`
	PixabayBaseURL     string `yaml:"pixabay_base_url"`
	Orientation        string `yaml:"orientation"`
	MinDurationSec     int    `yaml:"min_duration_sec"`
	LongMinDurationSec int    `yaml:"long_min_duration_sec"`
	LongThresholdSec   int    `yaml:"long_threshold_sec"`
	QueryFallback      string `yaml:"query_fallback"`
	TimeoutSec         int    `yaml:"timeout_sec"`
	Quality            string `yaml:"quality"`
	MaxWidth           int    `yaml:"max_width"`
	MaxHeight          int    `yaml:"max_height"`
	MaxPixels          int    `yaml:"max_pixels"`
	UseContext         bool   `yaml:"use_context"`
	Random             bool   `yaml:"random"`
	PerAyah            bool   `yaml:"per_ayah"`
	UseAI              bool   `yaml:"use_ai"`
	AISelect           bool   `yaml:"ai_select"`
	ExcludePeople      bool   `yaml:"exclude_people"`
	ExcludeReligious   bool   `yaml:"exclude_religious"`
}

type VideoConfig struct {
	Resolution         string       `yaml:"resolution"`
	DisplayMode        string       `yaml:"display_mode"`
	Renderer           string       `yaml:"renderer"`
	TranslationFont    string       `yaml:"translation_font"`
	TranslationSpacing int          `yaml:"translation_spacing"`
	Elongate           bool         `yaml:"elongate"`
	FadeInMs           int          `yaml:"fade_in_ms"`
	FadeOutMs          int          `yaml:"fade_out_ms"`
	Font               FontConfig   `yaml:"font"`
	Glass              GlassConfig  `yaml:"glass"`
	Reference          RefConfig    `yaml:"reference"`
	Background         BgConfig     `yaml:"background"`
	Margins            MarginConfig `yaml:"margins"`
	LineSpacing        int          `yaml:"line_spacing"`
	TextPosition       string       `yaml:"text_position"`
}

type FontConfig struct {
	File         string `yaml:"file"`
	Family       string `yaml:"family"`
	Size         int    `yaml:"size"`
	Color        string `yaml:"color"`
	OutlineColor string `yaml:"outline_color"`
	OutlineWidth int    `yaml:"outline_width"`
	ShadowColor  string `yaml:"shadow_color"`
	ShadowX      int    `yaml:"shadow_x"`
	ShadowY      int    `yaml:"shadow_y"`
}

type GlassConfig struct {
	Enabled bool    `yaml:"enabled"`
	Color   string  `yaml:"color"`
	Alpha   float64 `yaml:"alpha"`
	Padding int     `yaml:"padding"`
}

type RefConfig struct {
	Enabled bool   `yaml:"enabled"`
	Color   string `yaml:"color"`
	Size    int    `yaml:"size"`
	YOffset int    `yaml:"y_offset"`
}

type BgConfig struct {
	Color string `yaml:"color"`
}

type MarginConfig struct {
	Top    int `yaml:"top"`
	Bottom int `yaml:"bottom"`
	Left   int `yaml:"left"`
	Right  int `yaml:"right"`
}

type SocialConfig struct {
	EnabledPlatforms []string `yaml:"enabled_platforms"`
	DefaultTags      []string `yaml:"default_tags"`
}

type OutputConfig struct {
	Dir      string `yaml:"dir"`
	TempDir  string `yaml:"temp_dir"`
	Captions bool   `yaml:"captions"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type AIConfig struct {
	Enabled    bool   `yaml:"enabled"`
	BaseURL    string `yaml:"base_url"`
	Model      string `yaml:"model"`
	TimeoutSec int    `yaml:"timeout_sec"`
}

// Default returns a default configuration.
func Default() Config {
	return Config{
		QuranAPI: QuranAPIConfig{
			BaseURL:     "https://api.alquran.cloud/v1",
			Edition:     "quran-uthmani",
			Translation: "en.sahih",
			Reciter:     "ar.alafasy",
			TimeoutSec:  10,
		},
		Audio: AudioConfig{
			CDNBaseURL:             "https://cdn.islamic.network/quran/audio",
			BitrateKbps:            128,
			MaxConcurrent:          3,
			WordTiming:             "auto",
			WordOffsetMs:           -20,
			AutoWordOffset:         false,
			AutoWordOffsetWindowMs: 80,
			PauseSensitive:         false,
			PauseDB:                -35,
			PauseSec:               0.20,
			WhisperCmd:             "whisper",
			Language:               "ar",
			TrimSilence:            false,
			SilenceDB:              -35,
			SilenceSec:             0.30,
		},
		Background: BackgroundConfig{
			Provider:           "pexels",
			PexelsAPIKey:       "${PEXELS_API_KEY}",
			PexelsBaseURL:      "https://api.pexels.com/videos/search",
			PixabayAPIKey:      "${PIXABAY_API_KEY}",
			PixabayBaseURL:     "https://pixabay.com/api/videos/",
			Orientation:        "portrait",
			MinDurationSec:     10,
			LongMinDurationSec: 30,
			LongThresholdSec:   25,
			QueryFallback:      "nature",
			TimeoutSec:         30,
			Quality:            "best",
			MaxPixels:          2073600,
			UseContext:         false,
			Random:             true,
			PerAyah:            false,
			UseAI:              false,
			AISelect:           false,
			ExcludePeople:      true,
			ExcludeReligious:   true,
		},
		Video: VideoConfig{
			Resolution:         "1080x1920",
			DisplayMode:        "sequential",
			Renderer:           "drawtext",
			TranslationFont:    "Helvetica",
			TranslationSpacing: 24,
			Elongate:           false,
			FadeInMs:           120,
			FadeOutMs:          120,
			Font: FontConfig{
				File:         "",
				Family:       "Amiri Quran",
				Size:         64,
				Color:        "#FFFFFF",
				OutlineColor: "#000000",
				OutlineWidth: 3,
				ShadowColor:  "#000000",
				ShadowX:      2,
				ShadowY:      2,
			},
			Glass: GlassConfig{
				Enabled: false,
				Color:   "#FFFFFF",
				Alpha:   0.20,
				Padding: 18,
			},
			Reference: RefConfig{
				Enabled: true,
				Color:   "#FFFFFF",
				Size:    28,
				YOffset: 80,
			},
			Background: BgConfig{
				Color: "#000000",
			},
			Margins: MarginConfig{
				Top:    140,
				Bottom: 200,
				Left:   120,
				Right:  120,
			},
			LineSpacing:  10,
			TextPosition: "center",
		},
		Social: SocialConfig{
			EnabledPlatforms: []string{},
			DefaultTags:      []string{"#Quran", "#Islam", "#Reminder"},
		},
		AI: AIConfig{
			Enabled:    false,
			BaseURL:    "http://localhost:11434",
			Model:      "llama3.2:3b",
			TimeoutSec: 8,
		},
		Output: OutputConfig{
			Dir:      "./output",
			TempDir:  "./output/tmp",
			Captions: true,
		},
		Logging: LoggingConfig{
			Level: "info",
		},
	}
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, DefaultAppDirName, DefaultConfigName), nil
}

// LoadOrCreate loads configuration from path, creating defaults if missing.
func LoadOrCreate(path string) (*Config, bool, error) {
	if path == "" {
		return nil, false, errors.New("config path is empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, false, err
		}
		cfg := Default()
		if err := Write(path, cfg); err != nil {
			return nil, false, err
		}
		return &cfg, true, nil
	}
	if info.IsDir() {
		return nil, false, fmt.Errorf("config path points to a directory: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, false, err
	}
	cfg.ExpandEnv()
	if err := cfg.Validate(); err != nil {
		return nil, false, err
	}
	return &cfg, false, nil
}

// Write writes configuration to path, creating parent dirs.
func Write(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ExpandEnv resolves ${VAR} in string fields.
func (c *Config) ExpandEnv() {
	c.QuranAPI.BaseURL = expandEnv(c.QuranAPI.BaseURL)
	c.QuranAPI.Edition = expandEnv(c.QuranAPI.Edition)
	c.QuranAPI.Translation = expandEnv(c.QuranAPI.Translation)
	c.QuranAPI.Reciter = expandEnv(c.QuranAPI.Reciter)
	c.Background.PexelsAPIKey = expandEnv(c.Background.PexelsAPIKey)
	c.Background.PexelsBaseURL = expandEnv(c.Background.PexelsBaseURL)
	c.Background.PixabayAPIKey = expandEnv(c.Background.PixabayAPIKey)
	c.Background.PixabayBaseURL = expandEnv(c.Background.PixabayBaseURL)
	c.Output.Dir = expandEnv(c.Output.Dir)
	c.Output.TempDir = expandEnv(c.Output.TempDir)
	c.Video.Font.File = expandEnv(c.Video.Font.File)
	c.Video.Font.Family = expandEnv(c.Video.Font.Family)
	c.Video.Font.Color = expandEnv(c.Video.Font.Color)
	c.Video.Font.OutlineColor = expandEnv(c.Video.Font.OutlineColor)
	c.Video.Font.ShadowColor = expandEnv(c.Video.Font.ShadowColor)
	c.Video.Reference.Color = expandEnv(c.Video.Reference.Color)
	c.Video.Background.Color = expandEnv(c.Video.Background.Color)
	c.AI.BaseURL = expandEnv(c.AI.BaseURL)
	c.AI.Model = expandEnv(c.AI.Model)
}

func expandEnv(value string) string {
	if value == "" {
		return value
	}
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	return re.ReplaceAllStringFunc(value, func(match string) string {
		key := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		if key == "" {
			return match
		}
		return os.Getenv(key)
	})
}

// Validate performs basic config validation.
func (c *Config) Validate() error {
	if c.QuranAPI.BaseURL == "" {
		return errors.New("quran_api.base_url is required")
	}
	if c.QuranAPI.Edition == "" {
		return errors.New("quran_api.edition is required")
	}
	if c.QuranAPI.Reciter == "" {
		return errors.New("quran_api.reciter is required")
	}
	if c.Audio.BitrateKbps <= 0 {
		return errors.New("audio.bitrate_kbps must be positive")
	}
	if c.Audio.WordTiming != "" {
		switch strings.ToLower(c.Audio.WordTiming) {
		case "auto", "whisper", "even":
		default:
			return fmt.Errorf("unsupported audio.word_timing: %s", c.Audio.WordTiming)
		}
	}
	switch strings.ToLower(c.Video.DisplayMode) {
	case "sequential", "repeat", "sequential-repeat", "repeat-2x2", "repeat-two-by-two", "repeat-pair", "word-by-word", "two-by-two", "two", "pair", "2x2":
	default:
		return fmt.Errorf("unsupported video.display_mode: %s", c.Video.DisplayMode)
	}
	if c.Background.Quality != "" {
		switch strings.ToLower(c.Background.Quality) {
		case "best", "hd", "sd", "smallest":
		default:
			return fmt.Errorf("unsupported background.quality: %s", c.Background.Quality)
		}
	}
	if c.Background.Provider != "" {
		switch strings.ToLower(c.Background.Provider) {
		case "pexels", "pixabay":
		default:
			return fmt.Errorf("unsupported background.provider: %s", c.Background.Provider)
		}
	}
	if c.Video.Renderer != "" {
		switch strings.ToLower(c.Video.Renderer) {
		case "drawtext", "ass", "subtitles":
		default:
			return fmt.Errorf("unsupported video.renderer: %s", c.Video.Renderer)
		}
	}
	if c.Video.Font.Size <= 0 {
		return errors.New("video.font.size must be positive")
	}
	if c.Video.Glass.Alpha < 0 || c.Video.Glass.Alpha > 1 {
		return errors.New("video.glass.alpha must be between 0 and 1")
	}
	if c.Output.Dir == "" {
		return errors.New("output.dir is required")
	}
	if c.Output.TempDir == "" {
		return errors.New("output.temp_dir is required")
	}
	return nil
}
