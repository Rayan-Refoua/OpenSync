package autosync

import (
	"encoding/json"
	"testing"
)

func TestFullResolutionFingerprintParity(t *testing.T) {
	pcm := make([]int16, 5*16000)
	wave := [...]int16{0, 900, 4100, 7100, 16000, 11500, 3200, 300, 0, -300, -3200, -11500, -16000, -7100, -4100, -900}
	for i := range pcm {
		pcm[i] = wave[(i/4)%len(wave)]
	}
	// Full-spectrum outputs captured before pruning the maximum-filter work.
	for _, fixture := range []struct {
		name string
		pcm  []int16
		want string
	}{
		{"broadband", fixtureNoise(12, 9714), "97f690a3736a351ff6719a5798b0d058659879c6f18e9c2dee1be5c510f445f8"},
		{"periodic with numerical ties", pcm, "129128f221c0c6d620809f2630f5a0b0bfa363c819a1b6a8519dd8790f8b5115"},
		{"silence", make([]int16, 16000), "7d99eada377f3d6daf4b894d5ce5c65eec7d43225083dcdd7aa8f0ffc485bc2d"},
		{"shorter than one window", []int16{1, 0}, "7d99eada377f3d6daf4b894d5ce5c65eec7d43225083dcdd7aa8f0ffc485bc2d"},
	} {
		primary, alternate, metrics := fingerprintVariants(fixture.pcm, 19.2)
		encoded, err := json.Marshal([]any{primary, alternate, metrics.TiedPeaks})
		if err != nil {
			t.Fatal(err)
		}
		if got := shaBytes(encoded); got != fixture.want {
			t.Errorf("%s fingerprint digest = %s, want %s", fixture.name, got, fixture.want)
		}
	}
}

func BenchmarkFullResolutionFingerprint(b *testing.B) {
	pcm := fixtureNoise(50, 494)
	b.ReportAllocs()
	b.SetBytes(int64(len(pcm) * 2))
	for i := 0; i < b.N; i++ {
		fingerprintVariants(pcm, 0)
	}
}
