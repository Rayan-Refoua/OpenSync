package autosync

import (
	"context"
	"fmt"
)

// AnalysisSettings contains user-selectable policies. Zero values preserve
// the normal automatic defaults for older projects and callers.
type VerificationSettings struct {
	MinWave          float64 `json:"minWave"`
	MinFlux          float64 `json:"minFlux"`
	ControlRatio     float64 `json:"controlRatio"`
	ControlMargin    float64 `json:"controlMargin"`
	AmbiguityRatio   float64 `json:"ambiguityRatio"`
	EventToleranceMS float64 `json:"eventToleranceMS"`
}

func (v *VerificationSettings) normalize() error {
	for _, f := range []struct {
		value              *float64
		name               string
		fallback, min, max float64
	}{
		{&v.MinWave, "min-wave", 0.1, 0.01, 1},
		{&v.MinFlux, "min-flux", 0.25, 0.01, 1},
		{&v.ControlRatio, "control-ratio", 1.5, 1, 10},
		{&v.ControlMargin, "control-margin", 0.035, 0.001, 1},
		{&v.AmbiguityRatio, "ambiguity-ratio", 0.8, 0.01, 1},
		{&v.EventToleranceMS, "event-tolerance-ms", 150, 1, 1000},
	} {
		if *f.value == 0 {
			*f.value = f.fallback
		}
		if !(*f.value >= f.min && *f.value <= f.max) {
			return fmt.Errorf("%s must be between %g and %g", f.name, f.min, f.max)
		}
	}
	return nil
}
func (v VerificationSettings) custom() bool {
	var defaults VerificationSettings
	_ = defaults.normalize()
	_ = v.normalize()
	return v != defaults
}

type AnalysisSettings struct {
	GuideRecorderOrder     bool                 `json:"guideRecorderOrder,omitempty"`
	GlobalTiming           bool                 `json:"globalTiming,omitempty"`
	DisableSearchShortcuts bool                 `json:"disableSearchShortcuts,omitempty"`
	Verification           VerificationSettings `json:"verification"`
	Recovery               string               `json:"recovery"`
	Discovery              string               `json:"discovery,omitempty"` // Legacy dispatch-only option.
	TimingProfiles         string               `json:"timingProfiles"`
	ReadMode               string               `json:"readMode"`
	Readers                string               `json:"readers"`
	LongReferenceHours     float64              `json:"longReferenceHours"`
	RecoveryReferenceHours float64              `json:"recoveryReferenceHours,omitempty"`
}

func (s *AnalysisSettings) Normalize() error {
	if s.Recovery == "" {
		s.Recovery = "balanced"
	}
	switch s.Discovery {
	case "", "full", "progressive":
		s.Discovery = "" // Both modes complete the same landmark search.
	default:
		return fmt.Errorf("discovery must be full or progressive")
	}
	if s.TimingProfiles == "" {
		s.TimingProfiles = "auto"
	}
	switch s.TimingProfiles {
	case "auto", "dense", "adaptive":
	default:
		return fmt.Errorf("timing profiles must be auto, dense, or adaptive")
	}
	if s.ReadMode == "" {
		s.ReadMode = "auto"
	}
	if s.Readers == "" {
		s.Readers = "auto"
	}
	switch s.Recovery {
	case "off", "quick", "balanced", "deep", "forensic":
	default:
		return fmt.Errorf("recovery must be off, quick, balanced, deep, or forensic")
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
	if !(s.RecoveryReferenceHours >= 0 && s.RecoveryReferenceHours <= 10000) {
		return fmt.Errorf("distributed recovery budget must be 0..10000 reference hours")
	}
	return s.Verification.normalize()
}

type analysisSettingsKey struct{}

func analysisSettings(ctx context.Context) AnalysisSettings {
	s, _ := ctx.Value(analysisSettingsKey{}).(AnalysisSettings)
	_ = s.Normalize()
	return s
}
