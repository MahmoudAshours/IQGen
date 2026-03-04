**How Whisper Alignment Gets Word-by-Word Timings**

*A simple guide to the align package*

## Overview

The align package solves a common problem: you have an audio file and a list of words — and you want to know exactly when each word is spoken. It does this in two main stages:

- Stage 1 — Transcription: Feed the audio to Whisper, which returns timestamps for every word it hears
- Stage 2 — Alignment: Match your canonical word list to Whisper's output, handling differences in spelling and diacritics

## The Big Picture Flow

| Audio File (.mp3 / .wav)                                   |
|------------------------------------------------------------|
| ↓                                                          |
| Whisper STT  →  word + start_time + end_time               |
| ↓                                                          |
| Normalize both word lists  (strip diacritics, punctuation) |
| ↓                                                          |
| Alignment Algorithm  (LCS / Normalized Match / Index)      |
| ↓                                                          |
| Interpolate gaps for unmatched words                       |
| ↓                                                          |
| Your word list, each with exact Start & End time           |

## Stage 1: Transcription (Getting Raw Timings from Whisper)

The audio file is sent to Whisper using the --word\_timestamps True flag. Whisper's acoustic model does the heavy lifting — it listens to the audio and returns a word-level transcript with precise timestamps.

**Example: What Whisper Returns**

| Word (Whisper heard)   | Start Time   | End Time   |
|------------------------|--------------|------------|
| بسم                    | 0.0s         | 0.5s       |
| الله                   | 0.5s         | 1.1s       |
| الرحمن                 | 1.1s         | 1.8s       |
| الرحيم                 | 1.8s         | 2.4s       |

Note: Whisper strips diacritics and may spell words differently than your canonical list. This is why Stage 2 (alignment) is needed.

## Stage 2: Alignment — Matching Your Words to Whisper's Words

Your canonical word list may have diacritics, punctuation, or different spelling than what Whisper transcribed. The code tries four strategies in order, falling back to the next if one fails.

**The Mismatch Problem**

| Source         | Word 1   | Word 2   | Word 3   | Word 4   |
|----------------|----------|----------|----------|----------|
| Whisper output | بسم      | الله     | الرحمن   | الرحيم   |
| Your word list | بِسْمِ      | اللَّهِ     | الرَّحْمَٰنِ   | الرَّحِيمِ   |

After normalizing both (stripping diacritics and punctuation), they become identical — so timings can be directly mapped across.

## The Four Alignment Strategies

### Strategy 1 — Unit-Based LCS (Primary)

This is the main strategy. It handles a common Arabic feature: single-letter clitics (like و، ف، ب) are often written attached to the next word but Whisper may split them.

- Single-character clitics are merged with the next word into an "alignment unit"
- An LCS (Longest Common Subsequence) algorithm finds the best match between units and Whisper words
- Requires at least 25% of units to match (or at least 2 matches)

Example: و + اللَّهِ  →  unit: "والله"  (merged for matching)

### Strategy 2 — Normalized Sequential Match

Walks through both lists in order, matching each word after stripping diacritics and punctuation. Every word must match — if any word is skipped or missing, this strategy fails and the next is tried.

- Fast and simple — O(n) scan
- Strictest: requires 100% of words to match in order
- Falls through if any word is unmatched

### Strategy 3 — LCS Match

Uses the full Longest Common Subsequence algorithm on normalized words. More tolerant of differences — allows words to be missing from either list.

- Requires at least 45% of words to match
- Unmatched words get interpolated timings (see Stage 3)
- Good for audio with background noise or transcription errors

### Strategy 4 — Index-Strict Match (Last Resort)

Only used when both lists have exactly the same number of words. Compares words by position (word 1 vs word 1, word 2 vs word 2, etc.).

- Only works when len(your words) == len(Whisper words)
- Requires at least 50% positional matches

## Stage 3: Gap Filling — Interpolating Unmatched Words

When a word has no direct match from Whisper, its timing is estimated by interpolating between the surrounding matched words.

**Example: A Missing Word in the Middle**

| Word   | Match Status             | Start Time   | End Time   |
|--------|--------------------------|--------------|------------|
| بِسْمِ    | ✓ Matched                | 0.0s         | 0.5s       |
| اللَّهِ   | ✓ Matched                | 0.5s         | 1.1s       |
| الرَّحْمَٰنِ | ✗ Missing — interpolated | 1.1s         | 1.8s       |
| الرَّحِيمِ | ✓ Matched                | 1.8s         | 2.4s       |

The gap between 1.1s (end of الله) and 1.8s (start of الرحيم) is evenly divided across all unmatched words in between. This gives a reasonable estimate even without a direct Whisper match.

## Normalization: How Words Are Made Comparable

Before any matching, both word lists go through normalizeWord(), which removes everything that could cause a mismatch between your text and Whisper's output:

| What is removed             | Example            |
|-----------------------------|--------------------|
| Arabic diacritics (harakat) | بِسْمِ  →  بسم        |
| Tatweel character (ـ)       | اللـه  →  الله     |
| Punctuation marks           | الرَّحِيمِ.  →  الرحيم |
| Unicode symbols             | «text»  →  text    |
| Converts to lowercase       | ABC  →  abc        |

## Summary

Key Insight: The code never analyzes audio directly.
Whisper provides all acoustic timing data.
The align package's only job is the text-matching problem:
map YOUR word list onto WHISPER'S word list as accurately as possible.

The four-strategy cascade ensures robustness: even if transcription is imperfect or words are missing, the code falls back gracefully and always produces a complete set of timings for every word in your list.