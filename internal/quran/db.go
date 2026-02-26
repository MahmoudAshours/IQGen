package quran

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultArabicTranslationID = 1
	fieldSep                   = "\x1f"
)

var surahAyahCounts = [...]int{
	7, 286, 200, 176, 120, 165, 206, 75, 129, 109, 123, 111, 43, 52, 99, 128, 111,
	110, 98, 135, 112, 78, 118, 64, 77, 227, 93, 88, 69, 60, 34, 30, 73, 54, 45, 83,
	182, 88, 75, 85, 54, 53, 89, 59, 37, 35, 38, 29, 18, 45, 60, 49, 62, 55, 78, 96,
	29, 22, 24, 13, 14, 11, 11, 18, 12, 12, 30, 52, 52, 44, 28, 28, 20, 56, 40, 31,
	50, 40, 46, 42, 29, 19, 36, 25, 22, 17, 19, 26, 30, 20, 15, 21, 11, 8, 8, 19, 5,
	8, 8, 11, 11, 8, 3, 9, 5, 4, 7, 3, 6, 3, 5, 4, 5, 6,
}

// VerseSource abstracts Quran verse providers (API or local DB).
type VerseSource interface {
	FetchVerses(ctx context.Context, surahNumber, startAyah, endAyah int, edition string, translationEdition string) ([]Verse, error)
}

// DBClient reads Quran text/metadata from a local SQLite database file.
// It uses the sqlite3 CLI to avoid CGO/runtime driver requirements.
type DBClient struct {
	Path string
}

func NewDBClient(path string) *DBClient {
	return &DBClient{Path: strings.TrimSpace(path)}
}

func (c *DBClient) FetchSurah(ctx context.Context, surahNumber int) (Surah, error) {
	if err := c.validate(); err != nil {
		return Surah{}, err
	}
	query := fmt.Sprintf(
		"SELECT ID,name,englishName,englishNameTranslation,revelationType FROM TB_Surah WHERE ID=%d LIMIT 1;",
		surahNumber,
	)
	lines, err := c.query(ctx, query)
	if err != nil {
		return Surah{}, err
	}
	if len(lines) == 0 {
		return Surah{}, fmt.Errorf("surah %d not found in local DB", surahNumber)
	}
	parts := strings.Split(lines[0], fieldSep)
	if len(parts) < 5 {
		return Surah{}, fmt.Errorf("invalid TB_Surah row for surah %d", surahNumber)
	}
	id, _ := strconv.Atoi(parts[0])
	return Surah{
		Number:                 id,
		Name:                   parts[1],
		EnglishName:            parts[2],
		EnglishNameTranslation: parts[3],
		RevelationType:         parts[4],
	}, nil
}

func (c *DBClient) FetchSurahAyahs(ctx context.Context, surahNumber int) ([]Ayah, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	query := fmt.Sprintf(
		"SELECT AyahID,AyahText FROM TB_Quran WHERE TranslationID=%d AND SuraID=%d ORDER BY AyahID;",
		defaultArabicTranslationID, surahNumber,
	)
	lines, err := c.query(ctx, query)
	if err != nil {
		return nil, err
	}
	ayahs := make([]Ayah, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, fieldSep)
		if len(parts) < 2 {
			continue
		}
		numberInSurah, _ := strconv.Atoi(parts[0])
		ayahs = append(ayahs, Ayah{
			NumberInSurah: numberInSurah,
			Number:        numberInSurah,
			Text:          parts[1],
		})
	}
	if len(ayahs) == 0 {
		return nil, fmt.Errorf("no ayahs found in local DB for surah %d", surahNumber)
	}
	return ayahs, nil
}

func (c *DBClient) FetchVerses(ctx context.Context, surahNumber, startAyah, endAyah int, _ string, translationEdition string) ([]Verse, error) {
	if startAyah <= 0 || endAyah <= 0 || endAyah < startAyah {
		return nil, fmt.Errorf("invalid ayah range: %d-%d", startAyah, endAyah)
	}
	surah, err := c.FetchSurah(ctx, surahNumber)
	if err != nil {
		return nil, err
	}
	translationMap, err := c.fetchTranslationMap(ctx, surahNumber, startAyah, endAyah, translationEdition)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(
		"SELECT AyahID,AyahText FROM TB_Quran WHERE TranslationID=%d AND SuraID=%d AND AyahID BETWEEN %d AND %d ORDER BY AyahID;",
		defaultArabicTranslationID, surahNumber, startAyah, endAyah,
	)
	lines, err := c.query(ctx, query)
	if err != nil {
		return nil, err
	}
	meta := SurahMeta{
		Number:                 surah.Number,
		Name:                   surah.Name,
		EnglishName:            surah.EnglishName,
		EnglishNameTranslation: surah.EnglishNameTranslation,
		RevelationType:         surah.RevelationType,
	}
	verses := make([]Verse, 0, len(lines))
	for _, line := range lines {
		parts := strings.Split(line, fieldSep)
		if len(parts) < 2 {
			continue
		}
		ayahID, _ := strconv.Atoi(parts[0])
		verses = append(verses, Verse{
			Number:        surahAyahToGlobal(surahNumber, ayahID),
			NumberInSurah: ayahID,
			Text:          parts[1],
			Translation:   translationMap[ayahID],
			SurahMeta:     meta,
		})
	}
	if len(verses) == 0 {
		return nil, fmt.Errorf("no verses found in local DB for surah %d range %d-%d", surahNumber, startAyah, endAyah)
	}
	return verses, nil
}

func (c *DBClient) fetchTranslationMap(ctx context.Context, surah, startAyah, endAyah int, translationEdition string) (map[int]string, error) {
	out := map[int]string{}
	edition := strings.TrimSpace(translationEdition)
	if edition == "" {
		return out, nil
	}
	translationID, err := c.resolveTranslationID(ctx, edition)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(
		"SELECT AyahID,AyahText FROM TB_Quran WHERE TranslationID=%d AND SuraID=%d AND AyahID BETWEEN %d AND %d ORDER BY AyahID;",
		translationID, surah, startAyah, endAyah,
	)
	lines, err := c.query(ctx, query)
	if err != nil {
		return nil, err
	}
	for _, line := range lines {
		parts := strings.Split(line, fieldSep)
		if len(parts) < 2 {
			continue
		}
		ayahID, _ := strconv.Atoi(parts[0])
		out[ayahID] = parts[1]
	}
	return out, nil
}

func (c *DBClient) resolveTranslationID(ctx context.Context, edition string) (int, error) {
	if id, ok := translationEditionIDAliases[strings.ToLower(strings.TrimSpace(edition))]; ok {
		return id, nil
	}
	if n, err := strconv.Atoi(strings.TrimSpace(edition)); err == nil && n > 0 {
		return n, nil
	}
	token := strings.ToLower(strings.TrimSpace(edition))
	token = strings.ReplaceAll(token, ".", " ")
	token = strings.ReplaceAll(token, "_", " ")
	token = strings.ReplaceAll(token, "-", " ")
	parts := strings.Fields(token)
	if len(parts) == 0 {
		return 0, fmt.Errorf("invalid translation edition: %q", edition)
	}
	where := make([]string, 0, len(parts))
	for _, p := range parts {
		q := sqlQuote("%" + p + "%")
		where = append(where, fmt.Sprintf("(lower(translation_name) LIKE %s OR lower(subTitle) LIKE %s)", q, q))
	}
	query := fmt.Sprintf(
		"SELECT ID FROM TB_Translations WHERE %s ORDER BY isDownloaded DESC, ID LIMIT 1;",
		strings.Join(where, " AND "),
	)
	lines, err := c.query(ctx, query)
	if err != nil {
		return 0, err
	}
	if len(lines) == 0 {
		return 0, fmt.Errorf("translation %q not found in local DB; use translation ID or known alias", edition)
	}
	id, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid translation ID for %q", edition)
	}
	return id, nil
}

func (c *DBClient) validate() error {
	if strings.TrimSpace(c.Path) == "" {
		return fmt.Errorf("local quran db path is empty")
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return fmt.Errorf("sqlite3 is required for local db mode: %w", err)
	}
	return nil
}

func (c *DBClient) query(ctx context.Context, query string) ([]string, error) {
	cmd := exec.CommandContext(ctx,
		"sqlite3",
		"-readonly",
		"-noheader",
		"-separator", fieldSep,
		filepath.Clean(c.Path),
		query,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("sqlite3 query failed: %s", msg)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return nil, nil
	}
	return strings.Split(raw, "\n"), nil
}

func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func surahAyahToGlobal(surah, ayah int) int {
	if surah < 1 || surah > len(surahAyahCounts) || ayah <= 0 || ayah > surahAyahCounts[surah-1] {
		return ayah
	}
	global := ayah
	for i := 0; i < surah-1; i++ {
		global += surahAyahCounts[i]
	}
	return global
}

var translationEditionIDAliases = map[string]int{
	"en.sahih":           17,
	"en_sahih":           17,
	"sahih":              17,
	"sahihinternational": 17,
	"en.yusufali":        16,
	"en_yusufali":        16,
	"yusufali":           16,
	"en.pickthall":       15,
	"en_pickthall":       15,
	"pickthall":          15,
}
