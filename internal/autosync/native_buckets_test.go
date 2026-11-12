package autosync

import (
	"context"
	"math"
	"reflect"
	"testing"
)

func TestEnvelopeBucketsPreserveFullRanking(t *testing.T) {
	var ref, query [3][]float32
	for b := range ref {
		ref[b] = make([]float32, 90000)
		seed := uint32(b + 73)
		for i := range ref[b] {
			seed = 1664525*seed + 1013904223
			ref[b][i] = float32(int32(seed>>16)-32768)/32768 + float32(math.Sin(float64(i)*.023))
		}
		query[b] = append([]float32(nil), ref[b][43000:45000]...)
	}
	expected, err := envelopeCandidates(context.Background(), ref, query)
	if err != nil {
		t.Fatal(err)
	}
	a := NewNativeAnalyzer(nil)
	defer a.Close()
	actual, err := envelopeCandidatesScheduled(context.Background(), ref, query, a.computeTasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(actual) == 0 || !reflect.DeepEqual(expected, actual) {
		t.Fatal("bucket scheduling changed recovery ranking")
	}
	if math.Abs(actual[0].Offset-688) > .016 {
		t.Fatal("known relation not recovered")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = envelopeCandidatesScheduled(ctx, ref, query, a.computeTasks); err != context.Canceled {
		t.Fatal("bucket cancellation ignored", err)
	}
}
