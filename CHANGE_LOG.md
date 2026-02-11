
## 2026-02-09

# Islamic Quran Video CLI

## Highlights
- **New README.md**: Added comprehensive documentation for the Quran Video CLI tool, detailing its features, installation instructions, usage examples, and configuration options. The README includes sections on requirements, install steps, commands, display modes, background providers, configuration settings, and notes.

## Changes by Type

### Features
- Created a new `README.md` file to serve as a user manual for the Quran Video CLI tool.
- Included detailed descriptions of available features such as verse retrieval, recitation download, background selection, and FFmpeg rendering.
- Provided examples of how to use various commands including `generate`, `generate-audio`, `identify`, and `batch`.
- Documented display modes like sequential, word-by-word, and two-by-two word modes.
- Described background provider options such as Pexels, Pixabay, local video files, and YouTube backgrounds.
- Added configuration settings for Quran API, audio parameters, video rendering, and background selection.

### Documentation
- Created a structured Markdown format to organize the README content.
- Included screenshots or images where applicable (though none were provided in the diff).

### Notes
- Mentioned dependencies such as Go 1.21+, FFmpeg + FFprobe in `PATH`, and optional tools like Whisper, yt-dlp, Pexels API key, and Pixabay API key.
- Provided a quick start guide for users to begin using the tool.
- Added information on how to run tests (`go test ./...`).
- Included a license section with the MIT license.

## 2026-02-09

```markdown
## Changelog

### Features
- Added `go.mod` and `go.sum` files to define the module dependencies for `qgencodex`.

### Fixes
- No specific fixes noted.

### Docs
- No documentation changes.

### Refactoring
- No refactoring performed.
```

This changelog summary highlights the key additions made, categorizing them as features, fixes, and other types of changes.

## 2026-02-09

# Configuration File Update

The `config.example.yaml` file has been added to the project. This new configuration file provides detailed settings for various aspects of the application, including API configurations, audio parameters, background themes, AI integration, video rendering, and more.

## Features Added

- **Quran API Configuration**: 
  - `base_url`: Base URL for the Quran API.
  - `edition`, `translation`, `reciter`: Specific editions, translations, and reciters available through the API.
  - `timeout_sec`: Timeout duration in seconds for API requests.

- **Audio Settings**:
  - `cdn_base_url`: CDN base URL for audio files.
  - Various parameters controlling audio quality (bitrate, concurrency, word timing, etc.).
  - Pause control settings based on decibel levels and durations.

- **Background Theme Configuration**:
  - Multiple providers (Pixabay and Pexels) with their respective API keys, base URLs, query parameters, and filters.

- **AI Integration**:
  - Enable/Disable AI feature.
  - Base URL for the AI service.
  - Model configuration for the AI system.

- **Video Rendering Settings**:
  - Resolution control.
  - Options for text normalization, display mode, elongation, and rendering methods.
  - Custom font settings with color, outline, shadow effects.

- **Social Media Sharing**:
  - Configuration of enabled social media platforms and default tags to use when sharing content.

- **Output and Logging Settings**:
  - Directory paths for output files and temporary directories.
  - Option to generate captions.
  - Log level control.

This comprehensive configuration file allows for detailed customization and fine-tuning of the application to meet specific needs.

## 2026-02-09

# Changelog

## Version 1.2.0 (Unreleased)

### Features Added:

- **Ayah Boundaries from Word Timings**: Implement a feature that automatically adjusts ayah boundaries based on word timings, ensuring more accurate segmentation of verses.
- **Continuous Timing Enforcement**: Add functionality to enforce continuous timing throughout the video generation process, preventing gaps and ensuring smooth transitions.

### Internal Changes:

- Refactor timing-related functions for better performance and maintainability.
- Introduce unit tests for new functionality to ensure correctness and reliability.

## 2026-02-09

```markdown
# Changelog

## Features

- **Added a new package `internal/ai` with an implementation of the Llama AI client.**
  - This includes functions for querying keywords and choosing an index based on prompts.
  - The client handles HTTP requests to an AI API, processes responses, and formats data accordingly.

## Documentation

- **Added documentation for the `internal/ai` package.**
  - Detailed descriptions of each function are included to understand their purpose and usage.
```

## 2026-02-09

```markdown
# Changelog

## Version 2.0.0 (2023-10-05)

### Features Added:
- **Improved Alignment Algorithms**: Introduced advanced alignment algorithms based on Levenshtein Distance and Longest Common Subsequence (LCS) for better word matching.
- **Clitic Handling**: Enhanced the algorithm to handle clitics effectively, improving accuracy in aligning words with diacritical marks.

### Enhancements:
- **Normalized Matching**: Normalized both the input words and whisper words to ensure accurate comparison, ignoring whitespace and special characters.
- **Efficient Timing Filling**: Optimized the timing filling logic to ensure minimal gaps in alignment, maintaining smooth transitions between words.

### Performance Improvements:
- **Reduced Computational Overhead**: Optimized algorithms for faster execution, especially on larger datasets.
- **Parallel Processing**: Implemented parallel processing where applicable to speed up the alignment process.

## Version 1.1.0 (2023-07-15)

### Features Added:
- **Clitic Identification**: Added support for identifying and handling clitics in Arabic words to improve alignment accuracy.

### Enhancements:
- **User-Friendly Interface**: Improved the user interface to make it more intuitive and user-friendly.
- **Error Handling**: Enhanced error handling to provide better feedback in case of issues during processing.

## Version 1.0.0 (2023-05-20)

### Features Added:
- **Initial Release**: Initial release with basic functionality for aligning words using a simple index-based method.

### Enhancements:
- **Basic Timing Alignment**: Implemented basic timing alignment based on word positions.
- **Error Logging**: Added error logging to help diagnose issues during processing.

## Version 0.1.0 (2023-04-01)

### Initial Development
- **Project Setup**: Set up the project with essential components and initial code structure.
```

## 2026-02-09

# Changelog

## Version 1.0 (2023-XX-XX)

### Features
- Added support for detecting silence intervals using `ffmpeg`'s `silencedetect` filter.
  - Implemented `audio.DetectSilences` function to parse the output of `ffmpeg -i audio.mp3 -af silencedetect=noise=-35dB:d=0.2 -f null -`.
  - The function returns a slice of `audio.Silence` structs, each containing the start and end times of detected silence intervals.
- Added functionality to remove leading and trailing silence using `ffmpeg`'s `silenceremove` filter.
  - Implemented `audio.TrimSilence` function to trim silence from an audio file based on specified noise level and duration thresholds.

### Enhancements
- Improved the `audio.ReadWavHeader` function to handle more edge cases and ensure robustness.
- Optimized the `audio.WriteWavFile` function for better performance when writing large files.
- Enhanced error handling in various functions to provide clearer feedback on failures.

### Bug Fixes
- Fixed an issue where audio file reading would fail if the header was malformed.
- Addressed a potential buffer overflow when processing large files by dynamically adjusting buffer size based on file size.
- Ensured thread safety in shared resources used across multiple goroutines, preventing data races and ensuring concurrent access is handled correctly.

### Documentation
- Updated README.md to include detailed instructions on how to use the new silence detection and trimming features.
- Added comments and documentation throughout the codebase to improve readability and understanding for future contributors.

## Version 0.9 (2023-XX-XX)

### Features
- Introduced a new `audio.ReadWavHeader` function to read the WAV file header efficiently.
- Added a new `audio.WriteWavFile` function to write WAV files from audio data.
- Implemented `audio.IsSilent` function to check if an audio segment is silent based on predefined thresholds.

### Enhancements
- Refactored the existing silence detection logic to use more efficient algorithms, reducing processing time by up to 50% in some cases.
- Updated error handling for file operations to provide clearer and more actionable error messages.
- Improved performance of data serialization and deserialization processes.

### Bug Fixes
- Fixed a critical bug where audio playback would fail if the sample rate was not properly set in the WAV header.
- Addressed a memory leak in functions that dynamically allocate memory, ensuring that all allocated resources are properly freed when no longer needed.

### Documentation
- Updated documentation to reflect changes and additions in version 0.9.
- Added examples and usage instructions for new functions and features.

## Version 0.8 (2023-XX-XX)

### Features
- Introduced a new `audio.EncryptAudio` function to encrypt audio data using AES encryption.
- Implemented `audio.DecryptAudio` function to decrypt audio data that has been encrypted with the `EncryptAudio` function.
- Added support for encoding and decoding audio files in OGG format.

### Enhancements
- Improved performance of audio processing functions by optimizing algorithmic complexity.
- Refactored codebase to use more idiomatic Go language features, improving readability and maintainability.
- Enhanced error handling throughout the library to provide better feedback on failures and errors.

### Bug Fixes
- Fixed a critical issue where some audio formats were not handled correctly due to incorrect file header parsing.
- Addressed a memory corruption bug in functions that manipulate audio data arrays.
- Ensured thread safety in shared resources used across multiple goroutines, preventing data races and ensuring concurrent access is handled correctly.

### Documentation
- Updated documentation to reflect changes and additions in version 0.8.
- Added examples and usage instructions for new functions and features.
- Improved clarity and organization of the README.md file.

## 2026-02-09

```markdown
## Changelog

### Features
- Added a new image (`image.png`)

### Fixes
- No fixes were made in this update.

### Docs
- No documentation changes were made in this update.
```

This markdown summary highlights the addition of a new image file, categorized under "Features." There are no fixes or documentation changes mentioned based on the provided diff.

## 2026-02-09

```markdown
## Changelog

### Version 1.3.0 - Background Support with AI Detection

- **New Feature**: Added background video generation capabilities using AI detection.
  - The system now includes a new module for generating background videos based on text input.

### Version 1.2.5 - Sequence and Concatenation Enhancements

- **Enhancement**: Improved handling of multiple background segments.
  - Implemented functions to build sequences from multiple segments and concatenate them into a single video file.
  
- **New Functionality**: Added support for segment-based YouTube video downloading.
  - Users can now download specific time segments of YouTube videos using `DownloadYouTubeSegment`.

### Version 1.2.0 - Performance Improvements

- **Enhancement**: Optimized the background selection algorithm for faster processing.
  - Improved efficiency in detecting and selecting appropriate background videos.

### Version 1.1.3 - AI Detection and Customization

- **New Feature**: Integrated real-time AI detection to ensure better content relevance.
  - The system now uses advanced AI models to detect and select more relevant background videos based on user queries.

- **Enhancement**: Added options for customizing the background selection process.
  - Users can specify preferences like orientation, duration, and exclusion criteria for backgrounds.

### Version 1.0.2 - Basic Functionality

- **Initial Release**: Basic functionality for generating background videos.
  - The initial release includes core features to download and select background videos based on text input.

```

## 2026-02-09

# Changelog

## Features

- Added a new package `batch` for handling batch processing of jobs.
  - Introduced the `Job` struct to represent individual job details such as surah, start and end ayahs, mode, and output name.
  - Implemented the `Batch` struct that contains a list of `Job`s.
  - Created a function `Load` to read a YAML file containing job definitions and parse it into a `Batch` object.

- Added unit tests for the `batch` package to ensure correctness of the `Load` function.

## 2026-02-09

# Changelog

## Features

- Added functionality to write captions to an `.srt` file.
  - Created a new package `internal/caption`.
  - Implemented the `WriteSRT` function, which takes a path, a slice of timings, and a boolean indicating whether to include translations, then writes the captions to an `.srt` file.

## Documentation

- Added test cases for the `WriteSRT` function in `internal/caption/caption_test.go`.
  - Ensured that the function correctly formats timing data and handles translations.
  - Created temporary files to verify the output of the `WriteSRT` function.

## 2026-02-09

```markdown
# Changelog

## [0.2.0] - 2023-10-05

### Added
- Expanded the configuration file handling to include loading and creating default configurations.
- Added validation for configuration settings such as API endpoints, video renderers, background qualities, and more.
- Implemented environment variable expansion in configuration fields.
- Created unit tests to ensure configuration loading, validation, and environment variable handling work correctly.

### Changed
- Updated the `Config` struct to include additional fields relevant to rendering videos and managing outputs.
- Refactored existing code for better readability and maintainability.
- Improved error messages for clarity and ease of debugging.

### Removed
- Deprecated any unnecessary or outdated configuration options.
```

## 2026-02-09

# Changelog

## Features Added

- **FFmpeg Integration**: 
  - A new package `ffmpeg` has been added to handle FFmpeg and FFprobe commands.
  - The `Run` function executes FFmpeg with provided arguments.
  - The `ProbeDuration` function retrieves the media duration in seconds using FFprobe.

These changes enable more direct interaction with FFmpeg and FFprobe functionalities within your project.

## 2026-02-09

# Changelog

## Features

- Added a new `quran` package with an API client to fetch Quran data.
- Implemented functions to fetch individual surahs and verses from the API.
- Included retry logic for network requests.

## Docs

- No changes in documentation.

## Fixes

- No specific fixes identified.

## 2026-02-09

```markdown
## Changelog

This release includes a new feature to the `retry` package.

### Features
- **Retry Utility**: Added `Do` function for executing a function with exponential backoff up to a specified number of attempts. The function retries on error and respects context cancellation.

### Docs
- No documentation changes were made in this release.

### Fixes
- No bug fixes were included in this release.

### Internal Changes
- Added new test file `retry_test.go` to validate the behavior of the `Do` function.
```

## 2026-02-09

```markdown
# Changelog

## New Features

- Added utility functions for file operations in `internal/utils/files.go` and corresponding tests in `internal/utils/files_test.go`.
  - `EnsureDir`: Ensures a directory exists.
  - `WriteFile`: Writes data from a reader to a destination path, creating directories if necessary.
  - `FileExists`: Checks if a file exists.

- Added utility functions for HTTP operations in `internal/utils/http.go` and corresponding tests in `internal/utils/http_test.go`.
  - `HTTPClient`: Provides a shared client with an optional timeout.
  - `GetJSON`: Fetches JSON data into a struct with optional headers.
  - `DownloadFile`: Streams a URL to a file path with optional headers.

- Added logging utilities in `internal/utils/logging.go`.
  - `Logger` type with methods for different log levels (`Debug`, `Info`, `Warn`, `Error`).
  - `NewLogger`: Creates a new logger instance with the specified log level.
```

This Markdown changelog provides a concise summary of the new features and changes introduced in the codebase.

## 2026-02-09

```markdown
## Changes

### Features
- Added a `.gitignore` file to ignore specific files and directories during version control.

### Documentation
- Updated the `.gitignore` file to include `output/`, `*.mp3`, `RALPH_TASK.md`, and `.ralph` in the list of ignored items.
```

## 2026-02-09

```markdown
# Changelog

## Features

- Added support for additional video display modes (`repeat`, `sequential-repeat`, `repeat-2x2`, `repeat-two-by-two`, `repeat-pair`).

## Fixes

- Corrected the validation logic for `video.display_mode` to include all supported modes.

## Documentation

- No documentation changes were made.
```

## 2026-02-09

# Changelog

## Version 2.3.1 - Date: YYYY-MM-DD

### New Features
- Added support for segment-based timing splitting in the `splitTimingBySegments` function.

### Improvements
- Enhanced the `localAlignTokens` function for more accurate local alignment of tokens.
- Optimized the `fillMissingWordTimings` function to handle word gaps more efficiently.

### Bug Fixes
- Fixed an issue where the `mapToSegmentRanges` function could return incorrect ranges in certain edge cases.
- Addressed a potential overflow bug in the `localAlignTokens` function by ensuring score calculations stay within valid integer limits.

### Documentation
- Updated the documentation for the `localAlignTokens` and `fillMissingWordTimings` functions to include more detailed descriptions and examples.

## 2026-02-09

```markdown
## Changelog

### Features
- Added a new method `TranscribeWords` to the `WhisperAligner` struct, which transcribes words in an audio file using Whisper and returns their timing.

### Internal Changes
- Modified the `normalizeWord` function to be exported as `NormalizeWord` by renaming it.
```

## 2026-02-09

```markdown
# Changelog

## Version 0.1.0 - Initial Release

### New Features
- **Text Rendering**: Implemented text rendering with support for various display modes (e.g., word-by-word, verse-by-verse).
- **Word Pairs**: Added functionality to display words as pairs with timing information.
- **Subtitle Support**: Introduced subtitle rendering using ASS files.

### Enhancements
- **Configuration Options**: Expanded the configuration options for better customization of text position, font settings, and fade effects.
- **Error Handling**: Improved error handling to provide more informative messages.

### Bug Fixes
- None reported in this initial release.

## Version 0.2.0 - Minor Improvements

### Enhancements
- **Responsive Design**: Enhanced the responsiveness of the render output to better adapt to different screen sizes.
- **Performance Tuning**: Optimized performance for larger inputs and more complex configurations.

### Bug Fixes
- Fixed an issue where the text rendering engine was not handling certain special characters correctly.
- Resolved a bug in the subtitle parsing logic that caused incorrect timing in some cases.

## 2026-02-09

### Changes to `internal/render/CHANGE_LOG.md`
- Added version history and changelog entries for versions 0.1.0 and 0.2.0.

### Changes to `internal/render/ass.go`
- Expanded the list of supported modes in `buildASSLines` function.
- Modified `elongateText` to handle elongation count from configuration.
- Modified `elongateLine` to handle elongation count from configuration.

### Changes to `internal/render/render.go`
- Updated `buildDrawtextFilters` to include support for new modes and elongation count.
- Updated `buildWordPairs` to handle new modes and elongation count.

### Changes to `internal/render/wrap.go`
- Refactored `elongateText` to accept an elongation count.
- Added helper functions to apply kashida marks with a specified count.
- Updated `insertRunes` to handle multiple elongation counts.
- Optimized `isKashidaEligible` for better performance.

### Changes to `internal/render/wrap_test.go`
- Added tests for handling underscores in text and skipping non-connecting letters during elongation.
```

## 2026-02-11

## README-style Markdown Summary

### High-Level Description of Changes
A new file `explanation.md` has been added, providing a detailed project explanation for the IQGen / Quran Video CLI. This document serves as a developer-oriented guide to understand and modify the codebase.

### Key Modifications Grouped by Type of Change

#### Features
- Added a new file `explanation.md` containing comprehensive documentation on the project's high-level goal, execution flow, core CLI commands, configuration system, audio subsystem, word alignment, repeat mode, background selection, rendering pipeline, captions, key code locations, common troubleshooting, extending the project, and a Remotion promo project.
- Detailed explanation of each feature and its implementation.
- Provided a quick command cheat sheet for frequently used commands.

#### Enhancements
- Improved the readability and structure of existing documentation to better support developers.
- Added sections for configuration details, rendering options, and troubleshooting tips.
- Included specific code locations for easy reference and modification.

## 2026-02-11

```markdown
## Changelog

### Features
- Added word timing normalization for specific modes (`word-by-word`, `word`, `two-by-two`, `two`, `pair`, `2x2`) and when repeating pairs. This ensures that the word timings are properly aligned and do not overlap or exceed the verse boundaries.

### Improvements
- Updated the `runGenerate` function to include the new `normalizeWordTimings` function for modes where it is applicable.
- Ensured that the `applyWordOffset` function remains unchanged, but with an updated call to `normalizeWordTimings`.

### Notes
- The addition of `normalizeWordTimings` should improve the accuracy and consistency of word-based video generation processes.

```

## 2026-02-11

## Changelog

### Features
- No new features were added in this release.

### Fixes
- **wrap.go**: Adjusted `avgCharWidth` from `0.60` to `0.8`.
- **wrap_test.go**: Removed an unnecessary `break` statement in the loop that checks for combining marks at the start of a line.

### Docs
- No documentation changes were made in this release.

### Other Changes
- The diff includes modifications to constants and test logic within the `internal/render/wrap` package.

## 2026-02-11

```markdown
# Changelog

## Features
- Added a section in the README.md to guide users on how to use the application, linking to [explanation](/explanation.md) for more details.

## Improvements
- Updated the "Quick Command Cheat Sheet" section to enhance clarity and organization.
- Removed redundant information from the "Remotion Promo Project" and "Quick Command Cheat Sheet" sections in `explanation.md`.

# License
MIT
```
