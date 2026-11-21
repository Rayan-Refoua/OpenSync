package desktop

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"math"
	"os"
	"path/filepath"
)

const peakDiskVersion = "OpenSync Peaks v1\x00"
const peakDiskPayload = peakChunkBins * 8

func peakDiskPath(dir, key string) string {
	hash := sha256.Sum256([]byte(peakDiskVersion + key))
	return filepath.Join(dir, hex.EncodeToString(hash[:])+".peak")
}

func readPeakChunk(dir, key string) *peakChunk {
	if dir == "" {
		return nil
	}
	f, err := os.Open(peakDiskPath(dir, key))
	if err != nil {
		return nil
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, peakDiskPayload+sha256.Size+1))
	if err != nil || len(data) != peakDiskPayload+sha256.Size {
		return nil
	}
	sum := sha256.Sum256(data[:peakDiskPayload])
	if !bytes.Equal(sum[:], data[peakDiskPayload:]) {
		return nil
	}
	base := make([]peakPair, peakChunkBins)
	for i := range base {
		base[i] = peakPair{math.Float32frombits(binary.LittleEndian.Uint32(data[i*8:])), math.Float32frombits(binary.LittleEndian.Uint32(data[i*8+4:]))}
	}
	return buildPeakChunk(base)
}

func writePeakChunk(dir, key string, chunk *peakChunk) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data := make([]byte, peakDiskPayload, peakDiskPayload+sha256.Size)
	for i, p := range chunk.levels[0] {
		binary.LittleEndian.PutUint32(data[i*8:], math.Float32bits(p.min))
		binary.LittleEndian.PutUint32(data[i*8+4:], math.Float32bits(p.max))
	}
	sum := sha256.Sum256(data)
	data = append(data, sum[:]...)
	file, err := os.CreateTemp(dir, ".peak-partial-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), peakDiskPath(dir, key))
}
