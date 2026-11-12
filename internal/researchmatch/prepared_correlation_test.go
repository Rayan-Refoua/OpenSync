package researchmatch

import (
	"math"
	"reflect"
	"testing"
)

func TestPreparedCorrelationMatchesOriginalAcrossPaddingChanges(t *testing.T) {
	for _, queryLength := range []int{1, 7, 63, 1023, 2048} {
		query := make([]float64, queryLength)
		for i := range query {
			query[i] = math.Sin(float64(i)*.31) - .2*math.Cos(float64(i)*1.7)
		}
		prepared := PrepareCorrelationQuery(query)
		for _, extra := range []int{0, 1, 103, 4097, 1, 0} {
			reference := make([]float64, queryLength+extra)
			for i := range reference {
				reference[i] = math.Sin(float64(i)*.71) + .4*math.Cos(float64(i)*.13)
			}
			want, err := CorrelationDots(reference, query)
			if err != nil {
				t.Fatal(err)
			}
			got, err := prepared.Dots(reference)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("dot products changed: query=%d reference=%d", queryLength, len(reference))
			}
		}
	}
}
