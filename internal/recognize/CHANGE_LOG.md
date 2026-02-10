
## 2026-02-09

```markdown
# Changelog

## Features
- Added `corpus.go` file with the implementation for fetching Quran verses from an API.
  - Introduced `QuranCorpus` interface with a method `FetchSurah`.
  - Defined `Ayah` struct to represent a verse of the Quran.
  - Created `APICorpus` struct to hold configuration for API requests, including base URL, edition, and timeout.
  - Implemented `FetchSurah` method in `APICorpus` to make HTTP GET request to retrieve verses from the API.
  - Handled HTTP response decoding and error checking.

## Internal
- Added new file `internal/recognize/corpus.go`.
```

## 2026-02-09

```markdown
# Changelog

## Features

- Added new package `recognize` with functionality to identify Quranic verses from audio files using Whisper.
  - Implemented the `WhisperRecognizer` struct for transcription and identification.
  - Added methods for checking if the whisper command is available, transcribing audio, and identifying verses based on a transcript.

## Key Modifications

- **New File**: A new file `internal/recognize/identify.go` has been added to implement the recognition functionality.
- **Structs and Methods**:
  - Defined a `Result` struct to hold transcription results.
  - Created a `WhisperRecognizer` struct with methods for availability, transcribing audio, and identifying verses.
  - Implemented helper functions like `NewWhisperRecognizer`, `Available`, `Transcribe`, `Identify`, and `runWhisper`.
- **Error Handling**: Added error handling for various scenarios such as command not found, empty transcription, and nil matcher.

## Grouped by Type of Change

- **Features**: The new functionality has been added to the project.
- **Fixes**: No fixes were made in this commit.
- **Docs**: No documentation changes were included with this commit.
```

This Markdown changelog provides a clear and concise summary of the changes made, grouped by type of change. It includes a high-level description of what changed and a bullet-point list of key modifications.

## 2026-02-09

```markdown
# Changelog

## Features

- Added a new `Matcher` struct in the `recognize/matcher.go` file.
  - The `Matcher` struct includes a `Corpus` field of type `QuranCorpus`.
  - It also has an optional `ExpectedSurah` field to narrow search to a single surah (1-114).
  
## Functions

### `Identify`

- Added a new function `Identify` within the `Matcher` struct.
  - This function takes a context and text as input and returns a `Result` and an error.
  - It normalizes the input text, fetches ayahs from the corpus, and attempts to match the needles (normalized tokens) against the ayahs.
  - It uses `matchSurah` to find the best matching surah based on matches, coverage, score, and length.

### `matchSurah`

- Added a new function `matchSurah`.
  - This function takes normalized tokens and ayahs as input and returns a `matchCandidate` and a boolean.
  - It flattens the ayahs into tokens and token indices.
  - It uses `localAlign` to find the best matching candidate.

### `flattenAyahTokens`

- Added a new function `flattenAyahTokens`.
  - This function takes an array of ayahs and returns normalized tokens and their corresponding ayah indices.

### `localAlign`

- Added a new function `localAlign`.
  - This function performs a local alignment between the needles (normalized tokens) and the haystack (tokens from ayahs).
  - It uses dynamic programming to find the best alignment based on score, matches, coverage, and length.
  
## Helper Functions

- Added helper functions for normalization:
  - `normalize`
  - `normalizeTokens`
  - `normalizeRune`

- Added utility functions:
  - `minRequiredMatches`
  - `minCoverage`
  - `boolToInt`
  - `pickBestCell`

These new features and utilities provide a robust system for identifying matches within Quranic ayahs based on the input text, with optional surah constraints.
```
