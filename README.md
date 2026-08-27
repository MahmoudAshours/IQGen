# Islamic Quran Video CLI

A production‑oriented command‑line tool for generating Quran verse videos optimized for TikTok, YouTube Shorts, and Instagram Reels. It automates verse retrieval, recitation download, background selection, and FFmpeg rendering with Arabic‑friendly text handling.

## Highlights
- Fetch Quran verses (Uthmani and other editions) with full Tashkeel
- Optional local Quran source via `The_Holy_Quran.db`
- English translation overlay (optional)
- Download recitations from Islamic Network CDN
- Local recitation support (`generate-audio`) from file or YouTube audio
- Sequential, line-by-line, word‑by‑word, and two‑by‑two word modes
- Repeat-aware and pause-sensitive timing modes
- Optional leading Al-Fatihah + Ameen removal for uploaded recitations
- Pause‑sensitive display (text hides during silences)
- Background videos from Pexels or Pixabay, or local/YouTube inputs
- AI keyword extraction + AI video selection (local Llama/Ollama)
- Live stream / microphone caption workflows
- ASS (libass) and drawtext renderers
- Automatic captions (.srt)
- Batch jobs

## Requirements
- Go 1.21+
- FFmpeg + FFprobe in `PATH`
- Optional:
  - Whisper backend:
    - `whisper` CLI, or
    - `gowhisper` / go-whisper server setup
  - `yt-dlp` for YouTube backgrounds and YouTube audio input
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
./quranvideo random
./quranvideo identify --audio recitation.mp3
./quranvideo generate-audio --audio recitation.mp3
./quranvideo generate-audio --audio recitation.mp3 --remove-fatiha --mode word
```

## Commands
### `generate`
Generate using official ayah audio from CDN.
```bash
./quranvideo generate -surah 1 -start 1 -end 7 -mode sequential
./quranvideo generate -surah 71 -start 1 -end 10 -mode lines
./quranvideo generate -surah 2 -start 1 -end 5 --background "https://www.youtube.com/watch?v=XXXX"
```

### `generate-audio`
Use your own recitation file. Automatically detects surah/ayahs (Whisper + matcher).
```bash
./quranvideo generate-audio --audio recitation.mp3 --mode word-by-word
./quranvideo generate-audio --audio recitation.mp3 --mode two-by-two
./quranvideo generate-audio --audio recitation.mp3 --surah 2 --start 255 --end 257
./quranvideo generate-audio --audio recitation.mp3 --remove-fatiha
./quranvideo generate-audio --audio-yt-url "https://www.youtube.com/watch?v=XXXX" --mode sequential
./quranvideo generate-audio --audio recitation.mp3 --mode captions --output output/captions.srt
```

Useful flags:
- `--audio`: local audio file
- `--audio-yt-url`: download recitation audio from YouTube first
- `--yt-dlp-cmd`: custom `yt-dlp` path
- `--expected-surah`: hint recognition toward one surah
- `--surah --start --end`: skip recognition and force the ayah range
- `--remove-fatiha`: remove leading Al-Fatihah + Ameen and any pre-recitation gap
- `--mode`: `sequential|lines|word-by-word|two-by-two|repeat|repeat-2x2|captions`

### `random`
Generate a video from a uniformly selected consecutive ayah range within one surah. It defaults to three ayahs, sequential mode, translation disabled, and the configured reciter and background settings.
```bash
./quranvideo random
./quranvideo random --ayahs 5
./quranvideo random --translation
./quranvideo random --no-background --output output/daily-ayah.mp4
```

### `identify`
Detect surah + ayah range from a recitation file.
```bash
./quranvideo identify --audio recitation.mp3
./quranvideo identify --audio recitation.mp3 --expected-surah 2
./quranvideo identify --audio recitation.mp3 --remove-fatiha
./quranvideo identify --audio-yt-url "https://www.youtube.com/watch?v=XXXX"
```

### `live`
Live captioning for a fixed ayah range or a stream source.
```bash
./quranvideo live --surah 1 --start 1 --end 7 --mode full
./quranvideo live --yt-url "https://www.youtube.com/watch?v=XXXX" --stream --stream-url udp://127.0.0.1:23000
```

### `batch`
```bash
./quranvideo batch --file batch.yaml
```

## Display Modes
- `sequential`: full ayah on screen
- `lines` / `line`: Quran line mode (uses `lines/<surah>.txt`; switches line when recitation reaches the next line)
- `word-by-word` / `word`: one word at a time (Whisper aligned)
- `two-by-two` / `two` / `pair` / `2x2`: two words at a time (Whisper aligned)
- `repeat`: show repeated recited segments too
- `repeat-2x2`: repeated segments rendered as two-word pairs
- `captions`: generate only `.srt` captions (no video rendering/background)

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
./quranvideo generate --background /path/to/background.png
./quranvideo generate --background "https://www.youtube.com/watch?v=..."
```
The tool downloads only the needed duration (audio length) for remote video backgrounds.

Background notes:
- Per-ayah background switching is supported in sequential mode.
- Context-aware search can use local AI (`llama3.2:3b` via Ollama-style endpoint).
- Guardrails exclude people and religious imagery when configured.

## Configuration
Default location: `~/.quranvideo/config.yaml`

Key settings (non‑exhaustive):
```yaml
quran_api:
  use_local_db: false
  local_db_path: ./The_Holy_Quran.db
  edition: quran-uthmani
  translation: en.sahih
  reciter: ar.alafasy

audio:
  stt_backend: auto     # auto|python|whisper|gowhisper|go-whisper
  go_whisper_model: ggml-medium-q5_0
  go_whisper_identify_model: ""
  stt_timeout_sec: 90
  word_timing: auto      # auto|whisper|even
  whisper_cmd: whisper
  whisper_workers: 2
  echo_reduction: false
  pause_sensitive: true
  pause_db: -35
  pause_sec: 0.2
  word_offset_ms: -20
  auto_word_offset: false
  trim_silence: false

video:
  renderer: drawtext     # drawtext|ass
  display_mode: sequential
  translation_font: Helvetica
  translation_spacing: 24
  elongate: false        # kashida expansion mode
  fade_in_ms: 120
  fade_out_ms: 120
  encode_preset: veryfast
  encode_crf: 20

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
- Word and pair modes rely on Whisper/go-whisper word timestamps.
- `generate-audio` sequential mode uses Whisper word alignment to tighten ayah boundaries.
- Auto word offset can further calibrate subtitle onset against audio energy.
- If `--remove-fatiha` is enabled, the tool trims leading Al-Fatihah, trailing Ameen, and the pause before actual recitation.
- `lines` mode depends on the `lines/` directory and is intended mainly for horizontal layouts.
- Local DB mode avoids Quran API fetches and reads verse text from `The_Holy_Quran.db`.
- If no background provider is configured, a solid background is used.

## Tests
```bash
go test ./...
go build -o quranvideo ./cmd/quranvideo
```

## How to use?
For more details, view [explanation](/explanation.md)
For more details about whisper alignment algorithm view [whisper_alignment](/whisper_alignment.md)
## License
MIT
