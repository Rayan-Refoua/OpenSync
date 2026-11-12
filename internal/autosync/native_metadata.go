package autosync

import (
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var timecodePattern = regexp.MustCompile(`^(\d{2}):(\d{2}):(\d{2})([:;])(\d{2})$`)

func sourceTC(h, m, s, f int, rate FrameRate, drop bool) (map[string]any, error) {
	nominal := rate.Base
	if h < 0 || h >= 24 || m < 0 || m >= 60 || s < 0 || s >= 60 || f < 0 || f >= nominal {
		return nil, fmt.Errorf("invalid source timecode")
	}
	skipped := 0
	format, sep := "NDF", ":"
	if drop {
		skipped = int(math.Round(float64(nominal) * .0666666666667))
		if (nominal != 30 && nominal != 60) || (s == 0 && m%10 != 0 && f < skipped) {
			return nil, fmt.Errorf("invalid drop-frame address")
		}
		format = "DF"
		sep = ";"
	}
	minutes := h*60 + m
	frame := ((h*60+m)*60+s)*nominal + f - skipped*(minutes-minutes/10)
	return map[string]any{"frame": frame, "string": fmt.Sprintf("%02d:%02d:%02d%s%02d", h, m, s, sep, f), "rate": rate.String(), "displayformat": format}, nil
}
func parseSourceTC(text string, rate FrameRate) (map[string]any, error) {
	m := timecodePattern.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return nil, fmt.Errorf("invalid timecode text")
	}
	h, _ := strconv.Atoi(m[1])
	mm, _ := strconv.Atoi(m[2])
	s, _ := strconv.Atoi(m[3])
	f, _ := strconv.Atoi(m[5])
	return sourceTC(h, mm, s, f, rate, m[4] == ";")
}
func sonyLTC(encoded string, tcFPS int, half bool, rate FrameRate) (map[string]any, error) {
	raw, err := hex.DecodeString(encoded)
	if err != nil || len(raw) != 4 {
		return nil, fmt.Errorf("invalid packed LTC")
	}
	values := make([]int, 4)
	for i, mask := range []byte{0x3f, 0x7f, 0x7f, 0x3f} {
		v := raw[i] & mask
		if v%16 > 9 || v/16 > 9 {
			return nil, fmt.Errorf("invalid BCD timecode")
		}
		values[i] = int(v/16)*10 + int(v%16)
	}
	mult := 1
	if half {
		mult = 2
	}
	if tcFPS*mult != rate.Base {
		return nil, fmt.Errorf("timecode/video rate disagreement")
	}
	f := values[0] * mult
	if half && raw[3]&0x80 != 0 {
		f++
	}
	return sourceTC(values[3], values[2], values[1], f, rate, raw[0]&0x40 != 0)
}
func readSonyTC(path string, rate FrameRate) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var root xnode
	if err = xml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	if root.Name != "NonRealTimeMeta" {
		return nil, nil
	}
	var table, duration, creation, video *xnode
	var walk func(*xnode)
	walk = func(n *xnode) {
		switch n.Name {
		case "LtcChangeTable":
			table = n
		case "Duration":
			duration = n
		case "CreationDate":
			creation = n
		case "VideoFrame":
			video = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(&root)
	if table == nil || duration == nil {
		return nil, nil
	}
	count, _ := strconv.Atoi(duration.attribute("value"))
	tcFPS, _ := strconv.Atoi(table.attribute("tcFps"))
	half := strings.EqualFold(table.attribute("halfStep"), "true")
	var first, last *xnode
	for _, n := range table.Children {
		if n.Name == "LtcChange" {
			frame, _ := strconv.Atoi(n.attribute("frameCount"))
			if frame == 0 {
				first = n
			}
			if frame == count-1 {
				last = n
			}
		}
	}
	if first == nil || last == nil {
		return nil, nil
	}
	start, err := sonyLTC(first.attribute("value"), tcFPS, half, rate)
	if err != nil {
		return nil, err
	}
	end, err := sonyLTC(last.attribute("value"), tcFPS, half, rate)
	if err != nil {
		return nil, err
	}
	day := rate.Base * 86400
	if start["displayformat"] == "DF" {
		day -= int(math.Round(float64(rate.Base)*.0666666666667)) * (1440 - 144)
	}
	delta := (int(number(end["frame"])) - int(number(start["frame"])) + day) % day
	if delta != count-1 {
		return nil, fmt.Errorf("camera timecode does not cover source frames")
	}
	if video != nil {
		declared := strings.TrimRight(video.attribute("formatFps"), "pi")
		if declared != "" {
			v, err := strconv.ParseFloat(declared, 64)
			if err != nil || math.Abs(v-rate.FPS()) > .02 {
				return nil, fmt.Errorf("sidecar frame rate mismatch")
			}
		}
	}
	start["kind"] = "sony_nrt_sidecar"
	start["source"] = path
	start["source_sha256"] = shaBytes(raw)
	start["source_frames"] = count
	start["end_frame_verified"] = true
	start["creation_time"] = nil
	if creation != nil {
		start["creation_time"] = creation.attribute("value")
	}
	return start, nil
}
func nativeSourceTimecode(r MediaRecord) map[string]any {
	rate, err := recordRate(r, FrameRate{50, false})
	if err != nil {
		return nil
	}
	for _, stream := range []map[string]any{r.Video, r.Audio} {
		tags, _ := stream["tags"].(map[string]any)
		if text := stringValue(tags["timecode"]); text != "" {
			if tc, err := parseSourceTC(text, rate); err == nil {
				tc["kind"] = "container_timecode"
				return tc
			}
		}
	}
	if r.Video == nil {
		return nil
	}
	base := strings.TrimSuffix(r.Source, filepath.Ext(r.Source))
	expected := int(number(r.Video["nb_frames"]))
	for _, path := range []string{base + "M01.XML", base + ".XML"} {
		if tc, err := readSonyTC(path, rate); err == nil && tc != nil && (expected == 0 || int(number(tc["source_frames"])) == expected) {
			return tc
		}
	}
	tags, _ := r.Video["tags"].(map[string]any)
	created, err := time.Parse(time.RFC3339Nano, stringValue(tags["creation_time"]))
	if err != nil || expected == 0 {
		return nil
	}
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(r.Source), "*M01.XML"))
	var matches []map[string]any
	for _, path := range paths {
		tc, err := readSonyTC(path, rate)
		if err != nil || tc == nil || int(number(tc["source_frames"])) != expected {
			continue
		}
		stamp, err := time.Parse(time.RFC3339Nano, stringValue(tc["creation_time"]))
		if err == nil && stamp.Equal(created) {
			matches = append(matches, tc)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return nil
}
