package autosync

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
)

func TestSharedComputePoolCompletesConcurrentParents(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	a := NewNativeAnalyzer(nil)
	values := make([][]int, 12)
	var parents sync.WaitGroup
	for p := range values {
		values[p] = make([]int, 97)
		parents.Add(1)
		go func(p int) {
			defer parents.Done()
			a.computeTasks(len(values[p]), func(i int) { values[p][i] = p*1000 + i + 1 })
		}(p)
	}
	parents.Wait()
	a.Close()
	for p, list := range values {
		for i, v := range list {
			if v != p*1000+i+1 {
				t.Fatal("lost or reordered work")
			}
		}
	}
	if a.compute.peak.Load() > 4 || a.compute.completed.Load() != 12*97 {
		t.Fatal("pool limit or completion accounting failed")
	}
}

func TestParallelFeaturesAndMeasurementsMatchSingleWorker(t *testing.T) {
	root := integrationFolder(t)
	reference := fixtureNoise(180, 453)
	query := append([]int16(nil), reference[20*16000:140*16000]...)
	refPath, qPath := filepath.Join(root, "ref.wav"), filepath.Join(root, "query.wav")
	if err := writeFixtureWAV(refPath, reference); err != nil {
		t.Fatal(err)
	}
	if err := writeFixtureWAV(qPath, query); err != nil {
		t.Fatal(err)
	}
	records := []MediaRecord{{ID: "ref", Name: "ref.wav", Cache: refPath, Duration: 180}, {ID: "query", Name: "query.wav", Cache: qPath, Duration: 120}}
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	one := NewNativeAnalyzer(records)
	featuresOne, err := one.envelopeFeatures(0, filepath.Join(root, "one"))
	if err != nil {
		one.Close()
		t.Fatal(err)
	}
	profileOne, err := one.profileCandidate(0, 1, NativePrior{Offset: 20})
	one.Close()
	if err != nil {
		t.Fatal(err)
	}
	runtime.GOMAXPROCS(8)
	many := NewNativeAnalyzer(records)
	featuresMany, err := many.envelopeFeatures(0, filepath.Join(root, "many"))
	if err != nil {
		many.Close()
		t.Fatal(err)
	}
	profileMany, err := many.profileCandidate(0, 1, NativePrior{Offset: 20})
	many.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(featuresOne, featuresMany) {
		t.Fatal("feature values changed with parallelism")
	}
	left, _ := json.Marshal(profileOne)
	right, _ := json.Marshal(profileMany)
	if string(left) != string(right) {
		t.Fatal("measurement evidence or timing changed with parallelism")
	}
}
