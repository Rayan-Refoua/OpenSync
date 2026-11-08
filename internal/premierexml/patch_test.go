package premierexml

import (
	"bytes"
	"testing"
)

const fixture = `<xmeml version="4"><project><children><sequence><media>` +
	`<video><track>` +
	`<clipitem id="ref"><name>A</name><start>100</start><end>200</end><in>0</in><out>100</out></clipitem>` +
	`</track><track>` +
	`<clipitem id="target"><name>B</name><start>10</start><end>30</end><in>0</in><out>20</out></clipitem>` +
	`<clipitem id="other"><name>C</name><start>300</start><end>320</end><in>0</in><out>20</out></clipitem>` +
	`</track></video>` +
	`<audio><track><clipitem id="target-a"><name>B</name><start>10</start><end>30</end><in>0</in><out>20</out></clipitem></track></audio>` +
	`</media></sequence></children></project></xmeml>`

func plan() Plan {
	return Plan{Placements: []Placement{
		{ClipID: "target", Media: "video", Track: 2,
			ExpectedStart: 10, ExpectedEnd: 30, NewStart: 130},
		{ClipID: "target-a", Media: "audio", Track: 1,
			ExpectedStart: 10, ExpectedEnd: 30, NewStart: 130},
	}}
}

func TestPatchPreservesOtherXML(t *testing.T) {
	source := []byte(fixture)
	original := append([]byte(nil), source...)
	out, err := Patch(source, plan())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, original) {
		t.Fatal("source was modified")
	}
	if bytes.Count(out, []byte("<start>130</start>")) != 2 {
		t.Fatal("unexpected placement count")
	}
	if !bytes.Contains(out, []byte("<start>300</start>")) {
		t.Fatal("unrelated clip changed")
	}
}

func TestPatchRejectsCollision(t *testing.T) {
	p := plan()
	p.Placements[0].NewStart = 305
	if _, err := Patch([]byte(fixture), p); err == nil {
		t.Fatal("accepted overlapping placement")
	}
}

func TestPatchRejectsStalePlan(t *testing.T) {
	p := plan()
	p.Placements[0].ExpectedStart = 11
	if _, err := Patch([]byte(fixture), p); err == nil {
		t.Fatal("accepted stale source placement")
	}
}
