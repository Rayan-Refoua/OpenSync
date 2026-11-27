package autosync

import (
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

func sortedLandmarkCandidates(choices []landmarkVote) []NativeCandidate {
	sort.Slice(choices, func(i, j int) bool {
		if choices[i].count != choices[j].count {
			return choices[i].count > choices[j].count
		}
		return choices[i].bucket < choices[j].bucket
	})
	var result []NativeCandidate
	for _, c := range choices {
		if c.count < 6 || len(result) == 8 {
			break
		}
		offset := float64(c.bucket) * 16 * 128 / 16000
		distinct := true
		for _, prior := range result {
			if math.Abs(offset-prior.Offset) < .75 {
				distinct = false
				break
			}
		}
		if distinct {
			result = append(result, NativeCandidate{Offset: offset, Votes: c.count})
		}
	}
	return result
}

func TestLandmarkRankingMatchesFullSort(t *testing.T) {
	random := rand.New(rand.NewSource(42))
	for trial := 0; trial < 400; trial++ {
		var votes, competitive []landmarkVote
		for bucket := -trial; bucket <= trial; bucket++ {
			v := landmarkVote{random.Intn(12), int64(bucket)}
			votes = append(votes, v)
			if v.count >= 6 {
				competitive = append(competitive, v)
			}
		}
		random.Shuffle(len(competitive), func(i, j int) {
			competitive[i], competitive[j] = competitive[j], competitive[i]
		})
		got, want := landmarkCandidates(competitive), sortedLandmarkCandidates(votes)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("trial %d: got %v, want %v", trial, got, want)
		}
	}
}

func BenchmarkLandmarkRanking(b *testing.B) {
	random := rand.New(rand.NewSource(42))
	votes := make([]landmarkVote, 32000)
	for i := range votes {
		votes[i] = landmarkVote{random.Intn(8) + 1, int64(i) - 16000}
	}
	b.Run("full_sort", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var choices []landmarkVote
			for _, v := range votes {
				choices = append(choices, v)
			}
			sortedLandmarkCandidates(choices)
		}
	})
	b.Run("bounded", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			var competitive []landmarkVote
			for _, v := range votes {
				if v.count >= 6 {
					competitive = append(competitive, v)
				}
			}
			landmarkCandidates(competitive)
		}
	})
}
