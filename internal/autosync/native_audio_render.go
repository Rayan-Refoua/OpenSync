package autosync

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
)

func renderMappedAudio(ctx context.Context, r MediaRecord, points []TimePoint, target string) error {
	if r.Video != nil {
		return fmt.Errorf("audio correction cannot retime video")
	}
	if err := ValidateMapping(points); err != nil {
		return err
	}
	sr := int(number(r.Audio["sample_rate"]))
	if sr <= 0 {
		sr = 48000
	}
	channels := channelCount(r)
	if channels < 1 {
		return fmt.Errorf("no audio channels")
	}
	origin := MapTime(points, 0)
	inverse := InverseMapping(points)
	total := roundFrame((MapTime(points, r.Duration) - origin) * float64(sr))
	boundSet := map[int64]bool{0: true, total: true}
	for _, p := range points {
		if p[0] > 0 && p[0] < r.Duration {
			boundSet[roundFrame((p[1]-origin)*float64(sr))] = true
		}
	}
	bounds := []int64{}
	for b := range boundSet {
		bounds = append(bounds, b)
	}
	sort.Slice(bounds, func(i, j int) bool { return bounds[i] < bounds[j] })
	if len(bounds) < 2 || bounds[0] != 0 || bounds[len(bounds)-1] != total {
		return fmt.Errorf("invalid audio intervals")
	}
	fade := roundFrame(.005 * float64(sr))
	for i := 1; i < len(bounds); i++ {
		if bounds[i] <= bounds[i-1] {
			return fmt.Errorf("invalid audio intervals")
		}
		fade = min(fade, (bounds[i]-bounds[i-1])/4)
	}
	source := r.LocalSource
	if st, err := os.Stat(source); err != nil || st.IsDir() {
		source = r.Source
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(target), "render-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	assembled := filepath.Join(dir, "assembled.s32")
	output, err := os.Create(assembled)
	if err != nil {
		return err
	}
	defer output.Close()
	var tail []byte
	frameBytes := int64(channels * 4)
	for i := 0; i < len(bounds)-1; i++ {
		left, right := bounds[i], bounds[i+1]
		if i > 0 {
			left -= fade
		}
		if i < len(bounds)-2 {
			right += fade
		}
		sourceFirst := math.Max(0, MapTime(inverse, origin+float64(left)/float64(sr)))
		sourceLast := math.Min(r.Duration, MapTime(inverse, origin+float64(right)/float64(sr)))
		count := right - left
		tempo := (sourceLast - sourceFirst) / (float64(count) / float64(sr))
		if tempo < .5 || tempo > 2 {
			return fmt.Errorf("unsupported audio timing rate")
		}
		part := filepath.Join(dir, "part.s32")
		filters := fmt.Sprintf("atempo=%.14f,apad,atrim=end_sample=%d", tempo, count)
		if err = runMediaTool(ctx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-threads", "1", "-ss", fmt.Sprintf("%.12f", sourceFirst), "-t", fmt.Sprintf("%.12f", sourceLast-sourceFirst), "-i", source, "-map", "0:a:0", "-af", filters, "-ar", fmt.Sprint(sr), "-ac", fmt.Sprint(channels), "-c:a", "pcm_s32le", "-f", "s32le", part); err != nil {
			return err
		}
		st, err := os.Stat(part)
		if err != nil {
			return err
		}
		if st.Size() != count*frameBytes {
			return fmt.Errorf("audio segment duration mismatch")
		}
		input, err := os.Open(part)
		if err != nil {
			return err
		}
		overlap := int64(0)
		if i > 0 {
			overlap = 2 * fade
		}
		if overlap > 0 {
			head := make([]byte, overlap*frameBytes)
			if _, err = io.ReadFull(input, head); err != nil {
				input.Close()
				return err
			}
			if len(tail) != len(head) {
				input.Close()
				return fmt.Errorf("audio overlap size mismatch")
			}
			for frame := int64(0); frame < overlap; frame++ {
				weight := float64(frame) / float64(overlap-1)
				for ch := 0; ch < channels; ch++ {
					j := frame*frameBytes + int64(ch*4)
					a := float64(int32(binary.LittleEndian.Uint32(tail[j:])))
					b := float64(int32(binary.LittleEndian.Uint32(head[j:])))
					value := math.RoundToEven(a*(1-weight) + b*weight)
					value = math.Max(-2147483648, math.Min(2147483647, value))
					binary.LittleEndian.PutUint32(head[j:], uint32(int32(value)))
				}
			}
			if _, err = output.Write(head); err != nil {
				input.Close()
				return err
			}
		}
		keep := int64(0)
		if i < len(bounds)-2 {
			keep = 2 * fade
		}
		remaining := count - overlap - keep
		if remaining < 0 {
			input.Close()
			return fmt.Errorf("invalid audio join")
		}
		if _, err = io.CopyN(output, input, remaining*frameBytes); err != nil {
			input.Close()
			return err
		}
		tail = make([]byte, keep*frameBytes)
		_, err = io.ReadFull(input, tail)
		input.Close()
		if err != nil {
			return err
		}
	}
	if err = output.Close(); err != nil {
		return err
	}
	st, err := os.Stat(assembled)
	if err != nil {
		return err
	}
	if st.Size() != total*frameBytes {
		return fmt.Errorf("audio join changed total duration")
	}
	partial := target + ".partial.wav"
	if err = runMediaTool(ctx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-f", "s32le", "-ar", fmt.Sprint(sr), "-ac", fmt.Sprint(channels), "-i", assembled, "-c:a", "pcm_s24le", "-rf64", "auto", partial); err != nil {
		return err
	}
	return os.Rename(partial, target)
}
func renderedPlacementRecord(r MediaRecord, p NativePlacement) MediaRecord {
	if p.Derivative == nil {
		return r
	}
	r.ID += "-corrected"
	r.Source = *p.Derivative
	r.Name = filepath.Base(r.Source)
	r.Duration = p.DerivativeDuration
	r.Video = nil
	r.SourceTimecode = nil
	audio := map[string]any{}
	for k, v := range r.Audio {
		audio[k] = v
	}
	audio["bits_per_sample"] = 24
	r.Audio = audio
	return r
}
