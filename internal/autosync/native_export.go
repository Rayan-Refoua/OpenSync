package autosync

// Native FCP7 export. Rate canonicalization and collision-free lane allocation
// were reviewed against align's premiere.rs; this implementation is original
// OpenSync code and uses OpenSync's native project/evidence model.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type xnode struct {
	Name     string
	Text     string
	Attrs    []xml.Attr
	Children []*xnode
}

func xn(name string, value any) *xnode {
	n := &xnode{Name: name}
	if value != nil {
		n.Text = fmt.Sprint(value)
	}
	return n
}
func (n *xnode) add(name string, value any) *xnode {
	c := xn(name, value)
	n.Children = append(n.Children, c)
	return c
}
func (n *xnode) attr(k, v string) *xnode {
	n.Attrs = append(n.Attrs, xml.Attr{Name: xml.Name{Local: k}, Value: v})
	return n
}
func (n *xnode) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	start.Name.Local = n.Name
	start.Attr = n.Attrs
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	if n.Text != "" {
		if err := e.EncodeToken(xml.CharData(n.Text)); err != nil {
			return err
		}
	}
	for _, c := range n.Children {
		if err := e.Encode(c); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}
func rateXML(n *xnode, r FrameRate) {
	v := n.add("rate", nil)
	v.add("ntsc", strings.ToUpper(strconv.FormatBool(r.NTSC)))
	v.add("timebase", r.Base)
}
func mediaURL(path string) (string, error) {
	p := strings.ReplaceAll(path, "\\", "/")
	if strings.HasPrefix(p, "//") {
		parts := strings.SplitN(strings.TrimPrefix(p, "//"), "/", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid network source")
		}
		return (&url.URL{Scheme: "file", Host: parts[0], Path: "/" + parts[1]}).String(), nil
	}
	if len(p) > 2 && p[1] == ':' {
		return (&url.URL{Scheme: "file", Host: "localhost", Path: "/" + p}).String(), nil
	}
	a, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return (&url.URL{Scheme: "file", Host: "localhost", Path: filepath.ToSlash(a)}).String(), nil
}
func sourceFrames(r MediaRecord, rate FrameRate) int64 {
	if n := int64(number(r.Video["nb_frames"])); n > 0 {
		return n
	}
	return roundFrame(r.Duration * rate.FPS())
}
func channelCount(r MediaRecord) int { return int(number(r.Audio["channels"])) }
func channelType(n int) string {
	if n == 2 {
		return "stereo"
	}
	if n > 2 {
		return "multichannel"
	}
	return "mono"
}

type NativePlacement struct {
	Disabled             bool    `json:"disabled,omitempty"`
	LayoutBasis          string  `json:"layout_basis,omitempty"`
	PlacementUncertainty float64 `json:"placement_uncertainty_seconds,omitempty"`
	TimingBasis          string  `json:"timing_basis,omitempty"`
	Recording            int     `json:"recording"`
	ID                   string  `json:"id"`
	Status               string  `json:"status"`
	Component            int     `json:"component"`
	HasVideo             bool    `json:"has_video"`
	Start                int64   `json:"start"`
	End                  int64   `json:"end"`
	In                   int64   `json:"source_in"`
	Out                  int64   `json:"source_out"`
	Rate                 string  `json:"source_fps"`
	Segment              int     `json:"segment"`
	Segments             int     `json:"segments"`
	Sequence             string  `json:"sequence"`
	Error                float64 `json:"presentation_error_seconds"`
	Derivative           *string `json:"derivative"`
	DerivativeDuration   float64 `json:"derivative_duration_seconds,omitempty"`
	rate                 FrameRate
}
type NativeExportOptions struct {
	AudioEarlyMS, AudioLateMS float64
	Output, Unsynced          string
	MaxResidualMS             float64
	IncludeProvisional        bool
}

func validUnmatchedPolicy(policy string) bool {
	switch policy {
	case "append", "separate-sequence", "order_only", "disabled", "remove":
		return true
	}
	return false
}

func NativeExportCommand(args []string) error {
	// Match the public positional-project interface, followed by options.
	if len(args) == 0 {
		return fmt.Errorf("export requires a saved .opensync project")
	}
	project := args[0]
	f := flag.NewFlagSet("export", flag.ContinueOnError)
	o := NativeExportOptions{}
	f.StringVar(&o.Output, "output", filepath.Dir(project), "output directory")
	f.StringVar(&o.Unsynced, "unsynced", "append", "append, separate-sequence, order_only, disabled, or remove")
	f.Float64Var(&o.MaxResidualMS, "max-residual-ms", 150, "maximum presentation residual")
	f.BoolVar(&o.IncludeProvisional, "include-provisional", false, "include marked comparison candidates")
	f.Float64Var(&o.AudioEarlyMS, "audio-early-ms", 50, "allowed audio lead in milliseconds")
	f.Float64Var(&o.AudioLateMS, "audio-late-ms", 70, "allowed audio lag in milliseconds")
	if err := f.Parse(args[1:]); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return fmt.Errorf("unexpected export arguments")
	}
	return ExportNative(project, o)
}
func nativePlacements(p *NativeProject, o NativeExportOptions) ([]NativePlacement, FrameRate, error) {
	if !validUnmatchedPolicy(o.Unsynced) {
		return nil, FrameRate{}, fmt.Errorf("invalid unresolved policy")
	}
	if o.MaxResidualMS < 1 || o.MaxResidualMS > 150 {
		return nil, FrameRate{}, fmt.Errorf("residual limit must be 1..150ms")
	}
	early, late := o.AudioEarlyMS, o.AudioLateMS
	if early == 0 {
		early = 50
	}
	if late == 0 {
		late = 70
	}
	for _, v := range []float64{early, late} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return nil, FrameRate{}, fmt.Errorf("invalid audio drift tolerance")
		}
	}
	counts := map[FrameRate]int{}
	for _, r := range p.Data.Records {
		if r.Video != nil {
			rate, err := recordRate(r, FrameRate{50, false})
			if err != nil {
				return nil, FrameRate{}, err
			}
			counts[rate]++
		}
	}
	seq := FrameRate{50, false}
	best := 0
	for rate, n := range counts {
		if n > best || (n == best && rate.FPS() > seq.FPS()) {
			seq = rate
			best = n
		}
	}
	models := append([]TimeModel(nil), p.Data.Models...)
	if o.IncludeProvisional {
		if list, ok := p.Data.Graph["provisional_placements"].([]any); ok {
			for _, raw := range list {
				a, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				idx := int(number(a["recording"]))
				for j, m := range models {
					if m.Recording != idx || !strings.HasPrefix(m.Status, "unresolved") {
						continue
					}
					m.Status = stringValue(a["status"])
					m.Offset = number(a["start_seconds"]) - m.AudioOrigin
					m.Rate = 1
					m.Error = number(a["fit_residual_seconds"])
					m.PlacementUncertainty = number(a["placement_uncertainty_seconds"])
					m.TimingBasis = stringValue(a["timing_basis"])
					m.Component = 1
					m.Resolved = true
					m.Knots = []TimePoint{{0, m.Offset}, {p.Data.Records[idx].Duration, m.Offset + p.Data.Records[idx].Duration}}
					models[j] = m
				}
			}
		}
	}
	var result []NativePlacement
	for _, m := range models {
		r := p.Data.Records[m.Recording]
		rate, err := recordRate(r, seq)
		if err != nil {
			return nil, seq, err
		}
		total := sourceFrames(r, rate)
		duration := float64(total) / rate.FPS()
		points := m.Knots
		if len(points) == 0 {
			points = []TimePoint{{0, m.Offset}, {duration, m.Offset + m.Rate*duration}}
		}
		lo, hi := MapTime(points, 0), MapTime(points, duration)-duration
		if lo > hi {
			lo, hi = hi, lo
		}
		for _, v := range points {
			if v[0] > 0 && v[0] < duration {
				lo = math.Min(lo, v[1]-v[0])
				hi = math.Max(hi, v[1]-v[0])
			}
		}
		budget := o.MaxResidualMS/1000 - m.Error - .5/seq.FPS()
		resolved := !strings.HasPrefix(m.Status, "unresolved")
		if resolved && budget <= 0 {
			m.Status = "unresolved_timing_uncertainty"
			resolved = false
		}
		parts := []FrameSegment{{0, total, (lo + hi) / 2, (hi - lo) / 2}}
		if resolved && r.Video != nil {
			parts, err = SegmentVideo(points, total, rate.FPS(), budget)
			if err != nil {
				return nil, seq, fmt.Errorf("%s: %w", r.Name, err)
			}
		} else if resolved && r.Audio != nil && !strings.HasPrefix(m.Status, "provisional") && (MapTime(points, 0)-lo > late/1000+1e-9 || hi-MapTime(points, 0) > early/1000+1e-9) {
			if budget < .03 {
				m.Status = "unresolved_timing_uncertainty"
			} else {
				keyData, _ := json.Marshal([]any{r.ID, r.SourceBytes, r.SourceMtime, r.CacheSHA, points})
				target := strings.TrimSuffix(r.Source, filepath.Ext(r.Source)) + "_" + shaBytes(keyData)[:12] + "_driftCorrection.wav"
				if _, err := os.Stat(target); os.IsNotExist(err) {
					if err = renderMappedAudio(context.Background(), r, points, target); err != nil {
						return nil, seq, err
					}
				}
				info, err := probeNative(context.Background(), target)
				if err != nil {
					return nil, seq, err
				}
				format, _ := info["format"].(map[string]any)
				actual := number(format["duration"])
				if math.Abs(actual-(MapTime(points, r.Duration)-MapTime(points, 0))) > .001 {
					return nil, seq, fmt.Errorf("audio derivative duration mismatch")
				}
				start := roundFrame(MapTime(points, 0) * seq.FPS())
				result = append(result, NativePlacement{PlacementUncertainty: m.PlacementUncertainty, TimingBasis: m.TimingBasis, Recording: m.Recording, ID: fmt.Sprintf("recording-%s-part-1", r.ID), Status: m.Status, Component: m.Component, Start: start, End: start + roundFrame(actual*seq.FPS()), In: 0, Out: roundFrame(actual * rate.FPS()), Rate: rate.String(), Segment: 1, Segments: 1, Error: .03 + m.Error + .5/seq.FPS(), Derivative: &target, DerivativeDuration: actual, rate: rate})
				continue
			}
		}
		if resolved && r.Video == nil && m.Status != "unresolved_timing_uncertainty" {
			origin := MapTime(points, 0)
			parts[0].Offset = origin
			parts[0].Error = math.Max(origin-lo, hi-origin)
		}
		for j, v := range parts {
			start := roundFrame((float64(v.First)/rate.FPS() + v.Offset) * seq.FPS())
			end := start + roundFrame(float64(v.Last-v.First)/rate.FPS()*seq.FPS())
			result = append(result, NativePlacement{PlacementUncertainty: m.PlacementUncertainty, TimingBasis: m.TimingBasis, Recording: m.Recording, ID: fmt.Sprintf("recording-%s-part-%d", r.ID, j+1), Status: m.Status, Component: m.Component, HasVideo: r.Video != nil, Start: start, End: end, In: v.First, Out: v.Last, Rate: rate.String(), Segment: j + 1, Segments: len(parts), Error: v.Error + m.Error + .5/seq.FPS(), rate: rate})
		}
	}
	move := func(indices []int, start int64, sequence string) int64 {
		if len(indices) == 0 {
			return start
		}
		first := result[indices[0]].Start
		for _, i := range indices {
			first = min(first, result[i].Start)
		}
		last := start
		for _, i := range indices {
			result[i].Start += start - first
			result[i].End += start - first
			result[i].Sequence = sequence
			last = max(last, result[i].End)
		}
		return last
	}
	if o.Unsynced == "remove" {
		kept := result[:0]
		for _, v := range result {
			if !strings.HasPrefix(v.Status, "unresolved") {
				kept = append(kept, v)
			}
		}
		result = kept
	}
	var synced []int
	groups := map[int][]int{}
	for i, v := range result {
		if strings.HasPrefix(v.Status, "unresolved") {
			groups[v.Component] = append(groups[v.Component], i)
		} else {
			synced = append(synced, i)
		}
	}
	end := move(synced, 0, "synced")
	cursor := int64(0)
	unseq := "unresolved"
	if o.Unsynced != "separate-sequence" {
		unseq = "synced"
		if len(synced) > 0 && len(groups) > 0 {
			cursor = end + roundFrame(10*seq.FPS())
		}
	}
	components := make([]int, 0, len(groups))
	for c := range groups {
		components = append(components, c)
	}
	sort.Slice(components, func(i, j int) bool {
		a, b := groups[components[i]], groups[components[j]]
		av, bv := false, false
		ai, bi := len(result), len(result)
		for _, k := range a {
			av = av || result[k].HasVideo
			ai = min(ai, result[k].Recording)
		}
		for _, k := range b {
			bv = bv || result[k].HasVideo
			bi = min(bi, result[k].Recording)
		}
		if av != bv {
			return av
		}
		return ai < bi
	})
	if o.Unsynced == "order_only" {
		components = orderUnmatchedComponents(p.Data.Records, result, components)
	}
	for _, c := range components {
		for _, i := range groups[c] {
			result[i].Disabled = o.Unsynced == "disabled"
			if o.Unsynced == "order_only" {
				result[i].LayoutBasis = "ordered_without_reliable_timing"
			}
		}
		cursor = move(groups[c], cursor, unseq) + roundFrame(seq.FPS())
	}
	return result, seq, nil
}

type nativeInstance struct {
	ID, Kind              string
	Channel, Track, Index int
	Placement             int
}

func addLinks(n *xnode, instances []*nativeInstance) {
	for _, i := range instances {
		l := n.add("link", nil)
		l.add("linkclipref", i.ID)
		l.add("mediatype", i.Kind)
		l.add("trackindex", i.Track)
		l.add("clipindex", i.Index)
		if i.Kind == "audio" {
			l.add("groupindex", 1)
		}
	}
}
func addTrack(parent *xnode, kind string, channels, ch int) *xnode {
	n := parent.add("track", nil)
	if kind == "audio" {
		n.attr("premiereTrackType", channelType(channels))
		n.attr("currentExplodedTrackIndex", strconv.Itoa(ch))
		n.attr("totalExplodedTrackCount", strconv.Itoa(channels))
	}
	return n
}
func addFile(n *xnode, r MediaRecord, rate FrameRate, defined map[string]bool) error {
	id := "file-" + r.ID
	f := n.add("file", nil).attr("id", id)
	if defined[id] {
		return nil
	}
	defined[id] = true
	f.add("name", r.Name)
	rateXML(f, rate)
	f.add("duration", sourceFrames(r, rate))
	tc := f.add("timecode", nil)
	tr := rate
	value := "00:00:00:00"
	frame := 0.
	format := "NDF"
	if r.SourceTimecode != nil {
		var err error
		tr, err = canonicalRate(stringValue(r.SourceTimecode["rate"]))
		if err != nil {
			return err
		}
		value = stringValue(r.SourceTimecode["string"])
		frame = number(r.SourceTimecode["frame"])
		format = stringValue(r.SourceTimecode["displayformat"])
	}
	rateXML(tc, tr)
	tc.add("string", value)
	tc.add("frame", int64(frame))
	tc.add("displayformat", format)
	media := f.add("media", nil)
	if r.Video != nil {
		v := media.add("video", nil)
		v.add("duration", sourceFrames(r, rate))
		s := v.add("samplecharacteristics", nil)
		rateXML(s, rate)
		s.add("width", int(number(r.Video["width"])))
		s.add("height", int(number(r.Video["height"])))
		s.add("anamorphic", "FALSE")
		s.add("pixelaspectratio", "square")
		s.add("fielddominance", "none")
	}
	if count := channelCount(r); count > 0 {
		a := media.add("audio", nil)
		a.add("channelcount", count)
		a.add("duration", roundFrame(r.Duration*rate.FPS()))
		s := a.add("samplecharacteristics", nil)
		depth := int(number(r.Audio["bits_per_sample"]))
		if depth <= 0 {
			depth = 16
		}
		s.add("depth", depth)
		s.add("samplerate", int(number(r.Audio["sample_rate"])))
		a.add("layout", channelType(count))
		for c := 1; c <= count; c++ {
			a.add("audiochannel", nil).add("sourcechannel", c)
		}
	}
	path, err := mediaURL(r.Source)
	if err != nil {
		return err
	}
	f.add("pathurl", path)
	return nil
}
func buildNativeXML(p *NativeProject, placements []NativePlacement, seqRate FrameRate) ([]byte, error) {
	root := xn("xmeml", nil).attr("version", "4")
	project := root.add("project", nil)
	project.add("name", p.Data.Name)
	children := project.add("children", nil)
	defined := map[string]bool{}
	binRecords := append([]MediaRecord(nil), p.Data.Records...)
	for _, v := range placements {
		if v.Derivative != nil {
			binRecords = append(binRecords, renderedPlacementRecord(p.Data.Records[v.Recording], v))
		}
	}
	groupKeys := map[string]bool{}
	recordGroup := make([]string, len(binRecords))
	for i, r := range binRecords {
		kind := "Audio"
		if r.Video != nil {
			kind = "Camera"
		}
		recordGroup[i] = kind + "|" + filepath.Dir(r.Source)
		if i >= len(p.Data.Records) {
			recordGroup[i] = "Timing-corrected Audio|"
		}
		groupKeys[recordGroup[i]] = true
	}
	keys := make([]string, 0, len(groupKeys))
	for k := range groupKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	bins := map[string]*xnode{}
	counts := map[string]int{}
	for _, k := range keys {
		kind := strings.SplitN(k, "|", 2)[0]
		counts[kind]++
		b := children.add("bin", nil)
		b.add("name", fmt.Sprintf("%s %d", kind, counts[kind]))
		bins[k] = b.add("children", nil)
	}
	for i, r := range binRecords {
		rate, err := recordRate(r, seqRate)
		if err != nil {
			return nil, err
		}
		id := "master-" + r.ID
		clip := bins[recordGroup[i]].add("clip", nil).attr("id", id).attr("explodedTracks", "TRUE")
		clip.add("name", r.Name)
		clip.add("duration", sourceFrames(r, rate))
		rateXML(clip, rate)
		clip.add("masterclipid", id)
		clip.add("ismasterclip", "TRUE")
		media := clip.add("media", nil)
		var instances []*nativeInstance
		if r.Video != nil {
			instances = append(instances, &nativeInstance{ID: id + "-video-0", Kind: "video", Track: 1, Index: 1})
		}
		for ch := 0; ch < channelCount(r); ch++ {
			instances = append(instances, &nativeInstance{ID: fmt.Sprintf("%s-audio-%d", id, ch), Kind: "audio", Channel: ch, Track: ch + 1, Index: 1})
		}
		for _, kind := range []string{"video", "audio"} {
			var section *xnode
			for _, instance := range instances {
				if instance.Kind != kind {
					continue
				}
				if section == nil {
					section = media.add(kind, nil)
				}
				tr := addTrack(section, kind, channelCount(r), instance.Channel)
				c := tr.add("clipitem", nil).attr("id", instance.ID)
				c.add("masterclipid", id)
				if err = addFile(c, r, rate, defined); err != nil {
					return nil, err
				}
				s := c.add("sourcetrack", nil)
				s.add("mediatype", kind)
				s.add("trackindex", instance.Channel+1)
				addLinks(c, instances)
				tr.add("enabled", "TRUE")
				tr.add("locked", "FALSE")
			}
		}
	}
	for _, seqName := range []string{"synced", "unresolved"} {
		var indices []int
		end := int64(0)
		for i, v := range placements {
			if v.Sequence == seqName {
				indices = append(indices, i)
				end = max(end, v.End)
			}
		}
		if len(indices) == 0 && seqName == "unresolved" {
			continue
		}
		s := children.add("sequence", nil).attr("id", "opensync-"+seqName)
		name := "Synced Sequence"
		if seqName == "unresolved" {
			name = "Unresolved Clips"
		}
		s.add("name", name)
		s.add("duration", end)
		rateXML(s, seqRate)
		media := s.add("media", nil)
		sections := map[string]*xnode{"video": media.add("video", nil), "audio": media.add("audio", nil)}
		sample := sections["video"].add("format", nil).add("samplecharacteristics", nil)
		width, height := 1920, 1080
		for _, r := range p.Data.Records {
			if r.Video != nil {
				width = int(number(r.Video["width"]))
				height = int(number(r.Video["height"]))
				break
			}
		}
		sample.add("width", width)
		sample.add("height", height)
		sample.add("anamorphic", "FALSE")
		sample.add("pixelaspectratio", "square")
		rateXML(sample, seqRate)
		sample = sections["audio"].add("format", nil).add("samplecharacteristics", nil)
		sample.add("depth", 24)
		sample.add("samplerate", 48000)
		sort.SliceStable(indices, func(i, j int) bool {
			a, b := placements[indices[i]], placements[indices[j]]
			if a.Start != b.Start {
				return a.Start < b.Start
			}
			if a.Recording != b.Recording {
				return a.Recording < b.Recording
			}
			return a.Segment < b.Segment
		})
		lanes := map[string][][]*nativeInstance{}
		byPlacement := map[int][]*nativeInstance{}
		for _, i := range indices {
			v := placements[i]
			r := p.Data.Records[v.Recording]
			var entries []*nativeInstance
			if r.Video != nil {
				entries = append(entries, &nativeInstance{ID: v.ID + "-video-0", Kind: "video", Placement: i})
			}
			for ch := 0; ch < channelCount(r); ch++ {
				entries = append(entries, &nativeInstance{ID: fmt.Sprintf("%s-audio-%d", v.ID, ch), Kind: "audio", Channel: ch, Placement: i})
			}
			byPlacement[i] = entries
			for _, in := range entries {
				k := fmt.Sprintf("%s|%s|%04d|%04d", in.Kind, recordGroup[v.Recording], in.Channel, channelCount(r))
				choice := -1
				for j, lane := range lanes[k] {
					if placements[lane[len(lane)-1].Placement].End <= v.Start {
						choice = j
						break
					}
				}
				if choice < 0 {
					lanes[k] = append(lanes[k], nil)
					choice = len(lanes[k]) - 1
				}
				lanes[k][choice] = append(lanes[k][choice], in)
			}
		}
		laneKeys := make([]string, 0, len(lanes))
		for k := range lanes {
			laneKeys = append(laneKeys, k)
		}
		sort.Strings(laneKeys)
		trackCounts := map[string]int{}
		type renderedLane struct {
			node  *xnode
			items []*nativeInstance
		}
		var rendered []renderedLane
		for _, k := range laneKeys {
			for _, lane := range lanes[k] {
				kind := lane[0].Kind
				trackCounts[kind]++
				r := p.Data.Records[placements[lane[0].Placement].Recording]
				tr := addTrack(sections[kind], kind, channelCount(r), lane[0].Channel)
				for j, in := range lane {
					in.Track = trackCounts[kind]
					in.Index = j + 1
				}
				rendered = append(rendered, renderedLane{tr, lane})
			}
		}
		for _, lane := range rendered {
			for _, in := range lane.items {
				v := placements[in.Placement]
				r := p.Data.Records[v.Recording]
				r = renderedPlacementRecord(r, v)
				c := lane.node.add("clipitem", nil).attr("id", in.ID).attr("premiereChannelType", channelType(channelCount(r)))
				name := r.Name
				if strings.HasPrefix(v.Status, "provisional") {
					name += " [REVIEW]"
				}
				if v.Segments > 1 {
					name += fmt.Sprintf(" [part %d/%d]", v.Segment, v.Segments)
				}
				c.add("name", name)
				c.add("duration", sourceFrames(r, v.rate))
				rateXML(c, v.rate)
				c.add("in", v.In)
				c.add("out", v.Out)
				if v.Disabled {
					c.add("enabled", "FALSE")
				}
				c.add("start", v.Start)
				c.add("end", v.End)
				c.add("masterclipid", "master-"+r.ID)
				if err := addFile(c, r, v.rate, defined); err != nil {
					return nil, err
				}
				st := c.add("sourcetrack", nil)
				st.add("mediatype", in.Kind)
				st.add("trackindex", in.Channel+1)
				addLinks(c, byPlacement[in.Placement])
				label := "Caribbean"
				description := "OpenSync acoustic and graph checks passed; production review pending"
				if strings.HasPrefix(v.Status, "unresolved") {
					label = "Rose"
					description = "Unresolved relative to the main sequence"
				} else if strings.HasPrefix(v.Status, "provisional") {
					label = "Mango"
					description = "Provisional acoustic placement for comparison, not certified"
				}
				if v.Status == "provisional_spectral_consensus_review_required" {
					description = "Spectral comparison placement; waveform timing is unverified"
				}
				if v.PlacementUncertainty > 0 {
					description = fmt.Sprintf("Chronological estimate (%s), uncertainty +/- %.3f s; not acoustically verified", v.TimingBasis, v.PlacementUncertainty)
				}
				c.add("labels", nil).add("label2", label)
				c.add("logginginfo", nil).add("description", description)
			}
			lane.node.add("enabled", "TRUE")
			lane.node.add("locked", "FALSE")
		}
		unresolvedStart := int64(-1)
		for _, i := range indices {
			v := placements[i]
			if strings.HasPrefix(v.Status, "unresolved") && (unresolvedStart < 0 || v.Start < unresolvedStart) {
				unresolvedStart = v.Start
			}
			if strings.HasPrefix(v.Status, "provisional") {
				marker := s.add("marker", nil)
				marker.add("name", "REVIEW: "+p.Data.Records[v.Recording].Name)
				marker.add("in", v.Start)
				marker.add("out", v.End)
			}
		}
		if unresolvedStart >= 0 {
			marker := s.add("marker", nil)
			marker.add("name", "UNRESOLVED CLIPS — NOT SYNCHRONIZED")
			marker.add("in", unresolvedStart)
			marker.add("out", -1)
		}
	}
	var b bytes.Buffer
	b.WriteString(xml.Header + "<!DOCTYPE xmeml>\n")
	e := xml.NewEncoder(&b)
	e.Indent("", "  ")
	if err := e.Encode(root); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
func ExportNative(projectPath string, o NativeExportOptions) error {
	p, err := LoadNativeProject(projectPath)
	if err != nil {
		return err
	}
	for _, r := range p.Data.Records {
		st, e := os.Stat(r.Source)
		if e != nil {
			return e
		}
		if r.SourceBytes > 0 && (st.Size() != r.SourceBytes || st.ModTime().UnixNano() != r.SourceMtime) {
			return fmt.Errorf("source changed: %s", r.Source)
		}
	}
	placements, seq, err := nativePlacements(p, o)
	if err != nil {
		return err
	}
	data, err := buildNativeXML(p, placements, seq)
	if err != nil {
		return err
	}
	if err = ValidateNativeExport(data, p.Data.Records, placements, seq, o.Unsynced); err != nil {
		return err
	}
	out, err := filepath.Abs(o.Output)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return err
	}
	path := filepath.Join(out, "OpenSync.xml")
	if err = atomicWrite(path, data); err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	var unresolved, provisional []string
	for _, r := range p.Data.Records {
		u, v := false, false
		for _, x := range placements {
			if p.Data.Records[x.Recording].ID == r.ID {
				u = u || strings.HasPrefix(x.Status, "unresolved")
				v = v || strings.HasPrefix(x.Status, "provisional")
			}
		}
		if u {
			unresolved = append(unresolved, r.Name)
		}
		if v {
			provisional = append(provisional, r.Name)
		}
	}
	omitted := []string{}
	if o.Unsynced == "remove" {
		present := map[int]bool{}
		for _, v := range placements {
			present[v.Recording] = true
		}
		for i, r := range p.Data.Records {
			if !present[i] {
				omitted = append(omitted, r.ID)
			}
		}
	}
	derivatives := []map[string]any{}
	for _, v := range placements {
		if v.Derivative != nil {
			digest, err := hashFile(*v.Derivative)
			if err != nil {
				return err
			}
			r := p.Data.Records[v.Recording]
			derivatives = append(derivatives, map[string]any{"recording_id": r.ID, "original": r.Source, "derivative": *v.Derivative, "sha256": digest, "actual_duration_seconds": v.DerivativeDuration})
		}
	}
	manifest := map[string]any{"status": "EXPORTED_REQUIRES_PREMIERE_AND_ACCURACY_REVIEW", "project": projectPath, "project_content_sha256": p.Hash, "xml": path, "xml_sha256": hex.EncodeToString(sum[:]), "recordings": len(p.Data.Records), "sequence_fps": seq.String(), "unsynced_policy": o.Unsynced, "include_provisional": o.IncludeProvisional, "placements": placements, "omitted_recording_ids": omitted, "derivatives": derivatives, "unresolved_recordings": unresolved, "provisional_recordings": provisional, "source_media_modified": false, "engine": "native_go"}
	if err = writeJSON(filepath.Join(out, "timeline-manifest.json"), manifest); err != nil {
		return err
	}
	if err = writeJSON(filepath.Join(out, "structural-audit.json"), map[string]any{"status": "PASS_STRUCTURAL_ONLY", "recordings": len(p.Data.Records), "source_frames_accounted": true, "rate_arithmetic_matches_xml": true, "premiere_import_test": "not_performed"}); err != nil {
		return err
	}
	fmt.Printf("PROJECT %s\nEXPORT %s\nRECORDINGS %d\nUNRESOLVED %d\n", projectPath, path, len(p.Data.Records), len(unresolved))
	return nil
}
