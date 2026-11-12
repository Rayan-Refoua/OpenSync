package syntheticbench

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// ExtendAcceptance adds polarity, reversed direction, silence and sub-grid
// offsets. These fixtures never derive their labels from matcher output.
func ExtendAcceptance(dir string, seed int64, cases []Scenario) ([]Scenario, error) {
	master := synthMaster(seed)
	inverted := transform(slice(master, queryStart, querySeconds, 0), "clean", seed)
	for i := range inverted {
		inverted[i] = -inverted[i]
	}
	for name, samples := range map[string][]int16{
		"query-inverted.wav": inverted,
		"query-silence.wav":  make([]int16, querySeconds*rate),
		"query-subgrid.wav":  transform(slice(master, 8.3754375, querySeconds, 0), "mic", seed+1),
	} {
		if err := writeWAV(filepath.Join(dir, name), samples); err != nil {
			return nil, err
		}
	}
	return append(cases,
		Scenario{Name: "negative-offset", Reference: "query-clean.wav", Query: "reference.wav", ExpectedOffsetNS: targetOffset(queryStart, refStart), Purpose: "reverse recording direction"},
		Scenario{Name: "polarity-inverted", Reference: "reference.wav", Query: "query-inverted.wav", ExpectedOffsetNS: targetOffset(refStart, queryStart), Purpose: "microphone polarity reversal"},
		Scenario{Name: "subgrid-offset", Reference: "reference.wav", Query: "query-subgrid.wav", ExpectedOffsetNS: targetOffset(refStart, 8.3754375), Purpose: "fractional-hop offset, no grid-aligned truth"},
		Scenario{Name: "silence-negative", Reference: "reference.wav", Query: "query-silence.wav", Purpose: "silence must produce no discovery candidates"},
	), nil
}

// TimingFixture specifies an analytic mapping from query time to reference
// time. It tests local timing measurements, not whole-recording discovery.
type TimingFixture struct {
	Name            string  `json:"name"`
	Reference       string  `json:"reference"`
	Query           string  `json:"query"`
	DurationSeconds int     `json:"duration_seconds"`
	OffsetNS        int64   `json:"offset_ns"`
	DriftPPM        float64 `json:"drift_ppm"`
}

func noiseAt(i int64) float64 {
	x := uint64(i) + 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	x ^= x >> 31
	return float64(x>>11)/float64(uint64(1)<<53)*2 - 1
}

func proceduralSample(position float64) float64 {
	i := int64(math.Floor(position))
	f := position - float64(i)
	return (1-f)*noiseAt(i) + f*noiseAt(i+1)
}

// streamTiming writes bounded buffers even for multi-hour fixtures.
func streamTiming(path string, seconds int, start, ppm float64) (err error) {
	count := int64(seconds) * rate
	if count <= 0 || count*2 > int64(^uint32(0))-36 {
		return fmt.Errorf("invalid timing fixture duration")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if e := f.Close(); err == nil {
			err = e
		}
	}()
	header := make([]byte, 44)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+count*2))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], 1)
	binary.LittleEndian.PutUint32(header[24:], rate)
	binary.LittleEndian.PutUint32(header[28:], rate*2)
	binary.LittleEndian.PutUint16(header[32:], 2)
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(count*2))
	if _, err = f.Write(header); err != nil {
		return err
	}
	buf := make([]byte, 32768)
	for pos := int64(0); pos < count; {
		n := int64(len(buf) / 2)
		if n > count-pos {
			n = count - pos
		}
		for j := int64(0); j < n; j++ {
			v := proceduralSample(start*rate + float64(pos+j)*(1+ppm/1e6))
			binary.LittleEndian.PutUint16(buf[2*j:], uint16(int16(math.Round(v*16000))))
		}
		if _, err = f.Write(buf[:2*n]); err != nil {
			return err
		}
		pos += n
	}
	return nil
}

func GenerateTiming(dir string, seconds int) ([]TimingFixture, error) {
	if err := os.Mkdir(dir, 0700); err != nil {
		return nil, err
	}
	if err := streamTiming(filepath.Join(dir, "reference.wav"), seconds+12, 0, 0); err != nil {
		return nil, err
	}
	fixtures := []TimingFixture{}
	for _, ppm := range []float64{-250, 0, 250} {
		name := fmt.Sprintf("drift-%+.0fppm", ppm)
		file := name + ".wav"
		if err := streamTiming(filepath.Join(dir, file), seconds, 4.375, ppm); err != nil {
			return nil, err
		}
		fixtures = append(fixtures, TimingFixture{Name: name, Reference: "reference.wav", Query: file, DurationSeconds: seconds, OffsetNS: 4375000000, DriftPPM: ppm})
	}
	return fixtures, nil
}
