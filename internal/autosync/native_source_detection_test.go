package autosync

import "testing"

func TestSourceTrackGroupUsesDeviceAndRecordingProfile(t *testing.T) {
	a := MediaRecord{Name: "First.mp4", Source: "card-a/First.mp4", Video: map[string]any{"codec_name": "h264"}, FormatTags: map[string]any{"make": "Sony", "model": "FX3", "serial_number": "12345"}}
	b := MediaRecord{Name: "Renamed.mov", Source: "card-b/Renamed.mov", Video: map[string]any{"codec_name": "hevc"}, FormatTags: map[string]any{"make": "SONY", "model": "FX3", "serial_number": "12345"}}
	key := func(r MediaRecord) string { k, _ := SourceTrackGroup(r); return k }
	if key(a) != key(b) {
		t.Fatal("same recorder split after rename, card change and codec change")
	}
	b.FormatTags["serial_number"] = "67890"
	if key(a) == key(b) {
		t.Fatal("different recorder serials merged")
	}
	b.Name, b.Source, b.Video = "Other.mp4", "card-a/Other.mp4", a.Video
	delete(a.FormatTags, "serial_number")
	delete(b.FormatTags, "serial_number")
	if key(a) != key(b) {
		t.Fatal("compatible, unnumbered originals should share their folder profile")
	}
	a.FormatTags["encoder"], b.FormatTags["encoder"] = "HandBrake 1.9", "HandBrake 1.9"
	if key(a) == key(b) {
		t.Fatal("shared transcoder incorrectly grouped unnumbered sources")
	}
}

func TestSourceReviewSplitsNumberedFamilyByDimensionsAndAudio(t *testing.T) {
	a := MediaRecord{ID: "one", Name: "C0001.mp4", Source: "card/C0001.mp4", Video: map[string]any{"width": 1920, "height": 1080}, Audio: map[string]any{"channels": 2}}
	b := MediaRecord{ID: "two", Name: "C0002.mp4", Source: "card/C0002.mp4", Video: map[string]any{"width": 3840, "height": 2160}, Audio: a.Audio}
	original, _ := SourceTrackGroup(a)
	changed, _ := SourceTrackGroup(b)
	if changed == original {
		t.Fatal("different frame dimensions merged")
	}
	b.Video = a.Video
	b.AudioStreams = []map[string]any{{"channels": 1}, {"channels": 1}}
	changed, _ = SourceTrackGroup(b)
	if changed == original {
		t.Fatal("stereo and dual mono merged")
	}
	records := []MediaRecord{a, b}
	detected, reviewed := sourceInventory(records), sourceInventory(records)
	reviewed.Clips[1].Assignment = reviewed.Clips[0].Assignment
	match := reviewed.Clips[1].Mtime
	reviewed.Clips[1].Mtime = "stale"
	if applySourcePlan(records, detected, reviewed) == nil || records[1].SourceAssignment != nil {
		t.Fatal("stale plan was accepted or partially applied")
	}
	reviewed.Clips[1].Mtime = match
	if err := applySourcePlan(records, detected, reviewed); err != nil {
		t.Fatal(err)
	}
	if group, _ := SourceTrackGroup(records[1]); group != original {
		t.Fatal("reviewed merge did not override detection")
	}
}
