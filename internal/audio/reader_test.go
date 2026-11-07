package audio

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.wav")

	samples := []int16{-32768, -100, 0, 1234, 32767}
	data := make([]byte, len(samples)*2)
	for i, v := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(v))
	}

	wav := make([]byte, 44+len(data))
	copy(wav[0:], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
	copy(wav[8:], "WAVE")
	copy(wav[12:], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], 1)
	binary.LittleEndian.PutUint32(wav[24:], 16000)
	binary.LittleEndian.PutUint32(wav[28:], 32000)
	binary.LittleEndian.PutUint16(wav[32:], 2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(data)))
	copy(wav[44:], data)

	if err := os.WriteFile(path, wav, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExactWindow(t *testing.T) {
	r, err := Open(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.Frames() != 5 || r.SampleRate() != 16000 {
		t.Fatal("incorrect audio metadata")
	}

	got, err := r.ReadFrames(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []int16{-100, 0, 1234}) {
		t.Fatalf("incorrect samples: %v", got)
	}

	got, err = r.ReadFrames(4, 1)
	if err != nil || !reflect.DeepEqual(got, []int16{32767}) {
		t.Fatalf("incorrect final sample: %v, %v", got, err)
	}
}

func TestWindowBounds(t *testing.T) {
	r, err := Open(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for _, tc := range [][2]int64{
		{-1, 1}, {0, -1}, {4, 2}, {6, 0},
	} {
		_, err := r.ReadFrames(tc[0], tc[1])
		if !errors.Is(err, ErrWindowBounds) {
			t.Fatalf("expected bounds error for %v: %v", tc, err)
		}
	}
}

func TestTruncatedFileRejected(t *testing.T) {
	path := fixture(t)
	if err := os.Truncate(path, 46); err != nil {
		t.Fatal(err)
	}
	if r, err := Open(path); err == nil {
		r.Close()
		t.Fatal("truncated recording was accepted")
	}
}

func TestActualNVMeCache(t *testing.T) {
	path := os.Getenv("OPENSYNC_TEST_WAV")
	if path == "" {
		t.Skip("no real cache fixture supplied")
	}

	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	if r.Frames() < 16000 {
		t.Fatal("unexpectedly short recording")
	}

	for _, start := range []int64{
		0, r.Frames() / 2, r.Frames() - 16000,
	} {
		got, err := r.ReadFrames(start, 16000)
		if err != nil || len(got) != 16000 {
			t.Fatalf("sample window at %d: %v", start, err)
		}
	}
}
