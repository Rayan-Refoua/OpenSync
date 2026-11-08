package premierexml

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

type Placement struct {
	ClipID        string `json:"clip_id"`
	Media         string `json:"media"`
	Track         int    `json:"track"`
	ExpectedStart int64  `json:"expected_start"`
	ExpectedEnd   int64  `json:"expected_end"`
	NewStart      int64  `json:"new_start"`
}

type Plan struct {
	Placements []Placement `json:"placements"`
}

type slot struct {
	ID, Name, Media     string
	Track               int
	Start, End, In, Out int64
}

type xmlClip struct {
	ID    string `xml:"id,attr"`
	Name  string `xml:"name"`
	Start string `xml:"start"`
	End   string `xml:"end"`
	In    string `xml:"in"`
	Out   string `xml:"out"`
}

func parseSlots(data []byte) (map[string]slot, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var stack []string
	var media string
	var track int
	counts := map[string]int{}
	slots := map[string]slot{}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			parent := strings.Join(stack, "/")
			if name == "track" &&
				(parent == "xmeml/project/children/sequence/media/video" ||
					parent == "xmeml/project/children/sequence/media/audio") {
				media = stack[len(stack)-1]
				counts[media]++
				track = counts[media]
			}
			if name == "clipitem" && track > 0 {
				var c xmlClip
				if err := dec.DecodeElement(&c, &t); err != nil {
					return nil, err
				}
				if c.ID == "" {
					return nil, fmt.Errorf("timeline clip without id")
				}
				if _, exists := slots[c.ID]; exists {
					return nil, fmt.Errorf("duplicate timeline clip id %q", c.ID)
				}
				numbers := make([]int64, 4)
				for i, value := range []string{c.Start, c.End, c.In, c.Out} {
					numbers[i], err = strconv.ParseInt(value, 10, 64)
					if err != nil {
						return nil, fmt.Errorf("clip %q: invalid timing: %w", c.ID, err)
					}
				}
				slots[c.ID] = slot{
					ID: c.ID, Name: c.Name, Media: media,
					Track: track, Start: numbers[0], End: numbers[1],
					In: numbers[2], Out: numbers[3],
				}
				continue
			}
			stack = append(stack, name)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected closing element")
			}
			if t.Name.Local == "track" && track > 0 {
				track = 0
				media = ""
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) != 0 || len(slots) == 0 {
		return nil, fmt.Errorf("invalid or empty timeline")
	}
	return slots, nil
}

var clipBlocks = regexp.MustCompile(`(?s)<clipitem\b[^>]*>.*?</clipitem>`)
var clipID = regexp.MustCompile(`\bid="([^"]+)"`)

func changeTag(block []byte, tag string, old, next int64) ([]byte, error) {
	from := []byte(fmt.Sprintf("<%s>%d</%s>", tag, old, tag))
	to := []byte(fmt.Sprintf("<%s>%d</%s>", tag, next, tag))
	if bytes.Count(block, from) != 1 {
		return nil, fmt.Errorf("missing or ambiguous %s tag", tag)
	}
	return bytes.Replace(block, from, to, 1), nil
}

// Patch updates only timeline start/end values. Other XML bytes,
// project bins, media definitions, clip links, and source in/out stay intact.
func Patch(source []byte, plan Plan) ([]byte, error) {
	if len(plan.Placements) == 0 {
		return nil, fmt.Errorf("empty placement plan")
	}
	original, err := parseSlots(source)
	if err != nil {
		return nil, err
	}

	updates := make(map[string]Placement)
	for _, p := range plan.Placements {
		s, found := original[p.ClipID]
		if !found || p.NewStart < 0 || p.ExpectedStart < 0 ||
			p.ExpectedEnd <= p.ExpectedStart ||
			s.Media != p.Media || s.Track != p.Track ||
			s.Start != p.ExpectedStart || s.End != p.ExpectedEnd {
			return nil, fmt.Errorf("unexpected clip placement: %q", p.ClipID)
		}
		if _, duplicate := updates[p.ClipID]; duplicate {
			return nil, fmt.Errorf("duplicate patch id %q", p.ClipID)
		}
		updates[p.ClipID] = p
	}

	// Check all track collisions against the prospective result.
	for _, a := range original {
		as, ae := a.Start, a.End
		if p, ok := updates[a.ID]; ok {
			as = p.NewStart
			ae = as + (a.End - a.Start)
		}
		for _, b := range original {
			if a.ID >= b.ID || a.Media != b.Media || a.Track != b.Track {
				continue
			}
			// Check only pairs affected by a placement change.
			_, affectedA := updates[a.ID]
			_, affectedB := updates[b.ID]
			if !affectedA && !affectedB {
				continue
			}
			bs, be := b.Start, b.End
			if p, ok := updates[b.ID]; ok {
				bs = p.NewStart
				be = bs + (b.End - b.Start)
			}
			if as < be && bs < ae {
				return nil, fmt.Errorf(
					"track collision: %s and %s", a.ID, b.ID)
			}
		}
	}

	matches := clipBlocks.FindAllIndex(source, -1)
	var out bytes.Buffer
	last := 0
	changed := map[string]bool{}

	for _, m := range matches {
		out.Write(source[last:m[0]])
		block := source[m[0]:m[1]]
		headEnd := bytes.IndexByte(block, '>')
		if headEnd < 0 {
			return nil, fmt.Errorf("malformed clip block")
		}
		identity := clipID.FindSubmatch(block[:headEnd+1])
		if len(identity) == 2 {
			id := string(identity[1])
			if p, ok := updates[id]; ok {
				if changed[id] {
					return nil, fmt.Errorf("duplicate clip block %q", id)
				}
				s := original[id]
				block, err = changeTag(block, "start", s.Start, p.NewStart)
				if err != nil {
					return nil, err
				}
				end := p.NewStart + (s.End - s.Start)
				block, err = changeTag(block, "end", s.End, end)
				if err != nil {
					return nil, err
				}
				changed[id] = true
			}
		}
		out.Write(block)
		last = m[1]
	}
	out.Write(source[last:])
	if len(changed) != len(updates) {
		return nil, fmt.Errorf("not all planned clips were patched")
	}

	result := out.Bytes()
	after, err := parseSlots(result)
	if err != nil {
		return nil, err
	}
	if len(after) != len(original) {
		return nil, fmt.Errorf("timeline clip count changed")
	}
	for id, before := range original {
		a, ok := after[id]
		if !ok {
			return nil, fmt.Errorf("timeline clip disappeared: %s", id)
		}
		if p, changed := updates[id]; changed {
			if a.Start != p.NewStart ||
				a.End != p.NewStart+(before.End-before.Start) {
				return nil, fmt.Errorf("placement validation failed: %s", id)
			}
			a.Start, a.End = before.Start, before.End
		}
		if a != before {
			return nil, fmt.Errorf("unplanned clip modification: %s", id)
		}
	}
	return append([]byte(nil), result...), nil
}
