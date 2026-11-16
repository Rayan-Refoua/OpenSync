package autosync

import (
	"math"
	"testing"
)

func componentFixture() ([]MediaRecord, []*PairEvidence, []NativeProfilePair) {
	r := []MediaRecord{{ID: "a", Duration: 180}, {ID: "b", Duration: 180}, {ID: "c", Duration: 180}, {ID: "d", Duration: 180}}
	e := []*PairEvidence{testRelation(0, 1, 10, 1), testRelation(2, 3, 20, 1)}
	profile := func(offset float64) *NativeProfile {
		p := testProfile(offset, "corroboration_required")
		p.Limit = .05
		p.Range = []float64{10, 150}
		p.AuditResult = &ModelJudgment{Passes: true, Supported: 3}
		p.Training = nil
		p.Selection = nil
		p.Audit = nil
		for role, dst := range []*[]*Measurement{&p.Training, &p.Selection, &p.Audit} {
			for _, x := range []float64{15, 60, 120} {
				*dst = append(*dst, &Measurement{QueryTime: x + float64(role)*2, Offset: offset, Window: 1, Supported: true, Precise: true})
			}
		}
		return p
	}
	return r, e, []NativeProfilePair{{Reference: 0, Query: 2, Profiles: []*NativeProfile{profile(50)}}, {Reference: 1, Query: 3, Profiles: []*NativeProfile{profile(60)}}}
}

func TestComponentCorroborationNeedsDistributedEvidence(t *testing.T) {
	for _, name := range []string{"valid", "one_event", "duplicate_pcm", "conflicting_offset", "incompatible_drift", "audit_failed", "alternative_order"} {
		t.Run(name, func(t *testing.T) {
			r, edges, profiles := componentFixture()
			switch name {
			case "one_event":
				for _, pair := range profiles {
					p := pair.Profiles[0]
					for _, role := range [][]*Measurement{p.Training, p.Selection, p.Audit} {
						for _, o := range role {
							o.QueryTime = 40
							if pair.Query == 3 {
								o.QueryTime = 20
							}
							o.Window = 4
						}
					}
				}
			case "duplicate_pcm":
				r[0].CacheSHA = "same"
				r[1].CacheSHA = "same"
			case "conflicting_offset":
				profiles[1].Profiles[0].Model.Offset += 3
			case "incompatible_drift":
				profiles[1].Profiles[0].Model.Offset -= .09
				profiles[1].Profiles[0].Model.Slope = .001
			case "audit_failed":
				profiles[1].Profiles[0].AuditResult.Passes = false
			case "alternative_order":
				profiles[0], profiles[1] = profiles[1], profiles[0]
			}
			got, report := corroborateComponents(r, edges, profiles)
			want := 2
			if name == "valid" || name == "alternative_order" {
				want = 4
			}
			if len(got) != want {
				t.Fatalf("edges=%d want=%d: %+v", len(got), want, report)
			}
			if name == "valid" {
				integrated, _ := selectNativeEdges(r, edges, profiles)
				if len(integrated) != 4 {
					t.Fatal("component pass not integrated")
				}
			}
			if want == 4 {
				layout, g, err := nativeSolve(r, got)
				if err != nil {
					t.Fatal(err)
				}
				if len(g["components"].([][]int)) != 1 {
					t.Fatal(g)
				}
				var starts [4]float64
				for _, m := range layout {
					starts[m.Recording] = m.Start
					if m.Status == "certified" {
						t.Fatal("graph certified itself")
					}
				}
				if math.Abs(starts[2]-starts[0]-50) > 1e-8 || math.Abs(starts[3]-starts[1]-60) > 1e-8 {
					t.Fatal(starts)
				}
			}
		})
	}
}

func TestCorroborationDoesNotCountCopiesOrOneMoment(t *testing.T) {
	r := []MediaRecord{{ID: "one"}, {ID: "two"}}
	p, q := testProfile(4, "corroboration_required"), testProfile(4, "corroboration_required")
	g := []eventEntry{{reference: 0, profile: p}, {reference: 1, profile: q}}
	if corroboratingReferences(g, r) != 2 {
		t.Fatal("distributed evidence lost")
	}
	for _, h := range []*NativeProfile{p, q} {
		for _, role := range [][]*Measurement{h.Training, h.Selection, h.Audit} {
			for _, o := range role {
				o.QueryTime = 4
			}
		}
	}
	if corroboratingReferences(g, r) != 0 {
		t.Fatal("same event counted independently")
	}
	p, q = testProfile(4, "corroboration_required"), testProfile(4, "corroboration_required")
	g[0].profile = p
	g[1].profile = q
	r[0].CacheSHA = "same-pcm"
	r[1].CacheSHA = "same-pcm"
	if corroboratingReferences(g, r) != 1 {
		t.Fatal("duplicate source counted independently")
	}
}

func TestComponentCorroborationPreservesNonlinearClocks(t *testing.T) {
	r, edges, profiles := componentFixture()
	edges[0].Selected.Knots = []TimePoint{{0, 10}, {90, 100.2}, {180, 190}}
	for _, o := range edges[0].Selected.Fit {
		o.Offset = MapTime(edges[0].Selected.Knots, o.QueryTime) - o.QueryTime
	}
	p := profiles[1].Profiles[0]
	p.Model = &NativeRelationModel{Kind: "piecewise_linear"}
	inv := InverseMapping(edges[0].Selected.Knots)
	for _, x := range []float64{0, 30.2, 180} {
		p.Model.Knots = append(p.Model.Knots, TimePoint{x, MapTime(inv, 70+x) - x})
	}
	for _, role := range [][]*Measurement{p.Training, p.Selection, p.Audit} {
		for _, o := range role {
			o.Offset = p.Model.predict(o.QueryTime)
		}
	}
	got, report := corroborateComponents(r, edges, profiles)
	if len(got) != 4 {
		t.Fatalf("nonlinear component not recovered: %+v", report)
	}
	l, g, err := nativeSolve(r, got)
	if err != nil {
		t.Fatal(err)
	}
	if len(g["components"].([][]int)) != 1 {
		t.Fatal("nonlinear component lost")
	}
	for _, m := range l {
		if m.Recording == 1 && len(m.Knots) < 3 {
			t.Fatal("clock bend flattened")
		}
	}
}

// Read-only replay of saved acoustic evidence; no media extraction, no use of
// PluralEyes as truth. Existing project placements provide a regression check,
// not independent labels for false-positive-rate claims.

func TestComponentCorroborationCanAttachOneRecorder(t *testing.T) {
	for _, variant := range []string{"compatible", "only_two", "duplicate_pcm", "inconsistent"} {
		t.Run(variant, func(t *testing.T) {
			r, _, fixture := componentFixture()
			r[3].Duration = 350
			base := []*PairEvidence{testRelation(0, 1, 10, 1), testRelation(1, 2, 10, 1)}
			var profiles []NativeProfilePair
			for q := 0; q < 3; q++ {
				p := *fixture[0].Profiles[0]
				offset := 50 + float64(q*10)
				if variant == "inconsistent" && q == 2 {
					offset += .2
				}
				p.Model = &NativeRelationModel{Kind: "constant", Offset: offset}
				groups := []*[]*Measurement{&p.Training, &p.Selection, &p.Audit}
				for _, g := range groups {
					independent := make([]*Measurement, len(*g))
					for i, o := range *g {
						m := *o
						m.Offset = offset
						independent[i] = &m
					}
					*g = independent
				}
				profiles = append(profiles, NativeProfilePair{Reference: 3, Query: q, Profiles: []*NativeProfile{&p}})
			}
			if variant == "only_two" {
				profiles = profiles[:2]
			}
			if variant == "duplicate_pcm" {
				r[1].CacheSHA = "same"
				r[2].CacheSHA = "same"
			}
			got, report := corroborateComponents(r, base, profiles)
			want := len(base)
			if variant == "compatible" {
				want += 3
			}
			if len(got) != want {
				t.Fatalf("got %d edges, want %d: %+v", len(got), want, report)
			}
			if variant == "compatible" {
				_, graph, err := nativeSolve(r, got)
				if err != nil || len(graph["components"].([][]int)) != 1 {
					t.Fatalf("recorder did not attach: %v", err)
				}
			}
		})
	}
}
