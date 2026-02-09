package batch

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Job struct {
	Surah      int    `yaml:"surah"`
	StartAyah  int    `yaml:"start_ayah"`
	EndAyah    int    `yaml:"end_ayah"`
	Mode       string `yaml:"mode"`
	OutputName string `yaml:"output_name"`
}

type Batch struct {
	Jobs []Job `yaml:"jobs"`
}

func Load(path string) (*Batch, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var b Batch
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}
