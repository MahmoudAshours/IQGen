# Ayah, Word, Pair, and Repeat Timing Algorithms

This document describes the behavior implemented by the current CLI, from Quran text and recitation audio to the timed text shown in the rendered video. It covers the normal `generate` path, the local-audio `generate-audio` path, the word and pair modes, pause handling, repeat handling, line mode, captions, and the renderer.

## 1. The central timeline model

All display modes are built from the same structure in `internal/render/timing.go`:

```go
type Timing struct {
    Verse       quran.Verse
    Start       time.Duration
    End         time.Duration
    WordTimings []WordTiming
}

type WordTiming struct {
    Word  string
    Start time.Duration
    End   time.Duration
}
```

`Timing` is an output display interval, not always exactly one original ayah.

| Field | Meaning |
| --- | --- |
| `Verse` | Canonical Quran text, translation, ayah number, and surah metadata. Some modes replace `Verse.Text` with a partial ayah or a Quran line. |
| `Start`, `End` | Absolute times in the final, concatenated audio timeline. |
| `WordTimings` | Canonical display words paired with absolute start and end times. The words retain canonical Quran spelling even when Whisper transcribes a different spelling. |

The normal shape of the pipeline is:

```text
canonical verses + audio
  -> initial []Timing
  -> optional Whisper alignment
  -> offset, boundary, and timing repair
  -> mode-specific transformation
  -> drawtext or ASS timed overlays
  -> FFmpeg video, optionally with SRT
```

The canonical verse text is the source of truth for what is displayed. Whisper supplies acoustic timing evidence, not display text.

## 2. Mode names and aliases

The mode is lowercased before the timing logic chooses a path. These aliases are treated as the same output category.

| Category | Accepted names | What is displayed |
| --- | --- | --- |
| Sequential ayah | `sequential` | One full ayah, or one pause-derived ayah fragment, at a time. |
| Quran line | `line`, `lines`, `line-by-line` | A prepared Quran page-line fragment at a time. |
| Word | `word-by-word`, `word` | One canonical word at a time. |
| Pair | `two-by-two`, `two`, `pair`, `2x2` | Consecutive two-word groups, with a final one-word group if the count is odd. |
| Repeat ayah | `repeat`, `sequential-repeat` | A recited ayah or partial ayah segment each time it is detected. |
| Repeat pair | `repeat-2x2`, `repeat-two-by-two`, `repeat-pair` | Repeat-aware segments rendered as two-word groups. |
| Captions only | `caption`, `captions`, `captions-only`, `srt`, `subtitle`, `subtitles` | Sequential timing written to SRT; video rendering is skipped. |

`isWordMode` includes ordinary word and pair aliases, but not repeat-pair aliases. The caller explicitly adds repeat-pair to the normalization path.

## 3. Two audio sources, two initial-timing strategies

### 3.1 Official CDN audio: one segment per ayah

`generate` downloads one audio file per requested ayah. Each `audio.Segment` contains the global ayah number, file path, and probed duration. The files are concatenated in requested order.

`render.BuildTimings` requires exactly as many verses as segments. It then walks a cumulative cursor:

```text
ayah[i].Start = sum(segment[0].Duration ... segment[i-1].Duration)
ayah[i].End   = ayah[i].Start + segment[i].Duration
```

It initializes word timings by splitting `strings.Fields(Verse.Text)` uniformly across that ayah's segment:

```text
perWord = segmentDuration / wordCount
word[0] = [ayahStart, ayahStart + perWord]
word[1] = [previousEnd, previousEnd + perWord]
...
lastWord.End = ayahEnd
```

The last word receives the remainder from integer duration division, so the word intervals exactly end at the segment end.

This initial timeline is already reliable at the ayah level because the audio files themselves are ayah files. The individual word intervals are only estimates until Whisper replaces them.

### 3.2 Uploaded or downloaded recitation audio: weighted estimates first

For `generate-audio`, the input is one continuous recitation file. Before full-audio alignment exists, the program needs provisional ayah ranges. `buildSegmentsFromDuration` allocates the full duration to the selected ayahs by text weight:

```text
weight(ayah) = number of whitespace-separated Quran words
if no words: weight = runeCount(text) / 3
if still zero: weight = 1

portion = totalAudioDuration / sum(all weights)
provisionalAyahDuration = portion * ayahWeight
lastAyahDuration = remaining duration
```

Those provisional segments are passed into `BuildTimings`, which again creates an even word split. They are estimates only. Word, line, and local sequential modes subsequently try to replace them with full-audio Whisper evidence.

## 4. Identifying an uploaded recitation when no range is supplied

`generate-audio` can accept an explicit `--surah --start --end`. If all three are present, no recognition is needed. Otherwise it identifies the range before rendering.

The fast path performs one word-timestamp transcription, joins non-empty Whisper words with spaces, and sends that transcript to `recognize.Matcher`. If the input was not silence-trimmed or echo-reduced, those same precomputed word timestamps can be reused later for full-audio alignment.

If the word-timestamp fast path fails, the fallback requests a normal Whisper transcript and runs the same matcher.

### 4.1 Matcher algorithm

For each candidate surah, or only `--expected-surah` when set:

1. Fetch all canonical ayahs for the surah.
2. Normalize and flatten every ayah into one token list while storing the source ayah number beside every token.
3. Run local sequence alignment between transcript tokens and the flattened surah tokens.
4. Convert the best local-match start and end token indices back to start and end ayah numbers.
5. Compare surah candidates by, in order: match count, transcript coverage, alignment score, then aligned length.
6. Reject the final result unless it meets both an adaptive minimum-match threshold and an adaptive coverage threshold.

The adaptive matcher thresholds are:

| Transcript token count | Minimum exact matches | Minimum coverage |
| --- | --- | --- |
| 1-5 | all tokens | 50% |
| 6-11 | 3 | 50% |
| 12-19 | 4 | 40% |
| 20 or more | tokenCount / 4 | 35% |

Recognition normalization removes tatweel, combining marks, punctuation, symbols, and repeated whitespace; lowercases text; and also folds several Arabic variants: `alif` forms to plain alif, alif maqsura to ya, hamza-on-waw to waw, and hamza-on-ya to ya. The word-alignment normalizer does not do those letter folds, so recognition is intentionally more tolerant than normal word matching.

## 5. Whisper transcription and canonical word alignment

### 5.1 Raw Whisper words

The aligner exposes two related operations:

| Operation | Input | Result |
| --- | --- | --- |
| `TranscribeWordsContext` | Audio only | Raw Whisper words and absolute audio times. Used by repeat mode and the identify fast path. |
| `AlignContext` | Audio plus canonical words | Canonical words with Whisper-derived times. Used for normal word, pair, line, and sequential alignment. |

For Python Whisper, the first attempt uses Arabic language selection, word timestamps, JSON output, deterministic temperature zero, beam size five, and `best_of` five. If that command fails, it retries with only the basic word-timestamp JSON arguments.

For go-whisper, it invokes `transcribe <model> <audio> --format json`, adds the language when supplied, and optionally enables remote mode. The parser accepts several JSON shapes. If a go-whisper segment has text but no word array, it uniformly splits that segment's time across whitespace-separated segment words. A malformed or zero-length segment is assigned at least 300 ms before this fallback split.

All raw Whisper words are trimmed. An end before a start is corrected to the start.

Important implementation detail: `AlignContext` passes the expected verse words into `transcribeFlatWords` as a `prompt` argument, but the current Python and go-whisper command construction does not send that prompt to either backend. Canonical text influences the matching stage after transcription; it is not currently an STT prompt.

### 5.2 Word normalization

`align.NormalizeWord` removes:

- leading and trailing whitespace
- tatweel
- Unicode combining marks, including Arabic harakat
- Unicode punctuation
- Unicode symbols

It lowercases what remains. It does not fold Arabic alif or hamza variants. `normalizeForMatch` additionally removes any remaining spaces from the normalized result.

This means canonical text with tashkeel and a Whisper token without tashkeel can match, but two orthographic letter variants may not.

### 5.3 The alignment cascade

`alignToFlatWords` tries these strategies in this exact order. The first accepted result is returned.

#### Strategy A: alignment-unit LCS

The intended purpose is to combine a standalone clitic such as `waw`, `fa`, `ba`, `lam`, `kaf`, or `sin` with the following canonical word when Whisper emits the combined spelling.

1. Build alignment units from canonical words.
2. Run longest common subsequence, or LCS, over normalized units and normalized Whisper words.
3. Accept if at least 25% of units matched, or if there are at least two matches.
4. Copy matched Whisper ranges into their units.
5. Fill unmatched unit ranges from adjacent matches.
6. Expand each unit back to canonical words.

For a multi-word unit, its interval is partitioned in proportion to each canonical word's visible-letter count. That count ignores tatweel, marks, punctuation, symbols, and whitespace. The final word receives the rounding remainder.

Current caveat: `isClitic` checks `len(norm) == 1`, which measures UTF-8 bytes rather than Unicode runes. Arabic one-letter clitics are multibyte strings, so they do not satisfy that check in the current implementation. In practice, Arabic inputs normally skip the intended merged-clitic path and continue as ordinary single-word units.

#### Strategy B: normalized forward scan

The code walks the canonical list and raw Whisper list forward. For each canonical word, it scans ahead until it finds the same normalized Whisper token. Extra Whisper words are permitted; every canonical word must be found in order. If one cannot be found, this strategy fails.

This is the cheapest successful path, with linear scan behavior across the two token streams.

#### Strategy C: ordinary word-level LCS

This runs a full dynamic-programming LCS over normalized canonical and Whisper words. A non-empty exact normalized match scores as a subsequence match. It is accepted only when at least 45% of canonical words match. Matched canonical words take the corresponding Whisper timestamps; unmatched canonical words are filled afterward.

Time and memory cost are both `O(canonicalWordCount * whisperWordCount)` because the whole LCS matrix is stored for backtracking.

#### Strategy D: index-strict fallback

This is used only when the canonical and Whisper lists have the same length and at least 50% of their positions have equal normalized words. If accepted, every canonical word receives the timestamp at the same Whisper index, including positions that did not match textually.

If all strategies fail, the caller keeps the initial even timing for that ayah or the whole input rather than aborting rendering.

### 5.4 Missing-word interpolation

LCS-based strategies fill unmatched canonical entries using surrounding matched times.

For a gap between matched indices `prev` and `next`:

```text
gapStart = previous matched word end, or first Whisper word start
gapEnd   = next matched word start, or last Whisper word end
count    = next - prev - 1
step     = (gapEnd - gapStart) / (count + 1)
```

The current implementation gives each missing item:

```text
start = cursor + step
end   = start + step
cursor = end
```

This behavior leaves a leading step before the first filled item. With multiple consecutive missing words it can also extend beyond the next anchor; later timing normalization enforces monotonicity and may extend the enclosing ayah. This is important when diagnosing unusual long gaps or an ayah end that moves later than expected.

## 6. Where alignment runs

### 6.1 CDN audio: per-ayah alignment

For official audio, each ayah segment is aligned independently. Jobs run concurrently, using `audio.whisper_workers` when positive or half the CPU count otherwise, capped at the number of jobs.

Whisper produces times relative to the isolated ayah file. The code makes them final-timeline times by adding the ayah's cumulative `Timing.Start`.

An alignment failure for one ayah logs a warning and leaves that ayah's even split in place. Other ayahs can still receive Whisper timing.

### 6.2 Local audio: one full-audio alignment

For uploaded recitations, all canonical words from all selected ayahs are flattened into one list. A parallel `verseIndex` list records which ayah each canonical word belongs to.

Whisper aligns against the full file once. The result is distributed back to `Timing.WordTimings` according to `verseIndex`. This avoids the provisional local-audio ayah boundaries being used as hard constraints.

Precomputed identify words can replace a second transcription pass. The reuse path calls `AlignWordsToTranscript` with canonical words and the already-transcribed raw word timings.

`audio.word_timing` controls whether the program tries Whisper:

| Setting | Behavior |
| --- | --- |
| `even` | Do not invoke alignment. Retain even timing. |
| `auto` | Try alignment when needed; retain even timing if unavailable or unsuccessful. |
| `whisper` | Also tries alignment, logs a warning when the backend is unavailable, but still retains even timing rather than making rendering fail. |

## 7. Timing cleanup after alignment

Word, pair, line, and repeat-pair paths apply cleanup after their word source has been assembled.

### 7.1 Global offset

`audio.word_offset_ms` is added to every word start and end. Each shifted range is clamped to the current enclosing ayah interval.

When `audio.auto_word_offset` is enabled and at least three usable word starts exist, the code adds an audio-derived correction:

1. Decode mono 16 kHz signed 16-bit PCM with FFmpeg.
2. Calculate RMS energy for each 10 ms frame.
3. Inspect up to 80 representative predicted starts inside a configurable window, default 80 ms.
4. Near each start, select the first rising energy crossing at 35% of local energy range. If none exists, select the largest positive energy rise.
5. Keep only corrections inside the plus/minus window.
6. Take the median correction and add it to the configured fixed offset.

The default fixed offset is -20 ms. The estimated onset correction is global, not per word.

### 7.2 Ayah boundaries from word endpoints

`applyAyahBoundariesFromWordTimings` changes each ayah's outer timing to:

```text
Start = first WordTimings entry Start
End   = last WordTimings entry End
```

It then prevents a later ayah from starting before the prior ayah's end. It assumes word timings are in canonical order; it does not search for the minimum and maximum timestamps.

The code deliberately applies this before normalization. That preserves valid full-audio word times that might be outside the original provisional ayah range.

### 7.3 Standalone marks and minimum word duration

`normalizeWordTimings` applies these rules for every timing record with words:

1. A token with no Unicode letter or number is a standalone mark.
2. A standalone mark after a word is appended to that preceding word and can extend its end.
3. A standalone mark before the first real word is held pending, then prepended to the next word and can expand that word's interval backward or forward.
4. A final pending mark is appended to the last real word; an all-mark input remains one item.
5. Every word start is clamped to the ayah start and to the prior word's end.
6. Every end is made at least its start.
7. Every word is extended to at least 100 ms.
8. An extension beyond the ayah end enlarges the ayah end instead of clipping the word.

The mark collapse prevents a Quranic punctuation mark from becoming its own word flash or its own two-word-pair member.

### 7.4 Compressed-ayh repair

For every non-final ayah with words, `repairCompressedVerseWordTimings` detects a suspiciously compressed span:

```text
observedSpan = lastWord.End - firstWord.Start
requiredSpan = wordCount * 250 ms
availableSpan = firstWordOfNextAyah.Start - firstWord.Start
```

If `observedSpan < requiredSpan` and `availableSpan >= requiredSpan`, it considers the current Whisper ayah collapsed. It evenly redistributes the current ayah's word texts across the interval ending at the next ayah's first word and moves the current ayah end to that boundary.

This repair does not run for the final timing record because there is no following word boundary to trust.

## 8. Sequential ayah mode

Sequential mode renders one `Timing.Verse.Text` between its `Start` and `End`.

For local audio, even sequential mode invokes full-audio alignment when possible. It uses the first and last aligned word times to tighten the ayah display boundary.

### 8.1 Continuous sequential timing

When `audio.pause_sensitive` is false, `ensureContinuousTimings` makes the display cover the full audio duration:

1. Move the first start to zero when it was later than zero.
2. Fix any inverted interval by setting end to start.
3. If a later ayah overlaps the prior one, move the later start to the prior end.
4. If there is a gap, extend the prior ayah end to the later start.
5. Extend the final ayah end to the full audio duration when needed.

This chooses no blank gaps over literal silence gaps.

### 8.2 Pause-sensitive sequential timing

When `audio.pause_sensitive` is true, FFmpeg `silencedetect` finds intervals below `pause_db`, default -35 dB, for at least `pause_sec`, default 0.2 seconds. The program tries to split each ayah around those intervals and hides text during the detected silence.

With word timing:

1. Consider only silences that start inside the ayah interval.
2. Find the first word after the last word whose end is at or before silence start.
3. Reject a split that would leave a tiny side: a one-word side whose visible-letter count is at most one.
4. Build word-count groups from accepted boundaries.
5. Derive each speech fragment from its first and last word times.
6. Merge tiny text fragments and fragments shorter than 120 ms with a neighbor.
7. Split the canonical Arabic words among the new display records by the computed word counts.
8. Split translation words proportionally by those count weights.

Without usable word timing, it subtracts silences directly from the ayah interval, removes fragments shorter than 120 ms, allocates Arabic words in proportion to speech-fragment duration, then applies the same small-fragment merge logic. If splitting produces no usable result, it keeps the original timing.

The split records intentionally have no `WordTimings`: they are sequential speech chunks, not a new word-mode timeline.

## 9. Word-by-word mode

Word mode loops through `Timing.WordTimings` and creates one overlay per word:

```text
visible(word[i]) when t is between word[i].Start and word[i].End
```

The text is the canonical word after standalone-mark normalization. Ayah brackets are added unless the configured font-family name contains `cairo`. Optional kashida elongation is applied to the display string after bracket construction.

Word mode does not render an English translation overlay, even when `--translation` is enabled. Translation is only generated in the sequential and line renderer branches.

If Whisper is unavailable or all alignment strategies fail, word mode can still render using the initial even word split. This is a graceful fallback, not an exact acoustic result.

## 10. Two-word pair mode

Pair mode is a timing transformation inside the renderer. It does not ask Whisper for special pair timestamps. It first obtains ordinary canonical word timings, then groups indices `(0, 1)`, `(2, 3)`, and so on.

### 10.1 Constructing pairs from word timings

For every pair beginning at word `i`:

```text
text  = trim(word[i].Word)
start = word[i].Start
end   = word[i].End

if word[i + 1] exists:
    append its non-empty text with one space
    end = max(end, word[i + 1].End)
```

An odd final word becomes a one-word pair. The pair starts at its first word, not the minimum of both words, and ends at the maximum available end.

If any constructed pair has empty text or `End <= Start`, the renderer discards the entire timed-pair result and evenly creates all pairs from the word texts across the enclosing ayah range.

If `WordTimings` is absent completely, it creates even pairs directly from `Verse.Text`.

### 10.2 Smoothing pair duration

Timed pairs are passed through `smoothPairDurations` with a target minimum of 300 ms.

1. Set the span to `[firstPair.Start, lastPair.End]`.
2. Calculate each non-negative original pair duration.
3. Scale all durations so their sum exactly equals the full span. This removes any gaps between pair ranges.
4. Set `minEffective = min(300 ms, totalSpan / pairCount)`.
5. For each too-short pair from left to right, borrow duration from later pairs, searching longest-index first, without reducing a donor below `minEffective`.
6. Rebuild all pair intervals contiguously from the first start. Force the final pair to end exactly at the original final end.

Therefore pair timing is intentionally less literal than word timing. It preserves the global start and end and tries to preserve relative duration, but packs pairs into a continuous run and prevents unreadably fast flashes when possible.

Like word mode, pair mode renders Arabic only. It does not add translation or the sequential reference label.

## 11. Repeat and repeat-pair mode

Repeat modes are only specially built for local audio. They do not start from the provisional ayah intervals.

### 11.1 Segmenting the recitation

1. Transcribe the entire audio into raw Whisper words.
2. Detect silence intervals with the configured pause threshold and duration.
3. Subtract silence from the full audio interval.
4. Remove non-speech segments shorter than 120 ms.
5. If no usable segment remains, fall back to one full-audio segment.
6. Put a raw Whisper word in a segment when the midpoint of its timestamp lies within that segment.

### 11.2 Choosing an ayah for each speech segment

Every candidate ayah is tokenized with `strings.Fields` and normalized using the word-alignment normalizer. The algorithm keeps a `current` ayah index and considers only that ayah and the immediately following ayah for each segment.

For each candidate, `localAlignTokens` runs local dynamic-programming alignment between segment tokens and ayah tokens:

| Event | Score change |
| --- | --- |
| Exact non-empty normalized token match | +2 |
| Gap or mismatch move | -1 |
| Non-positive result | reset to zero |

The DP keeps only the previous and current rows, so its memory is `O(ayahTokenCount)` and its time is `O(segmentTokenCount * ayahTokenCount)`. It tracks match count, start index, aligned length, score, and best end index. Ties prefer higher score, then more matches, then longer aligned span.

A candidate is accepted when it has a valid local match and either satisfies the normal match count or has at least 20% coverage:

```text
minimum matches = 1 for a segment with fewer than 4 tokens
minimum matches = 2 for a segment with 4 or more tokens
coverage = exactMatches / segmentTokenCount

reject only when matches < minimumMatches AND coverage < 0.20
```

Between current and next ayah, the selection prefers more matches, then higher coverage, then higher score, then longer span. `current` only advances. A repeated current ayah can be matched again, but once the cursor advances, a repeat of an older ayah is not searched.

The matched canonical start and end word indices determine what is displayed. A partial match displays only the corresponding canonical phrase and clears translation. A complete matched ayah keeps translation.

### 11.3 Repeat-pair word times

Ordinary `repeat` renders the segment text sequentially and does not need word times. `repeat-2x2` asks for word timing per matched segment.

For a segment phrase and its raw Whisper words:

1. If the canonical and raw word counts are equal, assign by position directly.
2. Otherwise run normalized LCS and assign every matched canonical word its raw Whisper range.
3. If no LCS pairs exist, evenly split the whole segment across canonical words.
4. If some words matched, fill unmatched words from surrounding times.
5. Run the shared standalone-mark, minimum-duration, boundary, and compressed-ayah repair stages.
6. Feed the result to the normal pair builder.

There is no LCS quality threshold in this repeat-specific word mapper. One exact LCS pair is enough to use interpolation for the rest.

## 12. Quran line mode

Line mode uses `lines/<surah>.txt`, or `QURAN_LINES_DIR/<surah>.txt`, rather than simply wrapping a full ayah. A line file may contain markers such as `( 3 )` to identify ayah boundaries.

The loader walks each source line while tracking the current ayah number. Text before a marker belongs to the current ayah; after marker `N`, following text belongs to ayah `N + 1`. It keeps the text parts that overlap the requested ayah range and combines them into output Quran lines.

`buildLineModeTimings` then:

1. Collects chronological Arabic word intervals from original `WordTimings`, skipping empty words and standalone marks.
2. Uses even word intervals from each outer timing only when word timings are absent.
3. Uses each prepared Quran line's word count as a weight.
4. Allocates the collected timed-word count over line weights while guaranteeing at least one word per remaining line where possible.
5. Gives a line the first and last timestamp of its allocated timed words.
6. Collects all translation words and allocates them using the resulting per-line word counts.
7. Produces new sequential timing records whose Arabic text is the prepared line text.

Line mode needs word timing to anchor lines to recitation. If it fails to load or build lines, it falls back to sequential timing.

## 13. Captions-only mode

Captions-only mode internally uses sequential timing logic. It can therefore benefit from local-audio full alignment and from pause-sensitive splitting, but it does not render video.

`caption.WriteSRT` writes one entry per final `Timing`:

```text
index
HH:MM:SS,mmm --> HH:MM:SS,mmm
Arabic text
optional translation
```

It does not generate word-level or pair-level SRT entries.

## 14. Rendering the final intervals

The renderer computes video duration from `timings[len(timings)-1].End`, then emits either FFmpeg drawtext filters or an ASS subtitle file.

### 14.1 Drawtext renderer

For sequential and line modes, each final timing produces Arabic text, optional translation, and optional ayah reference overlays. For word and pair modes, each word or pair produces an Arabic-only text file and an FFmpeg enable expression:

```text
between(t, startSeconds, endSeconds)
```

The expression is inclusive at both endpoints, so adjacent elements can technically both be enabled at their exact shared timestamp. Start and end values are formatted to milliseconds for drawtext.

### 14.2 ASS renderer

The ASS path builds an equivalent `Dialogue` record per ayah, word, or pair. Its timestamps are rounded to centiseconds. It uses ASS fade overrides when fades are configured and is generally the more robust option for Arabic shaping when FFmpeg has libass, FriBidi, HarfBuzz, and a suitable Quran font.

Both renderers apply `fade_in_ms` and `fade_out_ms` separately to every display item. Neither renderer reorders or changes timing after the timing pipeline finishes.

## 15. Configuration controls that change this algorithm

| Setting | Effect |
| --- | --- |
| `audio.word_timing` | `even` skips Whisper; `auto` and `whisper` attempt it then retain fallback timing on failure. |
| `audio.stt_backend`, `audio.whisper_cmd`, go-whisper settings | Select the raw-word backend. |
| `audio.whisper_workers` | Limits concurrent per-ayah alignment jobs for CDN audio. |
| `audio.word_offset_ms` | Fixed global timestamp shift. |
| `audio.auto_word_offset` and `audio.auto_word_offset_window_ms` | Enable RMS-onset median correction around predicted starts. |
| `audio.pause_sensitive`, `audio.pause_db`, `audio.pause_sec` | Control sequential pause-based splitting. |
| `audio.trim_silence` | Trims input or individual CDN segments before duration probing and alignment. |
| `audio.echo_reduction` | Sends enhanced WAV inputs to Whisper when enhancement succeeds. |
| `video.renderer` | Chooses drawtext or ASS event generation. |
| `video.elongate`, `video.elongate_count` | Change rendered Arabic string width, not the timing algorithm. |
| `video.fade_in_ms`, `video.fade_out_ms` | Change opacity envelopes, not interval boundaries. |

## 16. Practical correctness notes

- Word and pair mode are best when Whisper word timestamps are available, but they intentionally remain renderable with even timing.
- Full-audio local alignment is more important than per-ayah alignment because it can correct provisional ayah boundaries.
- A `Timing` record can be an original ayah, a phrase caused by pause splitting, a repeated partial phrase, or a Quran line. Do not assume its text always equals the source ayah text.
- Pair timing is a readability-oriented postprocess, not a direct acoustic measurement.
- Repeat mode only searches the current and next expected ayah. It is deliberately forward-oriented rather than a general repeated-phrase search across the whole corpus.
- Alignment and recognition use different normalization rules. A range may be recognized successfully even if strict word alignment later has fewer exact matches.
- The existing expected-text prompt argument and Arabic clitic-unit path do not currently have the intended backend/runtime effect described above.

## 17. Primary implementation locations

| Concern | Source |
| --- | --- |
| Command orchestration and mode dispatch | `cmd/quranvideo/main.go` |
| Alignment application, offsets, normalization, boundaries | `cmd/quranvideo/runtime_helpers.go` |
| Repeat mode, silence splitting, line mode | `cmd/quranvideo/timing_split.go` |
| Whisper parsing and four-stage canonical alignment | `internal/align/whisper.go` |
| Initial timing construction | `internal/render/timing.go` |
| Pair construction and smoothing | `internal/render/pairs.go` |
| Drawtext and ASS output generation | `internal/render/render.go`, `internal/render/ass.go` |
| Silence detection and onset calibration | `internal/audio/silencedetect.go`, `internal/audio/onset.go` |
| Uploaded-recitation identification | `internal/recognize/matcher.go` |
| SRT output | `internal/caption/caption.go` |
