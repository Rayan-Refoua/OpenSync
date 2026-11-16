package autosync

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

func (n *xnode) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	n.Name = start.Name.Local
	n.Attrs = start.Attr
	for {
		t, err := d.Token()
		if err != nil {
			return err
		}
		switch v := t.(type) {
		case xml.StartElement:
			c := new(xnode)
			if err = d.DecodeElement(c, &v); err != nil {
				return err
			}
			n.Children = append(n.Children, c)
		case xml.CharData:
			n.Text += string(v)
		case xml.EndElement:
			return nil
		}
	}
}
func (n *xnode) get(path string) *xnode {
	if n == nil {
		return nil
	}
	parts := strings.SplitN(path, "/", 2)
	for _, c := range n.Children {
		if c.Name == parts[0] {
			if len(parts) == 1 {
				return c
			}
			return c.get(parts[1])
		}
	}
	return nil
}
func (n *xnode) text(path string) string {
	c := n.get(path)
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Text)
}
func (n *xnode) attribute(name string) string {
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}
func (n *xnode) all(name string) []*xnode {
	var result []*xnode
	for _, c := range n.Children {
		if c.Name == name {
			result = append(result, c)
		}
		result = append(result, c.all(name)...)
	}
	return result
}
func xmlInt(n *xnode, name string) (int64, error) {
	v, err := strconv.ParseInt(n.text(name), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s %s: invalid %s", n.Name, n.attribute("id"), name)
	}
	return v, nil
}
func xmlRate(n *xnode) (FrameRate, error) {
	base, err := xmlInt(n, "rate/timebase")
	if err != nil {
		return FrameRate{}, err
	}
	if base < 1 || base > 1000 {
		return FrameRate{}, fmt.Errorf("invalid XML timebase")
	}
	ntsc := n.text("rate/ntsc")
	if ntsc != "TRUE" && ntsc != "FALSE" {
		return FrameRate{}, fmt.Errorf("invalid XML NTSC flag")
	}
	return FrameRate{int(base), ntsc == "TRUE"}, nil
}
func ValidateNativeExport(data []byte, records []MediaRecord, placements []NativePlacement, seqRate FrameRate, policy string) error {
	var root xnode
	d := xml.NewDecoder(bytes.NewReader(data))
	if err := d.Decode(&root); err != nil {
		return err
	}
	for {
		_, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if root.Name != "xmeml" {
		return fmt.Errorf("invalid FCP7 document")
	}
	files := map[string]*xnode{}
	for _, f := range root.all("file") {
		if f.get("pathurl") == nil {
			continue
		}
		id := f.attribute("id")
		if files[id] != nil {
			return fmt.Errorf("duplicate file definition %s", id)
		}
		files[id] = f
	}
	byID := map[string]NativePlacement{}
	for _, p := range placements {
		if _, ok := byID[p.ID]; ok {
			return fmt.Errorf("duplicate placement")
		}
		byID[p.ID] = p
	}
	type location struct {
		kind         string
		track, index int
		p            NativePlacement
		n            *xnode
	}
	locations := map[string]location{}
	masterIDs := map[string]bool{}
	masters := append([]MediaRecord(nil), records...)
	for _, p := range placements {
		if p.Derivative != nil {
			masters = append(masters, renderedPlacementRecord(records[p.Recording], p))
		}
	}
	for _, c := range root.all("clip") {
		masterIDs[c.attribute("id")] = true
		found := false
		for _, r := range masters {
			if c.attribute("id") != "master-"+r.ID {
				continue
			}
			found = true
			want, err := recordRate(r, seqRate)
			if err != nil {
				return err
			}
			got, err := xmlRate(c)
			if err != nil || got != want {
				return fmt.Errorf("master clip rate mismatch: %s", r.Name)
			}
			duration, err := xmlInt(c, "duration")
			if err != nil || duration != sourceFrames(r, want) {
				return fmt.Errorf("master source duration mismatch: %s", r.Name)
			}
		}
		if !found {
			return fmt.Errorf("unexpected master clip")
		}
	}
	if len(masterIDs) != len(masters) {
		return fmt.Errorf("master clip count mismatch")
	}
	seen := map[string]int{}
	for _, seq := range root.all("sequence") {
		rate, err := xmlRate(seq)
		if err != nil {
			return err
		}
		if rate != seqRate {
			return fmt.Errorf("sequence rate serialization mismatch")
		}
		sequence := strings.TrimPrefix(seq.attribute("id"), "opensync-")
		for _, kind := range []string{"video", "audio"} {
			section := seq.get("media/" + kind)
			if section == nil {
				continue
			}
			ti := 0
			for _, track := range section.Children {
				if track.Name != "track" {
					continue
				}
				ti++
				last := int64(0)
				ci := 0
				for _, c := range track.Children {
					if c.Name != "clipitem" {
						continue
					}
					ci++
					id := c.attribute("id")
					split := strings.LastIndex(id, "-"+kind+"-")
					if split < 0 {
						return fmt.Errorf("unknown timeline item %s", id)
					}
					p, ok := byID[id[:split]]
					if !ok {
						return fmt.Errorf("unplanned timeline item %s", id)
					}
					if _, ok = locations[id]; ok {
						return fmt.Errorf("duplicate timeline item %s", id)
					}
					r := records[p.Recording]
					r = renderedPlacementRecord(r, p)
					start, e := xmlInt(c, "start")
					if e != nil {
						return e
					}
					end, e := xmlInt(c, "end")
					if e != nil {
						return e
					}
					first, e := xmlInt(c, "in")
					if e != nil {
						return e
					}
					stop, e := xmlInt(c, "out")
					if e != nil {
						return e
					}
					sourceRate, e := xmlRate(c)
					if e != nil {
						return e
					}
					delta := float64(stop-first)/sourceRate.FPS() - float64(end-start)/seqRate.FPS()
					if start < last || end <= start || first < 0 || stop <= first || math.Abs(delta) > .5/seqRate.FPS()+1e-9 {
						return fmt.Errorf("invalid item %s (%s): timeline %d..%d at %s, source %d..%d at %s; duration difference %.9fs", id, r.Name, start, end, seqRate.String(), first, stop, sourceRate.String(), delta)
					}
					if start != p.Start || end != p.End || first != p.In || stop != p.Out || sourceRate != p.rate || sequence != p.Sequence {
						return fmt.Errorf("serialized placement differs for %s", r.Name)
					}
					if stop > sourceFrames(r, p.rate) {
						return fmt.Errorf("source boundary exceeded: %s", r.Name)
					}
					f := c.get("file")
					if f == nil {
						return fmt.Errorf("missing source reference")
					}
					definition := files[f.attribute("id")]
					if definition == nil {
						return fmt.Errorf("undefined source file")
					}
					want, e := mediaURL(r.Source)
					if e != nil {
						return e
					}
					if definition.text("pathurl") != want {
						return fmt.Errorf("source identity mismatch for %s", r.Name)
					}
					if c.text("masterclipid") != "master-"+r.ID || !masterIDs[c.text("masterclipid")] {
						return fmt.Errorf("invalid master link")
					}
					fr, e := xmlRate(definition)
					if e != nil || fr != sourceRate {
						return fmt.Errorf("file/source rate mismatch")
					}
					tc, e := xmlInt(definition, "timecode/frame")
					if e != nil {
						return e
					}
					if tc != int64(number(r.SourceTimecode["frame"])) {
						return fmt.Errorf("source timecode changed")
					}
					if (c.text("enabled") == "FALSE") != p.Disabled {
						return fmt.Errorf("clip enabled state differs from export placement")
					}
					channel, e := xmlInt(c, "sourcetrack/trackindex")
					if e != nil {
						return e
					}
					if c.text("sourcetrack/mediatype") != kind || channel < 1 || (kind == "video" && channel != 1) || (kind == "audio" && channel > int64(channelCount(r))) {
						return fmt.Errorf("invalid source channel")
					}
					locations[id] = location{kind, ti, ci, p, c}
					seen[p.ID]++
					last = end
				}
			}
		}
	}
	for _, loc := range locations {
		links := 0
		for _, l := range loc.n.Children {
			if l.Name != "link" {
				continue
			}
			links++
			other, ok := locations[l.text("linkclipref")]
			if !ok {
				return fmt.Errorf("undefined linked timeline item")
			}
			track, _ := xmlInt(l, "trackindex")
			index, _ := xmlInt(l, "clipindex")
			if other.kind != l.text("mediatype") || other.track != int(track) || other.index != int(index) || other.p.ID != loc.p.ID {
				return fmt.Errorf("incorrect link track or clip index")
			}
		}
		r := records[loc.p.Recording]
		expected := channelCount(r)
		if r.Video != nil {
			expected++
		}
		if links != expected {
			return fmt.Errorf("incomplete link set")
		}
	}
	for _, p := range placements {
		r := records[p.Recording]
		expected := channelCount(r)
		if r.Video != nil {
			expected++
		}
		if seen[p.ID] != expected {
			return fmt.Errorf("missing recording/channel %s", r.Name)
		}
	}
	for i, r := range records {
		var parts []NativePlacement
		for _, p := range placements {
			if p.Recording == i {
				parts = append(parts, p)
			}
		}
		if len(parts) == 0 {
			if policy == "remove" {
				continue
			}
			return fmt.Errorf("missing recording %s", r.Name)
		}
		sort.Slice(parts, func(i, j int) bool { return parts[i].In < parts[j].In })
		next := int64(0)
		for _, p := range parts {
			if p.In != next {
				return fmt.Errorf("missing or duplicated source frames %s", r.Name)
			}
			next = p.Out
		}
		if next != sourceFrames(renderedPlacementRecord(r, parts[0]), parts[0].rate) {
			return fmt.Errorf("incomplete source extent %s", r.Name)
		}
	}
	syncedEnd := int64(0)
	unStart := int64(math.MaxInt64)
	for _, p := range placements {
		if strings.HasPrefix(p.Status, "unresolved") {
			unStart = min(unStart, p.Start)
			if policy == "separate-sequence" && p.Sequence != "unresolved" {
				return fmt.Errorf("unresolved sequence policy violated")
			}
		} else {
			syncedEnd = max(syncedEnd, p.End)
		}
	}
	if (policy == "append" || policy == "disabled" || policy == "order_only" || policy == "local-tracks") && unStart < syncedEnd {
		return fmt.Errorf("unresolved section overlaps synchronized section")
	}
	return nil
}
