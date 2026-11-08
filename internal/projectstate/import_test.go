package projectstate

import "testing"

const testInventory = `{
 "idA":{"source":"C:\\clips\\A.mp4","duration_seconds":60},
 "idB":{"source":"C:\\clips\\B.mp4","duration_seconds":30}
}`

const testDiscovery = `{
 "experiment":"M4-005","status":"discovery_only",
 "results":[{"query_id":"idB","query_name":"B.mp4",
 "candidates":[
 {"reference_id":"idA","reference_name":"A.mp4",
  "offset_seconds":12.128,"votes":21,"support_windows":2,
  "status":"discovered_unverified",
  "anchors":[{"query_start":0,"offset_bucket":95,
   "offset_seconds":12.16,"votes":12}]},
 {"reference_id":"idA","reference_name":"A.mp4",
  "offset_seconds":-4,"votes":9,"support_windows":1,
  "status":"discovered_unverified",
  "anchors":[{"query_start":0,"offset_bucket":-31,
   "offset_seconds":-3.968,"votes":9}]}
 ]}]
}`

func TestImportPreservesAlternativesAndAnchors(t *testing.T) {
	p, s, err := ImportDiscovery([]byte(testInventory), []byte(testDiscovery))
	if err != nil {
		t.Fatal(err)
	}
	if s.Recordings != 2 || s.Hypotheses != 2 ||
		s.Anchors != 2 || s.Observations != 0 {
		t.Fatalf("unexpected summary: %+v", s)
	}
	h := p.Hypotheses["idB/idA/0"]
	if h.Status != Discovered || h.Reference != "idA" ||
		h.Query != "idB" || h.OffsetNS != 12_128_000_000 ||
		h.Discovery.Votes != 21 {
		t.Fatalf("incorrect imported hypothesis: %+v", h)
	}
	if _, ok := p.Hypotheses["idB/idA/1"]; !ok {
		t.Fatal("competing offset lost")
	}
}

func TestImportRejectsIdentityMismatch(t *testing.T) {
	bad := []byte(`{"experiment":"M4-005","status":"discovery_only",
	"results":[{"query_id":"idB","query_name":"wrong.mp4"}]}`)
	if _, _, err := ImportDiscovery([]byte(testInventory), bad); err == nil {
		t.Fatal("incorrect identity accepted")
	}
}

func TestImportRejectsInvalidEvidence(t *testing.T) {
	bad := []byte(`{"experiment":"M4-005","status":"discovery_only",
	"results":[{"query_id":"idB","query_name":"B.mp4",
	"candidates":[{"reference_id":"idA","reference_name":"A.mp4",
	"offset_seconds":12,"votes":0,"support_windows":1,
	"status":"discovered_unverified","anchors":[]}]}]}`)
	if _, _, err := ImportDiscovery([]byte(testInventory), bad); err == nil {
		t.Fatal("invalid discovery evidence accepted")
	}
}
