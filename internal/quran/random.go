package quran

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

// RandomAyahReference returns a uniformly selected Quran ayah reference.
func RandomAyahReference() (surah, ayah int, err error) {
	total := 0
	for _, count := range surahAyahCounts {
		total += count
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(total)))
	if err != nil {
		return 0, 0, fmt.Errorf("select random ayah: %w", err)
	}

	remaining := int(n.Int64())
	for index, count := range surahAyahCounts {
		if remaining < count {
			return index + 1, remaining + 1, nil
		}
		remaining -= count
	}
	return 0, 0, fmt.Errorf("select random ayah: invalid ayah index")
}

// RandomAyahRange returns a uniformly selected contiguous ayah range within one surah.
func RandomAyahRange(count int) (surah, startAyah, endAyah int, err error) {
	if count < 1 {
		return 0, 0, 0, fmt.Errorf("ayah count must be positive")
	}

	totalStarts := 0
	for _, ayahCount := range surahAyahCounts {
		if ayahCount >= count {
			totalStarts += ayahCount - count + 1
		}
	}
	if totalStarts == 0 {
		return 0, 0, 0, fmt.Errorf("ayah count must not exceed %d", maxAyahsPerSurah())
	}

	n, err := rand.Int(rand.Reader, big.NewInt(int64(totalStarts)))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("select random ayah range: %w", err)
	}

	remaining := int(n.Int64())
	for index, ayahCount := range surahAyahCounts {
		starts := ayahCount - count + 1
		if starts <= 0 {
			continue
		}
		if remaining < starts {
			return index + 1, remaining + 1, remaining + count, nil
		}
		remaining -= starts
	}
	return 0, 0, 0, fmt.Errorf("select random ayah range: invalid range index")
}

func maxAyahsPerSurah() int {
	max := 0
	for _, count := range surahAyahCounts {
		if count > max {
			max = count
		}
	}
	return max
}
