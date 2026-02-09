# Islamic Quran Video CLI

A production‑oriented command‑line tool for generating Quran verse videos optimized for TikTok, YouTube Shorts, and Instagram Reels. It automates verse retrieval, recitation download, background selection, and FFmpeg rendering with Arabic‑friendly text handling.

![alt text](image.png)

## Highlights
- Fetch Quran verses (Uthmani and other editions) with full Tashkeel
- English translation overlay (optional)
- Download recitations from Islamic Network CDN
- Local recitation support (`generate-audio`) with Whisper alignment
- Sequential, word‑by‑word, and two‑by‑two word modes
- Pause‑sensitive display (text hides during silences)
- Background videos from Pexels or Pixabay, or local/YouTube inputs
- AI keyword extraction + AI video selection (local Llama/Ollama)
- ASS (libass) and drawtext renderers
- Automatic captions (.srt)
- Batch jobs

## Requirements
- Go 1.21+
- FFmpeg + FFprobe in `PATH`
- Optional:
  - `whisper` CLI for word alignment
  - `yt-dlp` for YouTube backgrounds
  - Pexels or Pixabay API key for background search

## Install
```bash
go build -o quranvideo ./cmd/quranvideo
./quranvideo config init
# config at ~/.quranvideo/config.yaml
```

## Quick Start
```bash
./quranvideo generate -surah 1 -start 1 -end 7 -mode sequential
./quranvideo identify --audio recitation.mp3
./quranvideo generate-audio --audio recitation.mp3
```

## Commands
### `generate`
Generate using official ayah audio from CDN.
```bash
./quranvideo generate -surah 1 -start 1 -end 7 -mode sequential
```

### `generate-audio`
Use your own recitation file. Automatically detects surah/ayahs (Whisper + matcher).
```bash
./quranvideo generate-audio --audio recitation.mp3 --mode word-by-word
./quranvideo generate-audio --audio recitation.mp3 --mode two-by-two
```

### `identify`
Detect surah + ayah range from a recitation file.
```bash
./quranvideo identify --audio recitation.mp3
./quranvideo identify --audio recitation.mp3 --expected-surah 2
```

### `batch`
```bash
./quranvideo batch --file batch.yaml
```

## Display Modes
- `sequential`: full ayah on screen
- `word-by-word` / `word`: one word at a time (Whisper aligned)
- `two-by-two` / `two` / `pair` / `2x2`: two words at a time (Whisper aligned)

## Backgrounds
### Providers
```yaml
background:
  provider: pexels   # or pixabay
  pexels_api_key: ${PEXELS_API_KEY}
  pixabay_api_key: ${PIXABAY_API_KEY}
```

### Local or YouTube
```bash
./quranvideo generate --background /path/to/video.mp4
./quranvideo generate --background "https://www.youtube.com/watch?v=..."
```
The tool downloads only the needed duration (audio length).

## Configuration
Default location: `~/.quranvideo/config.yaml`

Key settings (non‑exhaustive):
```yaml
quran_api:
  edition: quran-uthmani
  reciter: ar.alafasy

audio:
  word_timing: auto      # auto|whisper|even
  whisper_cmd: whisper
  pause_sensitive: true
  pause_db: -35
  pause_sec: 0.2
  word_offset_ms: -20
  auto_word_offset: false

video:
  renderer: drawtext     # drawtext|ass
  display_mode: sequential
  translation_font: Helvetica
  translation_spacing: 24
  elongate: false        # kashida expansion mode
  fade_in_ms: 120
  fade_out_ms: 120

background:
  use_context: true
  random: true
  per_ayah: false
  use_ai: true           # AI keywords
  ai_select: true        # AI chooses video
  exclude_people: true
  exclude_religious: true
  long_min_duration_sec: 30
  long_threshold_sec: 25
```

## Notes
- Word modes rely on Whisper alignment for accurate timing.
- `generate-audio` sequential mode uses Whisper to align ayah boundaries.
- If no background provider is configured, a solid background is used.

## Tests
```bash
go test ./...
```

## License
MIT
