package autosync

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// SourceTrackGroup proposes a presentation group from metadata already collected
// during probing. Its key is not proof of device identity or timing.
func SourceTrackGroup(r MediaRecord) (key, label string) {
	if r.SourceAssignment != nil {
		return r.SourceAssignment.Group, r.SourceAssignment.Label
	}
	key, label, _ = detectSourceTrack(r)
	return
}

func detectSourceTrack(r MediaRecord) (key, label, basis string) {
	folder := filepath.Dir(r.Source)
	family := TrackFamily(r.Name)
	label = strings.TrimSuffix(family, filepath.Ext(family))
	kind := "audio"
	if r.Video != nil {
		kind = "video"
	}
	videoTags, _ := r.Video["tags"].(map[string]any)
	audioTags, _ := r.Audio["tags"].(map[string]any)
	tags := []map[string]any{r.FormatTags, videoTags, audioTags}
	makeName := sourceTag(tags, "make", "manufacturer", "com.apple.quicktime.make")
	model := sourceTag(tags, "model", "model_name", "com.apple.quicktime.model")
	encoder := sourceTag(tags, "encoder", "software")
	transcoded := strings.Contains(encoder, "handbrake") || strings.Contains(encoder, "lavf") ||
		strings.Contains(encoder, "ffmpeg") || strings.Contains(encoder, "media encoder")
	if !transcoded {
		serial := sourceTag(tags, "camera_serial_number", "device_serial_number", "serial_number", "serialnumber", "com.apple.quicktime.serialnumber")
		if serial != "" && (makeName != "" || model != "") {
			if model == "" {
				model = makeName
			}
			return sourceGroupKey("device", kind, makeName, model, serial), model, "device"
		}
	}
	if transcoded {
		if family == "" {
			return folder + "|" + sourceGroupKey("transcode", kind, folder, r.Name), "", "transcoded"
		}
		return folder + "|" + family + "|" + sourceGroupKey("transcode", kind, folder, family), label, "transcoded"
	}
	profile := []string{"profile", kind, folder, strings.ToLower(filepath.Ext(r.Name)), makeName, model, family}
	for _, tag := range []string{"major_brand", "compatible_brands", "com.android.version", "com.android.capture.fps"} {
		profile = append(profile, sourceTag(tags, tag))
	}
	profile = append(profile, streamProfile(r.Video, "codec_name", "codec_tag_string", "time_base", "r_frame_rate", "width", "height")...)
	for _, audio := range sourceAudioStreams(r) {
		profile = append(profile, streamProfile(audio, "codec_name", "codec_tag_string", "sample_rate", "channels", "channel_layout", "bits_per_sample", "bits_per_raw_sample")...)
	}
	if family != "" {
		return folder + "|" + family + "|" + sourceGroupKey(profile...), label, "filename_profile"
	}
	return folder + "|" + sourceGroupKey(profile...), model, "profile"
}

func streamProfile(stream map[string]any, fields ...string) []string {
	values := make([]string, 0, len(fields)+1)
	for _, field := range fields {
		raw, _ := json.Marshal(stream[field])
		values = append(values, string(raw))
	}
	tags, _ := stream["tags"].(map[string]any)
	return append(values, sourceTag([]map[string]any{tags}, "handler_name"))
}

func sourceAudioStreams(r MediaRecord) []map[string]any {
	if len(r.AudioStreams) != 0 {
		return r.AudioStreams
	}
	if r.Audio != nil {
		return []map[string]any{r.Audio}
	}
	return nil
}

func sourceGroupKey(parts ...string) string {
	return shaBytes([]byte(strings.Join(parts, "\x00")))
}

// Conflicting values for the same concept provide no usable identity.
func sourceTag(tags []map[string]any, keys ...string) string {
	value := ""
	for _, set := range tags {
		for key, raw := range set {
			for _, name := range keys {
				if !strings.EqualFold(key, name) {
					continue
				}
				v := strings.Join(strings.Fields(strings.ToLower(stringValue(raw))), " ")
				switch v {
				case "", "0", "00000000", "unknown", "none", "n/a":
					continue
				}
				if value != "" && value != v {
					return ""
				}
				value = v
			}
		}
	}
	return value
}
