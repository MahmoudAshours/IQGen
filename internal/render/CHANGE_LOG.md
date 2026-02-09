
## 2026-02-09

# Changelog

## Features

- **Added `internal/render/timing.go`**: 
  - Created a new file that defines the `Timing` and `WordTiming` structures to store timing information for verses and words.
  - Implemented the `BuildTimings` function to map verses and audio segments to timeline timings, calculating start and end times for each word in the verse.

- **Added `internal/render/timing_test.go`**: 
  - Created a new test file to verify the correctness of the `BuildTimings` function.
  - Implemented a test case that checks if the function correctly calculates timing for multiple verses with varying numbers of words and segments.

## Files

- `internal/render/timing.go`: Added new file
- `internal/render/timing_test.go`: Added new file

## 2026-02-09

```markdown
# Changelog

## Features

- Added a new package `render` with two files: `filters.go` and `font.go`.
  - **filters.go**:
    - Implemented the `DrawtextArgs` function to build a drawtext filter for text files.
    - Included helper functions like `filterEmpty` and `escapeValue` to process arguments for the drawtext filter.
  - **font.go**:
    - Added the `ResolveFontFile` function to resolve font file paths based on the configured family name.
    - Implemented helper functions such as `containsInsensitive` to handle case-insensitive comparisons.

These changes enable more complex text rendering and font resolution capabilities within the application.
```

## 2026-02-09

```markdown
### Features Added
- **Wrap Functionality**: Implemented a new function `wrapText` in the `render` package that splits text into lines based on a maximum width and font size. This function also handles combining characters correctly.
  
- **Elongate Lines Feature**: Added support for elongating lines by inserting tatweel characters (`ـ`) to increase line length if required.

### Test Cases Added
- **Wrap Text Splits Long Line**: A test case was added to ensure that the `wrapText` function correctly splits a long line of text into multiple lines.
  
- **Wrap Text Does Not Start With Combining Mark**: A test case was added to verify that the `wrapText` function does not start a line with combining characters.

- **Elongate Text Underscore**: A test case was added to ensure that the `elongateText` function correctly replaces underscores (`_`) with tatweel characters.
  
- **Maybe Elongate Lines**: A test case was added to verify that the `maybeElongateLines` function correctly elongates lines based on configuration settings.

### Internal Refactoring
- **Code Duplication Reduction**: The code for splitting long words and calculating approximate text width was refactored into separate functions (`splitLongWord` and `approxWidth`) to reduce duplication.
  
- **Consistent Code Style**: Ensured consistent coding style across the added files, including proper indentation and naming conventions.

These changes enhance the functionality of the text rendering system by providing more flexible line wrapping and elongation options. The addition of test cases ensures that these new features work as expected under various conditions.
```

## 2026-02-09

```markdown
## Changelog

### Version 1.0.0

- **Date:** [Insert Date]
- **Release Notes:**

  - Initial release of the Quran video rendering module.
  - Added support for generating subtitles in ASS (Advanced SubStation Alpha) format.
  - Implemented functionality to handle different reading modes (sequential, word-by-word).
  - Included options for customizing font settings and timings.
  - Ensured compatibility with various video playback software that supports ASS subtitles.

- **Features:**
  - Support for generating Quranic content in ASS format.
  - Flexible customization options for rendering preferences.
  - Robust handling of different reading modes (sequential, word-by-word).
  - Error handling and validation to ensure correct subtitle generation.

- **Dependencies:**
  - `quran` package for accessing Quranic data.
  - `config` package for managing configuration settings.
  - `time` package for handling time durations.
  - `strings` package for string manipulation.

- **Contributors:**
  - [Your Name] (Initial development)
```
