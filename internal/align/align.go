package align

import "time"

type WordTiming struct {
	Word  string
	Start time.Duration
	End   time.Duration
}

type Aligner interface {
	Align(audioPath string, words []string, language string) ([]WordTiming, error)
	Available() bool
}
