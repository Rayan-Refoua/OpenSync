package autosync

import (
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeFixtureWAV(path string, samples []int16) error {
	return writeFixtureChannels(path, samples, 1)
}
func writeFixtureChannels(path string, samples []int16, channels int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	header := make([]byte, 44)
	copy(header, "RIFF")
	binary.LittleEndian.PutUint32(header[4:], uint32(36+2*len(samples)))
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint16(header[20:], 1)
	binary.LittleEndian.PutUint16(header[22:], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:], 16000)
	binary.LittleEndian.PutUint32(header[28:], uint32(32000*channels))
	binary.LittleEndian.PutUint16(header[32:], uint16(2*channels))
	binary.LittleEndian.PutUint16(header[34:], 16)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], uint32(2*len(samples)))
	if _, err = f.Write(header); err != nil {
		return err
	}
	return binary.Write(f, binary.LittleEndian, samples)
}

func TestNativePiecewiseAudioEventTimingAndChannels(t *testing.T) {
	root := integrationFolder(t)
	samples := make([]int16, 12*16000*2)
	events := []struct {
		center  float64
		channel int
	}{{2, 0}, {5, 1}, {8, 0}, {11, 1}}
	for i := 0; i < 12*16000; i++ {
		time := float64(i) / 16000
		for _, e := range events {
			value := .4 * math.Exp(-math.Pow((time-e.center)/.06, 2)) * math.Sin(2*math.Pi*700*time)
			samples[i*2+e.channel] += int16(value * 32767)
		}
	}
	source, target := filepath.Join(root, "source.wav"), filepath.Join(root, "corrected.wav")
	if err := writeFixtureChannels(source, samples, 2); err != nil {
		t.Fatal(err)
	}
	before, _ := hashFile(source)
	mapping := []TimePoint{{0, 0}, {6, 6.12}, {12, 12}}
	r := MediaRecord{Source: source, LocalSource: source, Duration: 12, Audio: map[string]any{"sample_rate": "16000", "channels": 2}}
	if err := renderMappedAudio(context.Background(), r, mapping, target); err != nil {
		t.Fatal(err)
	}
	tool, _ := mediaTool("ffmpeg")
	data, err := exec.Command(tool, "-nostdin", "-v", "error", "-i", target, "-f", "f32le", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 12*16000*2*4 {
		t.Fatal("sample count changed")
	}
	after, _ := hashFile(source)
	if before != after {
		t.Fatal("source audio changed")
	}
	for _, e := range events {
		expected := MapTime(mapping, e.center)
		first, last := int((expected-.25)*16000), int((expected+.25)*16000)
		energy, moment, other := 0., 0., 0.
		for i := first; i < last; i++ {
			v := float64(math.Float32frombits(binary.LittleEndian.Uint32(data[(i*2+e.channel)*4:])))
			n := float64(math.Float32frombits(binary.LittleEndian.Uint32(data[(i*2+1-e.channel)*4:])))
			energy += v * v
			moment += v * v * float64(i) / 16000
			other = math.Max(other, math.Abs(n))
		}
		if energy == 0 || math.Abs(moment/energy-expected) > .03 || other > .001 {
			t.Fatalf("rendered event shifted or channel leaked: expected %.6f, got %.6f, other %.6f", expected, moment/energy, other)
		}
	}
}
func fixtureNoise(seconds int, seed uint32) []int16 {
	v := make([]int16, seconds*16000)
	for i := range v {
		seed = 1664525*seed + 1013904223
		envelope := .3 + .7*math.Pow(math.Sin(float64(i)/16000*1.731), 2)
		v[i] = int16(float64(int32(seed>>16)-32768) * .45 * envelope)
	}
	return v
}
func integrationFolder(t *testing.T) string {
	t.Helper()
	if os.Getenv("OPENSYNC_INTEGRATION") != "1" {
		t.Skip("set OPENSYNC_INTEGRATION=1 for FFmpeg integration tests")
	}
	if _, err := mediaTool("ffmpeg"); err != nil {
		t.Fatal(err)
	}
	root := os.Getenv("OPENSYNC_INTEGRATION_OUT")
	if root == "" {
		return t.TempDir()
	}
	root = filepath.Join(root, t.Name())
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	return root
}
func TestNativeFreshFileList(t *testing.T) {
	root := integrationFolder(t)
	reference := fixtureNoise(90, 1234)
	offset := 20.136
	first := int(offset * 16000)
	query := append([]int16(nil), reference[first:first+36*16000]...)
	for i := range query {
		query[i] = -query[i]
	}
	names := []string{"reference.wav", "query.wav", "unrelated.wav", "silence.wav"}
	audio := [][]int16{reference, query, fixtureNoise(18, 987654), make([]int16, 16000*3)}
	files := []string{}
	for i, name := range names {
		path := filepath.Join(root, name)
		if err := writeFixtureWAV(path, audio[i]); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	list := filepath.Join(root, "files.json")
	if err := writeJSON(list, files); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "project")
	if err := NativeSync(context.Background(), NativeSyncOptions{FileList: list, Output: out, Name: "Synthetic truth", Unsynced: "append", Stage: "all", MaxResidualMS: 150}); err != nil {
		t.Fatal(err)
	}
	project, err := LoadNativeProject(filepath.Join(out, "OpenSync.opensync"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"OpenSync.opensync": true, "OpenSync.xml": true, "input-report.json": true, "timeline-manifest.json": true, "structural-audit.json": true, "native-run-profile.json": true}
	for _, e := range entries {
		if !allowed[e.Name()] {
			t.Fatalf("default sync wrote analysis/cache data: %s", e.Name())
		}
	}
	for _, r := range project.Data.Records {
		if r.Cache != "" || r.LocalSource != r.Source {
			t.Fatal("saved project depends on cached media")
		}
	}
	models := map[string]TimeModel{}
	for _, m := range project.Data.Models {
		models[project.Data.Records[m.Recording].Name] = m
	}
	r, q := models["reference.wav"], models["query.wav"]
	if r.Component != q.Component || r.ComponentSize != 2 || math.Abs(q.Offset-r.Offset-offset) > .001 {
		t.Fatalf("known offset not recovered: reference=%+v query=%+v", r, q)
	}
	for _, name := range []string{"unrelated.wav", "silence.wav"} {
		if models[name].Resolved {
			t.Fatalf("negative certified: %s", name)
		}
	}
	xmlPath := filepath.Join(out, "OpenSync.xml")
	if err = auditNativeRoundtrip(xmlPath, xmlPath, filepath.Join(out, "self-import-audit.json")); err != nil {
		t.Fatal(err)
	}
}
func TestNativeAudioOnlyDriftRender(t *testing.T) {
	root := integrationFolder(t)
	path := filepath.Join(root, "original.wav")
	if err := writeFixtureWAV(path, fixtureNoise(6, 4321)); err != nil {
		t.Fatal(err)
	}
	r := MediaRecord{ID: "render-fixture", Name: "original.wav", Source: path, LocalSource: path, Duration: 6, Audio: map[string]any{"sample_rate": "16000", "channels": 1, "bits_per_sample": 16}}
	points := []TimePoint{{0, 4}, {3, 7.03}, {6, 10.09}}
	target := filepath.Join(root, "corrected.wav")
	if err := renderMappedAudio(context.Background(), r, points, target); err != nil {
		t.Fatal(err)
	}
	info, err := probeNative(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	duration := number(info["format"].(map[string]any)["duration"])
	if math.Abs(duration-6.09) > 1./16000 {
		t.Fatalf("drift render duration %.12f", duration)
	}
	p := &NativeProject{Data: NativePayload{Name: "Render", Records: []MediaRecord{r}, Models: []TimeModel{{Recording: 0, Component: 1, ComponentSize: 1, Offset: 4, Rate: 1.015, Knots: points, Error: .001, Status: "acoustically_supported_not_certified"}}}}
	placements, rate, err := nativePlacements(p, NativeExportOptions{Output: root, Unsynced: "append", MaxResidualMS: 45})
	if err != nil {
		t.Fatal(err)
	}
	if len(placements) != 1 || placements[0].Derivative == nil {
		t.Fatal("audio correction missing")
	}
	data, err := buildNativeXML(p, placements, rate)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateNativeExport(data, []MediaRecord{r}, placements, rate, "append"); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "render.xml"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
