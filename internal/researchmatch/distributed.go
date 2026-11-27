package researchmatch

// Distributed discovery extends full-range research matching. Channels and
// bands are pooled nuisance views, not independent statistical witnesses.
// Nothing in this file certifies a relationship or supplies fine timing.
import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"sort"

	"opensync/internal/dsp"
)

const DistributedVersion = "distributed-spectral-v1"
const featureMagic = "OSDTC001"
const featureHop = 80
const featureRate = 8000

var distributedBands = [][2]float64{{100, 350}, {350, 700}, {700, 1200}, {1200, 2200}, {2200, 3500}}

// WriteDistributed consumes interleaved little-endian float32 PCM at 8 kHz.
// Only one STFT window is buffered. Feature files are streamed and can be read
// in bounded reference tiles, regardless of the original recording length.
func WriteDistributed(pcm io.Reader, output io.Writer, channels int) error {
	if channels < 1 || channels > 8 {
		return fmt.Errorf("distributed features require 1..8 preserved channels")
	}
	w := bufio.NewWriter(output)
	if _, err := w.WriteString(featureMagic); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(channels)); err != nil {
		return err
	}
	window := make([]float64, 512*channels)
	raw := make([]byte, 512*channels*4)
	read := func(start, count int) error {
		if _, err := io.ReadFull(pcm, raw[:count*channels*4]); err != nil {
			return err
		}
		for i := 0; i < count*channels; i++ {
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:])))
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("nonfinite PCM")
			}
			window[start*channels+i] = v
		}
		return nil
	}
	if err := read(0, 512); err != nil {
		return fmt.Errorf("insufficient PCM: %w", err)
	}
	previous := make([]float64, channels*len(distributedBands))
	fft := make([]complex128, 512)
	first := true
	for {
		for ch := 0; ch < channels; ch++ {
			for i := range fft {
				fft[i] = complex(window[i*channels+ch]*(.5-.5*math.Cos(2*math.Pi*float64(i)/512)), 0)
			}
			if err := dsp.FFT(fft); err != nil {
				return err
			}
			for b, band := range distributedBands {
				sum := 0.
				count := 0
				for k := int(math.Ceil(band[0] * 512 / featureRate)); float64(k)*featureRate/512 < band[1]; k++ {
					sum += (real(fft[k])*real(fft[k]) + imag(fft[k])*imag(fft[k])) / (256 * 256)
					count++
				}
				energy := math.Log1p(sum / float64(count) * 1e5)
				index := ch*len(distributedBands) + b
				delta := energy - previous[index]
				if first {
					delta = 0
				}
				previous[index] = energy
				if err := binary.Write(w, binary.LittleEndian, float32(delta)); err != nil {
					return err
				}
			}
		}
		first = false
		copy(window, window[featureHop*channels:])
		if err := read(512-featureHop, featureHop); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return err
		}
	}
	return w.Flush()
}

type DistributedFile struct {
	file             io.ReaderAt
	closer           io.Closer
	Channels, Frames int
}

func OpenDistributed(path string) (*DistributedFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fail := func(e error) (*DistributedFile, error) { f.Close(); return nil, e }
	var header [12]byte
	if _, err = io.ReadFull(f, header[:]); err != nil {
		return fail(err)
	}
	c := int(binary.LittleEndian.Uint32(header[8:]))
	if string(header[:8]) != featureMagic || c < 1 || c > 8 {
		return fail(fmt.Errorf("invalid distributed feature header"))
	}
	st, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	stride := int64(4 * c * len(distributedBands))
	n := st.Size() - 12
	if n <= 0 || n%stride != 0 {
		return fail(fmt.Errorf("invalid distributed feature length"))
	}
	return &DistributedFile{file: f, closer: f, Channels: c, Frames: int(n / stride)}, nil
}

// OpenDistributedReader allows the same search to operate on run-local memory.
func OpenDistributedReader(r io.ReaderAt, size int64) (*DistributedFile, error) {
	var header [12]byte
	if _, err := r.ReadAt(header[:], 0); err != nil {
		return nil, err
	}
	c := int(binary.LittleEndian.Uint32(header[8:]))
	if string(header[:8]) != featureMagic || c < 1 || c > 8 {
		return nil, fmt.Errorf("invalid distributed feature header")
	}
	stride := int64(4 * c * len(distributedBands))
	n := size - 12
	if n <= 0 || n%stride != 0 {
		return nil, fmt.Errorf("invalid distributed feature length")
	}
	return &DistributedFile{file: r, Channels: c, Frames: int(n / stride)}, nil
}
func (f *DistributedFile) Close() error {
	if f.closer != nil {
		return f.closer.Close()
	}
	return nil
}
func (f *DistributedFile) Read(first, count int) ([][]float64, error) {
	if first < 0 || count < 1 || first+count > f.Frames || count > MaxDistributedQueryFrames+distributedReferenceTile {
		return nil, fmt.Errorf("feature window outside bounded range")
	}
	n := f.Channels * len(distributedBands)
	raw := make([]byte, count*n*4)
	if _, err := f.file.ReadAt(raw, int64(12+first*n*4)); err != nil {
		return nil, err
	}
	values := make([][]float64, n)
	for c := range values {
		values[c] = make([]float64, count)
	}
	for i := 0; i < count; i++ {
		for c := range values {
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[(i*n+c)*4:])))
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return nil, fmt.Errorf("nonfinite feature")
			}
			values[c][i] = v
		}
	}
	return values, nil
}

type DistributedSection struct {
	First int `json:"first_frame"`
	Count int `json:"frame_count"`
}
type DistributedPeak struct {
	Offset      float64   `json:"offset"`
	Score       float64   `json:"score"`
	Sections    []float64 `json:"section_scores"`
	Available   int       `json:"available_sections"`
	BackgroundZ float64   `json:"background_z"`
}
type DistributedResult struct {
	Version         string               `json:"version"`
	Status          string               `json:"status"`
	GridSeconds     float64              `json:"grid_seconds"`
	Sections        []DistributedSection `json:"sections"`
	Peaks           []DistributedPeak    `json:"candidates"`
	OnsetPeaks      []DistributedPeak    `json:"onset_candidates,omitempty"`
	OffsetsExamined int                  `json:"offsets_examined"`
	Certification   string               `json:"certification"`
}

// Sections adapt to the amount of available query material. Their receptive
// fields are separated by at least 100ms; short queries keep four smaller
// sections instead of silently demanding an eight-second verification window.
func DistributedSections(frames int) []DistributedSection {
	guard := 10
	n := 4
	if frames < 120 {
		return nil
	}
	if frames > 2400 {
		n = 8
	}
	width := min(300, (frames-(n+1)*guard)/n)
	if width < 15 {
		return nil
	}
	result := make([]DistributedSection, n)
	for i := range result {
		first := guard + i*(frames-2*guard-width)/(n-1)
		result[i] = DistributedSection{first, width}
	}
	return result
}

// Valid Pearson correlation, with centering of both local inputs. Silence is
// zero evidence, and roundoff cannot create an arbitrarily large score.
func distributedCorrelation(r, q []float64) ([]float64, error) {
	if len(q) > len(r) {
		return nil, nil
	}
	mean := 0.
	for _, v := range q {
		mean += v
	}
	mean /= float64(len(q))
	centered := make([]float64, len(q))
	energy := 0.
	for i, v := range q {
		centered[i] = v - mean
		energy += centered[i] * centered[i]
	}
	result := make([]float64, len(r)-len(q)+1)
	if energy < 1e-14 {
		return result, nil
	}
	dots, err := crossDots(r, centered)
	if err != nil {
		return nil, err
	}
	s, ss := prefix(r)
	n := len(q)
	for i := range result {
		variance := ss[i+n] - ss[i] - (s[i+n]-s[i])*(s[i+n]-s[i])/float64(n)
		if variance > 1e-14 {
			result[i] = math.Max(-1, math.Min(1, dots[i+n-1]/math.Sqrt(variance*energy)))
		}
	}
	return result, nil
}

// SearchDistributed scans all reference positions in 120-second tiles. It
// preserves alternatives and partial overlaps. Scores are ranking evidence,
// never p-values: views and adjacent offsets remain correlated.
func SearchDistributed(ref, query *DistributedFile, omitBand, omitSection int) (DistributedResult, error) {
	return SearchDistributedScheduled(ref, query, omitBand, omitSection, func(n int, work func(int)) {
		for i := 0; i < n; i++ {
			work(i)
		}
	})
}

// SearchDistributedScheduled splits the reference into independent buckets while
// retaining the original tile order for statistics and peak suppression.
// schedule must complete its leaf tasks before returning.
const MaxDistributedQueryFrames = 30000
const distributedReferenceTile = 12000

func SearchDistributedScheduled(ref, query *DistributedFile, omitBand, omitSection int, schedule func(int, func(int))) (DistributedResult, error) {
	return searchDistributed(ref, query, omitBand, omitSection, schedule, false, nil)
}

// SearchDistributedOnsets ranks offsets using three sections. The two
// intervening sections are reserved for evaluating the proposed offset.
func SearchDistributedOnsets(ref, query *DistributedFile, schedule func(int, func(int))) (DistributedResult, error) {
	return searchDistributed(ref, query, -1, -1, schedule, true, nil)
}

// OffsetSpan is a half-open range on the 100 Hz offset grid.
type OffsetSpan struct {
	First int `json:"first_frame"`
	End   int `json:"end_frame_exclusive"`
}

// SearchDistributedExcluding runs the ordinary search over unoccupied offsets.
// Pass exclusions in ascending start order.
func SearchDistributedExcluding(ref, query *DistributedFile, schedule func(int, func(int)), onsets bool, excluded []OffsetSpan) (DistributedResult, error) {
	return searchDistributed(ref, query, -1, -1, schedule, onsets, excluded)
}

func distributedTiles(ref, query int, excluded []OffsetSpan) []OffsetSpan {
	var tiles []OffsetSpan
	appendRange := func(first, end int) {
		for first < end {
			last := min(end, first+distributedReferenceTile)
			tiles = append(tiles, OffsetSpan{first, last})
			first = last
		}
	}
	first := -query
	for _, span := range excluded {
		end := min(span.First, ref)
		appendRange(first, end)
		first = max(first, span.End)
		if first >= ref {
			return tiles
		}
	}
	appendRange(first, ref)
	return tiles
}

const onsetLag = 24

func integrateOnsets(channels [][]float64) {
	for _, band := range channels {
		sum := 0.
		for i := range band {
			sum += band[i]
			if i >= onsetLag {
				band[i-onsetLag], sum = sum, sum-band[i-onsetLag]
			}
		}
		for i := len(band) - 1; i >= onsetLag; i-- {
			band[i] = band[i-onsetLag]
		}
		clear(band[:min(len(band), onsetLag)])
	}
}

func searchDistributed(ref, query *DistributedFile, omitBand, omitSection int, schedule func(int, func(int)), onsets bool, excluded []OffsetSpan) (DistributedResult, error) {
	result := DistributedResult{Version: DistributedVersion, Status: "DISCOVERY_ONLY", GridSeconds: .01, Certification: "not_assessed", Peaks: []DistributedPeak{}}
	if query.Frames > MaxDistributedQueryFrames {
		return result, fmt.Errorf("distributed short-query stage is bounded to 300 seconds")
	}
	q, err := query.Read(0, query.Frames)
	if err != nil {
		return result, err
	}
	sections := DistributedSections(query.Frames)
	if onsets {
		if query.Frames < 800 {
			return result, nil
		}
		integrateOnsets(q)
		const guard = onsetLag + 8
		width := min(350, (query.Frames-6*guard)/5)
		sections = make([]DistributedSection, 5)
		for i := range sections {
			sections[i] = DistributedSection{guard + i*(query.Frames-2*guard-width)/4, width}
		}
	}
	result.Sections = sections
	if len(sections) < 3 {
		return result, nil
	}
	var candidates []DistributedPeak
	backgroundCount, backgroundMean, backgroundM2 := 0, 0., 0.
	type tileResult struct {
		peaks      []DistributedPeak
		background []float64
		examined   int
	}
	tiles := distributedTiles(ref.Frames, query.Frames, excluded)
	tileCount := len(tiles)
	for batch := 0; batch < tileCount; batch += 32 {
		count := min(32, tileCount-batch)
		outputs := make([]tileResult, count)
		errors := make([]error, count)
		compute := func(index int) error {
			span := tiles[batch+index]
			base, last := span.First, span.End
			output := &outputs[index]
			rstart := max(0, base)
			if onsets {
				rstart = max(0, rstart-onsetLag)
			}
			rend := min(ref.Frames, last+query.Frames)
			if rend-rstart < 20 {
				return nil
			}
			r, err := ref.Read(rstart, rend-rstart)
			if err != nil {
				return err
			}
			if onsets {
				integrateOnsets(r)
			}
			curves := make([][]float64, len(sections))
			available := make([][]bool, len(sections))
			for si, section := range sections {
				curves[si] = make([]float64, last-base)
				available[si] = make([]bool, last-base)
				if si == omitSection {
					continue
				}
				bands := make([][]float64, 0, len(distributedBands))
				for b := range distributedBands {
					if b == omitBand || rend-rstart < section.Count {
						continue
					}
					pooled := make([]float64, rend-rstart-section.Count+1)
					// Cross-channel pooling also tolerates channel swaps; no channel is
					// permitted to contribute a separate corroboration vote.
					for rc := 0; rc < ref.Channels; rc++ {
						for qc := 0; qc < query.Channels; qc++ {
							corr, err := distributedCorrelation(r[rc*5+b], q[qc*5+b][section.First:section.First+section.Count])
							if err != nil {
								return err
							}
							for i, v := range corr {
								pooled[i] = math.Max(pooled[i], v)
							}
						}
					}
					bands = append(bands, pooled)
				}
				if len(bands) < 2 {
					continue
				}
				values := make([]float64, len(bands))
				for i := range curves[si] {
					index := base + i + section.First - rstart
					if index < 0 || index >= len(bands[0]) || (onsets && index < onsetLag) {
						continue
					}
					available[si][i] = true
					for b := range bands {
						values[b] = bands[b][index]
					}
					sort.Float64s(values)
					// Fixed 50th percentile rather than changing the selected order
					// statistic when ablation or unavailable bands changes the count.
					n := len(values)
					curves[si][i] = (values[(n-1)/2] + values[n/2]) / 2
				}
			}
			values := make([]float64, 0, len(sections))
			all := make([]float64, last-base)
			for i := range all {
				values = values[:0]
				for si := range sections {
					if available[si][i] && (!onsets || si%2 == 0) {
						values = append(values, curves[si][i])
					}
				}
				if len(values) < max(2, (len(sections)+1)/2) || (onsets && (!available[1][i] || !available[3][i])) {
					continue
				}
				sort.Float64s(values)
				keep := max(2, (3*len(values)+3)/4)
				score := 0.
				for _, v := range values[len(values)-keep:] {
					score += v
				}
				score /= float64(keep)
				expected := len(sections)
				if onsets {
					expected = 3
					score = (values[1] + values[2]) / 2
				}
				if omitSection >= 0 {
					expected--
				}
				score *= math.Sqrt(float64(len(values)) / float64(expected))
				all[i] = score
				output.examined++
				if i%10 == 0 {
					output.background = append(output.background, score)
				}
			}
			// Keep enough local maxima for global nonmaximum suppression, including
			// tile boundaries; there is no search-region hint from a graph or label.
			for i, score := range all {
				if score <= 0 || (i > 0 && all[i-1] >= score) || (i+1 < len(all) && all[i+1] > score) {
					continue
				}
				scores := make([]float64, len(sections))
				count := 0
				for si := range sections {
					scores[si] = curves[si][i]
					if available[si][i] {
						count++
					}
				}
				output.peaks = append(output.peaks, DistributedPeak{Offset: float64(base+i) * .01, Score: score, Sections: scores, Available: count})
			}
			// Bound candidate memory even for multi-hour references.

			return nil
		}
		schedule(count, func(i int) { errors[i] = compute(i) })
		for i, output := range outputs {
			if errors[i] != nil {
				return result, errors[i]
			}
			result.OffsetsExamined += output.examined
			for _, score := range output.background {
				backgroundCount++
				delta := score - backgroundMean
				backgroundMean += delta / float64(backgroundCount)
				backgroundM2 += delta * (score - backgroundMean)
			}
			candidates = append(candidates, output.peaks...)
			candidates = distributedTop(candidates, 32)
		}
	}
	sd := math.Sqrt(backgroundM2 / math.Max(1, float64(backgroundCount)))
	result.Peaks = distributedTop(candidates, 12)
	for i := range result.Peaks {
		result.Peaks[i].BackgroundZ = (result.Peaks[i].Score - backgroundMean) / math.Max(sd, .001)
	}
	return result, nil
}

// DistinctOnsetPeak requires a useful blind rank margin and agreement in
// both reserved sections at the same offset. It supplies discovery evidence.
func DistinctOnsetPeak(peaks []DistributedPeak) (DistributedPeak, bool) {
	if len(peaks) < 2 {
		return DistributedPeak{}, false
	}
	top := peaks[0]
	runner := peaks[1]
	if len(top.Sections) != 5 || top.Available != 5 || top.Score < .42 ||
		top.BackgroundZ < 6 || top.Score < 1.10*runner.Score ||
		min(top.Sections[1], top.Sections[3]) < .18 ||
		(top.Sections[1]+top.Sections[3])/2 < .28 {
		return DistributedPeak{}, false
	}
	if len(runner.Sections) == 5 && (top.Sections[1]+top.Sections[3]-runner.Sections[1]-runner.Sections[3])/2 < .08 {
		return DistributedPeak{}, false
	}
	return top, true
}
func distributedTop(candidates []DistributedPeak, count int) []DistributedPeak {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		return candidates[i].Offset < candidates[j].Offset
	})
	selected := make([]DistributedPeak, 0, count)
	for _, c := range candidates {
		distinct := true
		for _, p := range selected {
			if math.Abs(c.Offset-p.Offset) < .75 {
				distinct = false
				break
			}
		}
		if distinct {
			selected = append(selected, c)
		}
		if len(selected) == count {
			break
		}
	}
	return selected
}
