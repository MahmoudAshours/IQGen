# IQGen / Quran Video CLI - Detailed Project Explanation

This document explains the project in depth: what the system does, how it is structured, and how the major subsystems work together. It is written as a developer-oriented guide so you can confidently modify, extend, or troubleshoot the codebase.

## 1) High-Level Goal

The project is a production-oriented CLI that generates short-form Quran verse videos. It automates:
- Quran verse retrieval
- Recitation audio fetching (or alignment to your own recitation)
- Word/ayah timing
- Background selection (local, URL, YouTube, or API providers)
- Text rendering with Arabic-friendly options
- Final video rendering with FFmpeg

The output is optimized for TikTok, Reels, and Shorts (portrait format by default), with options for translation overlays, captions, and different display modes.

## 2) Execution Flow Overview

At a high level, the CLI follows this pipeline:
1. Parse command-line arguments (generate / generate-audio / identify / batch / config).
2. Load the configuration YAML (defaults if missing).
3. Fetch Quran verses (if needed).
4. Prepare audio:
   - Download official recitation segments and concatenate, or
   - Use a user-provided audio file.
5. Compute timings:
   - Base timings from segments
   - Whisper alignment for word-level accuracy (if enabled)
   - Optional pause-sensitive splitting
6. Select or build background video:
   - Provider search (Pexels/Pixabay), AI-guided selection
   - YouTube or local file input
   - Optionally build a per-ayah background sequence
7. Render captions (.srt) if enabled.
8. Render video with FFmpeg (drawtext or ASS subtitles).

## 3) Core CLI Commands

### 3.1 generate
Creates a video using official Quran recitation audio from the CDN.

Inputs:
- Surah, start ayah, end ayah
- Display mode
- Optional background override

Key files:
- `cmd/quranvideo/main.go`

### 3.2 generate-audio
Creates a video using a local recitation audio file.

Capabilities:
- Detect surah/ayah range from the recitation (Whisper + matcher)
- Or accept the exact surah/start/end as arguments

Modes:
- sequential
- repeat (repeats partial ayahs when the reciter repeats)
- repeat-2x2 (repeat + two-by-two pairing)
- word-by-word

### 3.3 identify
Detects the surah + ayah range for a recitation audio file.

### 3.4 batch
Runs multiple generation jobs from a YAML batch file.

### 3.5 config init
Creates a default configuration file in the standard location.

## 4) Configuration System

Configuration is YAML-driven, loaded via `internal/config`.

Key top-level sections:
- `quran_api`:
  - Base URL, edition, translation, reciter
- `audio`:
  - CDN base URL, bitrate, word timing strategy, pause sensitivity
  - Whisper settings, silence trimming
- `background`:
  - Provider choice, API keys, min duration
  - Exclude people/religious content, AI selection
- `ai`:
  - Local LLM endpoint for keyword extraction or selection
- `video`:
  - Resolution, display mode, renderer, fonts, spacing
  - Elongation (kashida), fade in/out, glass effect
- `output`:
  - Output directory and temp directory

### 4.1 Display Modes
The renderer supports multiple modes:
- `sequential`: full ayah on screen
- `word-by-word`: one word at a time
- `two-by-two` / `pair` / `2x2`: two words at a time
- `repeat`: sequential display but repeated recitation is shown again
- `repeat-2x2`: repeat mode + two-by-two word pairing

### 4.2 Renderer Options
Two renderers:
- `drawtext`: FFmpeg drawtext filter
- `ass`: libass subtitles renderer

ASS is more robust for complex Arabic shaping (with correct fonts and shaping libraries installed).

### 4.3 Kashida / Elongation
`video.elongate` enables automatic elongation to widen lines, with `video.elongate_count` controlling the number of tatweel characters inserted for each underscore `_`.

Manual underscores are also supported. The system attempts to insert elongation after eligible Arabic letters and avoid inserting after non-connecting letters (such as ا/أ/إ/آ/د/ذ/ر/ز/و/ؤ/ء/ى/ة).

## 5) Audio Subsystem

Audio handling lives in `internal/audio`.

Capabilities:
- Download CDN recitation segments
- Concatenate segments into one audio file
- Detect silences (FFmpeg silencedetect)
- Trim silence from audio segments

Timing is based on segment durations, then optionally refined with Whisper.

## 6) Word Alignment (Whisper)

Whisper alignment is handled in `internal/align/whisper.go`.

Key ideas:
- We prompt Whisper with the expected verse text (when available)
- We extract word-level timings from the JSON output
- We normalize Arabic tokens to reduce mismatch
- We use LCS and fallback strategies to align text with Whisper output

Alignment is used for:
- Word-by-word display
- Two-by-two display
- Adjusting ayah boundaries in sequential mode (generate-audio)

## 7) Repeat Mode (Reciter Repeats Ayahs)

Repeat mode is a special pipeline used for local recitations:

1. Whisper transcribes the full audio with word timings.
2. The audio is split into segments by silence.
3. Each segment is matched against the ayah corpus.
4. If the reciter repeats an ayah or a partial phrase, it is shown again.

For repeat-2x2 mode, we produce word timings per segment and render two words at a time.

## 8) Background Selection

Backgrounds are controlled in `internal/background`.

Providers:
- Pexels
- Pixabay

Capabilities:
- Random selection or context-aware search
- AI keyword extraction (optional)
- AI selection from candidates (optional)
- Exclude people or religious content
- Min duration enforcement (longer for long ayahs)

Other sources:
- Local file
- Direct URL
- YouTube (download only needed duration)

## 9) Rendering Pipeline

Rendering is handled in `internal/render`.

The renderer builds FFmpeg filter chains:
- `scale` and `crop` to fit resolution
- `drawtext` or `subtitles` (ASS)
- Fade in/out for text

Word modes render each word or pair with a time-based enable expression.

The output is encoded as H.264 + AAC (yuv420p) for wide compatibility.

## 10) Captions (.srt)

Captions are generated in `internal/caption`, using the final timings. This allows you to publish to platforms that accept captions or to debug timing.

## 11) Key Code Locations

- CLI and orchestration: `cmd/quranvideo/main.go`
- Config schema and validation: `internal/config`
- Quran API client: `internal/quran`
- Audio download/concat/silence: `internal/audio`
- Whisper alignment: `internal/align`
- Surah identification: `internal/recognize`
- Background providers and selection: `internal/background`
- Rendering and text shaping: `internal/render`
- Captions: `internal/caption`

## 12) Common Troubleshooting

### 12.1 Missing Arabic shaping / broken glyphs
- Ensure FFmpeg is compiled with libass + fribidi + harfbuzz.
- Use the ASS renderer for Arabic shaping.
- Make sure the font file is correctly resolved and includes Arabic glyphs.

### 12.2 Word timings overlap
- The system clamps word timings to avoid overlaps and ensures minimum word duration.

### 12.3 Background download issues
- Check API keys (Pexels/Pixabay)
- Verify network access
- Use a local or URL background to isolate issues

### 12.4 Whisper alignment mismatch
- Whisper alignment is only as good as the transcription.
- Use `audio.word_timing: whisper` or `auto` and check logs.
- Make sure the recitation is clean and not heavily noisy.

## 13) Extending the Project

Suggestions:
- Add new background providers (e.g. stock images or custom library)
- Add new display modes (e.g. 3-word chunks)
- Add more robust Arabic typography rules
- Implement a GUI wrapper

## 14) Remotion Promo Project

A separate Remotion project exists in `iqgen-promo/` to generate a marketing video. It uses:
- Inter for primary text
- Nerd Fonts for code blocks
- `public/logo.png` (copied from repo image)

## 15) Quick Command Cheat Sheet

```bash
# Generate from CDN recitations
./quranvideo generate --surah 1 --start 1 --end 7 --mode sequential

# Identify a recitation
./quranvideo identify --audio recitation.mp3

# Generate using local recitation
./quranvideo generate-audio --audio recitation.mp3 --mode repeat-2x2

# Batch jobs
./quranvideo batch --file batch.yaml

# Config init
./quranvideo config init
```
