package main

import (
	"testing"

	"opensync/internal/researchmatch"
)

func TestResearchSupportConfigLegacyM22AndExplicitM24(t *testing.T) {
	cases := []struct {
		name                string
		hop, integration    float64
		wantHop, wantFrames int
		wantErr             bool
	}{
		{"legacy_m22_8ms", 8, 0, 128, 1, false},
		{"legacy_m22_2ms", 2, 0, 32, 1, false},
		{"m24_8ms", 8, 8, 128, 1, false},
		{"m24_2ms", 2, 2, 32, 1, false},
		{"m24_2ms_stabilized", 2, 8, 32, 4, false},
		{"unknown_8ms_integration", 8, 2, 0, 0, true},
		{"unknown_2ms_integration", 2, 16, 0, 0, true},
		{"unknown_hop", 4, 0, 0, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := researchSupportConfig(researchmatch.Result{
				HopMS: tc.hop, IntegrationMS: tc.integration,
			})
			if (err != nil) != tc.wantErr {
				t.Fatalf("error=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if cfg.HopSamples != tc.wantHop || cfg.IntegrationFrames != tc.wantFrames {
				t.Fatalf("unexpected configuration: %+v", cfg)
			}
		})
	}
}
