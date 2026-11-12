package autosync

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type TimePoint [2]float64

func ValidateMapping(p []TimePoint) error {
	if len(p) < 2 {
		return fmt.Errorf("timing mapping needs two points")
	}
	for i, v := range p {
		if math.IsNaN(v[0]) || math.IsNaN(v[1]) || math.IsInf(v[0], 0) || math.IsInf(v[1], 0) || (i > 0 && (v[0] <= p[i-1][0] || v[1] <= p[i-1][1])) {
			return fmt.Errorf("timing mapping is not finite and monotonic")
		}
	}
	return nil
}
func MapTime(p []TimePoint, t float64) float64 {
	i := sort.Search(len(p), func(i int) bool { return p[i][0] > t }) - 1
	i = max(0, min(len(p)-2, i))
	a, b := p[i], p[i+1]
	return a[1] + (t-a[0])*(b[1]-a[1])/(b[0]-a[0])
}
func InverseMapping(p []TimePoint) []TimePoint {
	r := make([]TimePoint, len(p))
	for i, v := range p {
		r[i] = TimePoint{v[1], v[0]}
	}
	return r
}
func ComposeMapping(parent, relation []TimePoint, duration float64) []TimePoint {
	times := map[float64]bool{0: true, duration: true}
	for _, p := range relation {
		if p[0] > 0 && p[0] < duration {
			times[p[0]] = true
		}
	}
	inv := InverseMapping(relation)
	for _, p := range parent {
		if p[0] > relation[0][1] && p[0] < relation[len(relation)-1][1] {
			t := MapTime(inv, p[0])
			if t > 0 && t < duration {
				times[t] = true
			}
		}
	}
	ts := make([]float64, 0, len(times))
	for t := range times {
		ts = append(ts, t)
	}
	sort.Float64s(ts)
	result := make([]TimePoint, len(ts))
	for i, t := range ts {
		result[i] = TimePoint{t, MapTime(parent, MapTime(relation, t))}
	}
	return result
}

type FrameSegment struct {
	First, Last   int64
	Offset, Error float64
}

func SegmentVideo(p []TimePoint, total int64, fps, budget float64) ([]FrameSegment, error) {
	if err := ValidateMapping(p); err != nil {
		return nil, err
	}
	if budget <= 0 {
		return nil, fmt.Errorf("no video error budget")
	}
	pending := [][2]int64{{0, total}}
	var result []FrameSegment
	for len(pending) > 0 {
		v := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		a, b := float64(v[0])/fps, float64(v[1])/fps
		lo, hi := MapTime(p, a)-a, MapTime(p, b)-b
		if lo > hi {
			lo, hi = hi, lo
		}
		for _, x := range p {
			if x[0] > a && x[0] < b {
				off := x[1] - x[0]
				lo = math.Min(lo, off)
				hi = math.Max(hi, off)
			}
		}
		err := (hi - lo) / 2
		if err <= budget {
			result = append(result, FrameSegment{v[0], v[1], (lo + hi) / 2, err})
		} else if v[1]-v[0] <= 1 {
			return nil, fmt.Errorf("timing change cannot be represented without video retiming")
		} else {
			mid := (v[0] + v[1]) / 2
			pending = append(pending, [2]int64{mid, v[1]}, [2]int64{v[0], mid})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].First < result[j].First })
	return result, nil
}

// FrameRate encodes exactly the rate used for frame arithmetic. A denominator
// of 1001 alone does NOT make a rate NTSC (30031/1001 is about 30.001).
type FrameRate struct {
	Base int
	NTSC bool
}

func (r FrameRate) FPS() float64 {
	if r.NTSC {
		return float64(r.Base) * 1000 / 1001
	}
	return float64(r.Base)
}
func (r FrameRate) String() string {
	if r.NTSC {
		return fmt.Sprintf("%d/1001", r.Base*1000)
	}
	return strconv.Itoa(r.Base)
}
func parseRatio(s string) float64 {
	p := strings.Split(s, "/")
	a, _ := strconv.ParseFloat(p[0], 64)
	if len(p) == 2 {
		b, _ := strconv.ParseFloat(p[1], 64)
		if b <= 0 {
			return 0
		}
		return a / b
	}
	return a
}
func canonicalRate(raw string) (FrameRate, error) {
	f := parseRatio(raw)
	if f <= 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return FrameRate{}, fmt.Errorf("invalid source frame rate %q", raw)
	}
	best := FrameRate{}
	distance := math.Inf(1)
	for _, base := range []int{1, 5, 10, 12, 15, 20, 24, 25, 30, 48, 50, 60, 90, 100, 120, 240} {
		for _, ntsc := range []bool{false, true} {
			if ntsc && base != 24 && base != 30 && base != 60 && base != 120 && base != 240 {
				continue
			}
			r := FrameRate{base, ntsc}
			d := math.Abs(f - r.FPS())
			if d < distance {
				distance = d
				best = r
			}
		}
	}
	if distance > .02 {
		return FrameRate{}, fmt.Errorf("frame rate %s requires explicit variable-rate handling", raw)
	}
	return best, nil
}
func recordRate(r MediaRecord, fallback FrameRate) (FrameRate, error) {
	if r.Video == nil {
		return fallback, nil
	}
	avg := stringValue(r.Video["avg_frame_rate"])
	if parseRatio(avg) <= 0 {
		avg = stringValue(r.Video["r_frame_rate"])
	}
	return canonicalRate(avg)
}
func roundFrame(x float64) int64 { return int64(math.RoundToEven(x)) }
