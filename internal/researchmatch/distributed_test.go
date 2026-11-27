package researchmatch

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestDistributedBucketsPreserveStatisticsAndPeaks(t *testing.T) {
	root := t.TempDir()
	ref, query := make([][]float64, 5), make([][]float64, 5)
	for b := range ref {
		ref[b] = make([]float64, 38000)
		random := rand.New(rand.NewSource(int64(13 + b)))
		for i := range ref[b] {
			ref[b][i] = random.NormFloat64()
		}
		query[b] = append([]float64(nil), ref[b][24567:24967]...)
	}
	r, q := featureFixture(t, filepath.Join(root, "r.features"), 1, ref), featureFixture(t, filepath.Join(root, "q.features"), 1, query)
	want, err := SearchDistributed(r, q, -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	schedule := func(n int, work func(int)) {
		var wg sync.WaitGroup
		slots := make(chan struct{}, 8)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(j int) { defer wg.Done(); slots <- struct{}{}; work(j); <-slots }(i)
		}
		wg.Wait()
	}
	got, err := SearchDistributedScheduled(r, q, -1, -1, schedule)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatal("parallel buckets changed statistics or peaks")
	}
}

func TestOnsetSearchRequiresReservedSections(t *testing.T) {
	const offset = 4230
	root := t.TempDir()
	reference, query := make([][]float64, 5), make([][]float64, 5)
	for b := range reference {
		random := rand.New(rand.NewSource(int64(31 + b)))
		reference[b] = make([]float64, 12000)
		for i := range reference[b] {
			reference[b][i] = random.NormFloat64()
		}
		query[b] = append([]float64(nil), reference[b][offset:offset+1600]...)
	}
	r := featureFixture(t, filepath.Join(root, "r.features"), 1, reference)
	search := func(name string) DistributedResult {
		t.Helper()
		q := featureFixture(t, filepath.Join(root, name), 1, query)
		result, err := SearchDistributedOnsets(r, q, func(n int, work func(int)) {
			for i := 0; i < n; i++ {
				work(i)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	result := search("positive.features")
	if top, ok := DistinctOnsetPeak(result.Peaks); !ok || math.Abs(top.Offset-float64(offset)*.01) > .001 {
		t.Fatalf("known offset not recovered: %+v", result.Peaks)
	}
	for _, band := range query {
		for _, index := range []int{1, 3} {
			section := result.Sections[index]
			for i := section.First - onsetLag; i < section.First+section.Count; i++ {
				band[i] = 0
			}
		}
	}
	changed := search("bad-holdouts.features")
	if changed.Peaks[0].Offset != result.Peaks[0].Offset || changed.Peaks[0].Score != result.Peaks[0].Score {
		t.Fatalf("reserved sections leaked into candidate ranking: %+v", changed.Peaks[0])
	}
	if _, ok := DistinctOnsetPeak(changed.Peaks); ok {
		t.Fatal("accepted an offset without evidence in reserved audio")
	}
}

func featureFixture(t *testing.T, path string, channels int, values [][]float64) *DistributedFile {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(featureMagic)
	binary.Write(f, binary.LittleEndian, uint32(channels))
	for i := range values[0] {
		for c := range values {
			binary.Write(f, binary.LittleEndian, float32(values[c][i]))
		}
	}
	f.Close()
	r, err := OpenDistributed(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestDistributedPearsonMatchesDirectCenteredCalculation(t *testing.T) {
	r := []float64{3, 1, 4, 1, 5, 9, 2, 6, 5}
	q := []float64{7, 2, 8, 1}
	actual, err := distributedCorrelation(r, q)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range actual {
		a, b := 0., 0.
		for i := range q {
			a += r[k+i]
			b += q[i]
		}
		a /= float64(len(q))
		b /= float64(len(q))
		dot, aa, bb := 0., 0., 0.
		for i := range q {
			x, y := r[k+i]-a, q[i]-b
			dot += x * y
			aa += x * x
			bb += y * y
		}
		want := dot / math.Sqrt(aa*bb)
		if math.Abs(v-want) > 1e-10 {
			t.Fatalf("lag %d: %g != %g", k, v, want)
		}
	}
}

func TestDistributedPreservesIndependentEventsAndPartialOverlap(t *testing.T) {
	dir := t.TempDir()
	rng := rand.New(rand.NewSource(72))
	ref := make([][]float64, 5)
	query := make([][]float64, 5)
	for b := range ref {
		ref[b] = make([]float64, 15000)
		for i := range ref[b] {
			ref[b][i] = rng.NormFloat64()
		}
		query[b] = append([]float64(nil), ref[b][375:975]...)
		copy(ref[b][12375:], query[b])
	}
	r := featureFixture(t, filepath.Join(dir, "ref"), 1, ref)
	q := featureFixture(t, filepath.Join(dir, "query"), 1, query)
	result, err := SearchDistributed(r, q, -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Peaks) < 2 || math.Abs(result.Peaks[0].Offset-3.75) > .011 || math.Abs(result.Peaks[1].Offset-123.75) > .011 {
		t.Fatalf("lost competing event or tile offset: %+v", result.Peaks[:min(2, len(result.Peaks))])
	}
	if result.Certification != "not_assessed" {
		t.Fatal("discovery cannot certify")
	}
	// Query starts before the reference. Two complete later sections overlap.
	for b := range ref {
		ref[b] = append([]float64(nil), query[b][200:]...)
	}
	r = featureFixture(t, filepath.Join(dir, "partial"), 1, ref)
	result, err = SearchDistributed(r, q, -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Peaks) == 0 || math.Abs(result.Peaks[0].Offset+2) > .011 {
		t.Fatalf("negative partial offset lost: %+v", result.Peaks)
	}
}

func TestDistributedStereoPolarityDoesNotCancelAndSilenceHasNoPeaks(t *testing.T) {
	var pcm, output bytes.Buffer
	for i := 0; i < 16000; i++ {
		x := float32(math.Sin(float64(i)*.21) * (.2 + .1*math.Sin(float64(i)*.001)))
		binary.Write(&pcm, binary.LittleEndian, x)
		binary.Write(&pcm, binary.LittleEndian, -x)
	}
	if err := WriteDistributed(&pcm, &output, 2); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "stereo")
	os.WriteFile(path, output.Bytes(), 0600)
	f, err := OpenDistributed(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	values, err := f.Read(0, f.Frames)
	if err != nil {
		t.Fatal(err)
	}
	energy := 0.
	for b := 0; b < 5; b++ {
		for i, x := range values[b] {
			energy += x * x
			if math.Abs(x-values[b+5][i]) > 1e-10 {
				t.Fatal("polarity changed spectral features")
			}
		}
	}
	if energy == 0 {
		t.Fatal("cancelled channel energy")
	}
	zero := make([][]float64, 5)
	for b := range zero {
		zero[b] = make([]float64, 600)
	}
	z := featureFixture(t, filepath.Join(t.TempDir(), "silence"), 1, zero)
	result, err := SearchDistributed(z, z, -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Peaks) != 0 {
		t.Fatal("silence invented a candidate")
	}
}

func TestDistributedLongQueryCrossesReferenceTileBoundary(t *testing.T) {
	const start = 4789
	root := t.TempDir()
	ref, query := make([][]float64, 5), make([][]float64, 5)
	for band := range ref {
		random := rand.New(rand.NewSource(int64(81 + band)))
		ref[band] = make([]float64, 43000)
		for i := range ref[band] {
			ref[band][i] = random.NormFloat64()
		}
		query[band] = ref[band][start : start+26000]
	}
	r := featureFixture(t, filepath.Join(root, "long-ref.features"), 1, ref)
	q := featureFixture(t, filepath.Join(root, "long-query.features"), 1, query)
	got, err := SearchDistributed(r, q, -1, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Peaks) == 0 || math.Abs(got.Peaks[0].Offset-float64(start)/100) > .011 {
		t.Fatalf("long query did not recover expected start: %+v", got.Peaks)
	}
}

func TestOccupiedOffsetSearchKeepsFreeMatch(t *testing.T) {
	root := t.TempDir()
	ref, query := make([][]float64, 5), make([][]float64, 5)
	for b := range ref {
		ref[b] = make([]float64, 30000)
		random := rand.New(rand.NewSource(int64(55 + b)))
		for i := range ref[b] {
			ref[b][i] = random.NormFloat64()
		}
		query[b] = append([]float64(nil), ref[b][23000:23400]...)
	}
	r := featureFixture(t, filepath.Join(root, "reference.features"), 1, ref)
	q := featureFixture(t, filepath.Join(root, "query.features"), 1, query)
	run := func(excluded []OffsetSpan) DistributedResult {
		t.Helper()
		result, err := SearchDistributedExcluding(r, q, func(n int, work func(int)) {
			for i := 0; i < n; i++ {
				work(i)
			}
		}, false, excluded)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	full, constrained := run(nil), run([]OffsetSpan{{2000, 20000}})
	if full.Peaks[0].Offset != 230 || constrained.Peaks[0].Offset != 230 {
		t.Fatal("known free match was lost")
	}
	if full.OffsetsExamined-constrained.OffsetsExamined != 18000 {
		t.Fatalf("expected 18000 fewer offsets, got %d", full.OffsetsExamined-constrained.OffsetsExamined)
	}
	for _, peak := range constrained.Peaks {
		if peak.Offset >= 20 && peak.Offset < 200 {
			t.Fatalf("occupied offset evaluated: %+v", peak)
		}
	}
}
