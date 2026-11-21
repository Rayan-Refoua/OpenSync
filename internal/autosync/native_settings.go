package autosync

import (
	"context"
	"fmt"
)

// AnalysisSettings contains user-selectable policies. Zero values preserve
// the normal automatic defaults for older projects and callers.
type AnalysisSettings struct {
	Recovery           string  `json:"recovery"`
	ReadMode           string  `json:"readMode"`
	Readers            string  `json:"readers"`
	LongReferenceHours float64 `json:"longReferenceHours"`
}

func (s *AnalysisSettings) Normalize() error {
	if s.Recovery == "" {
		s.Recovery = "balanced"
	}
	if s.ReadMode == "" {
		s.ReadMode = "auto"
	}
	if s.Readers == "" {
		s.Readers = "auto"
	}
	switch s.Recovery {
	case "off", "quick", "balanced", "deep":
	default:
		return fmt.Errorf("recovery must be off, quick, balanced, or deep")
	}
	switch s.ReadMode {
	case "auto", "buffered":
	default:
		return fmt.Errorf("read mode must be auto or buffered")
	}
	switch s.Readers {
	case "auto", "serial", "parallel":
	default:
		return fmt.Errorf("readers must be auto, serial, or parallel")
	}
	if !(s.LongReferenceHours >= 0 && s.LongReferenceHours <= 600) {
		return fmt.Errorf("long reference budget must be 0..600 hours")
	}
	return nil
}

type analysisSettingsKey struct{}

func analysisSettings(ctx context.Context) AnalysisSettings {
	s, _ := ctx.Value(analysisSettingsKey{}).(AnalysisSettings)
	_ = s.Normalize()
	return s
}
