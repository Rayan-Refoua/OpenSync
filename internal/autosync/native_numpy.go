package autosync

// Read-only compatibility for the previous engine's compressed array caches.
// The runtime writes native caches and never needs NumPy to open old projects.
import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type numpyArray struct {
	Kind  string
	Shape []int
	Data  []byte
}

var numpyDType = regexp.MustCompile(`'descr':\s*'([^']+)'`)
var numpyShape = regexp.MustCompile(`'shape':\s*\(([^)]*)\)`)

func readNumpyArchive(path string) (map[string]numpyArray, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	result := map[string]numpyArray{}
	for _, file := range z.File {
		if !strings.HasSuffix(file.Name, ".npy") {
			continue
		}
		if file.UncompressedSize64 > 1<<30 {
			return nil, fmt.Errorf("legacy array exceeds memory limit")
		}
		r, err := file.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(r, 1<<30+1))
		r.Close()
		if err != nil {
			return nil, err
		}
		if len(data) < 10 || !bytes.Equal(data[:6], []byte{0x93, 'N', 'U', 'M', 'P', 'Y'}) {
			return nil, fmt.Errorf("invalid legacy array header")
		}
		start, size := 10, int(binary.LittleEndian.Uint16(data[8:10]))
		if data[6] == 2 || data[6] == 3 {
			if len(data) < 12 {
				return nil, fmt.Errorf("truncated array header")
			}
			start = 12
			size = int(binary.LittleEndian.Uint32(data[8:12]))
		} else if data[6] != 1 {
			return nil, fmt.Errorf("unsupported array version")
		}
		if size < 0 || start+size > len(data) {
			return nil, fmt.Errorf("truncated legacy array")
		}
		header := string(data[start : start+size])
		kind, shape := numpyDType.FindStringSubmatch(header), numpyShape.FindStringSubmatch(header)
		if kind == nil || shape == nil || !strings.Contains(header, "'fortran_order': False") {
			return nil, fmt.Errorf("unsupported legacy array layout")
		}
		a := numpyArray{Kind: kind[1], Data: data[start+size:]}
		for _, part := range strings.Split(shape[1], ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			v, err := strconv.Atoi(part)
			if err != nil || v < 0 {
				return nil, fmt.Errorf("invalid legacy dimensions")
			}
			a.Shape = append(a.Shape, v)
		}
		result[strings.TrimSuffix(file.Name, ".npy")] = a
	}
	return result, nil
}
func (a numpyArray) text() (string, error) {
	if strings.HasPrefix(a.Kind, "<U") {
		if len(a.Data)%4 != 0 {
			return "", fmt.Errorf("invalid unicode array")
		}
		runes := []rune{}
		for i := 0; i < len(a.Data); i += 4 {
			r := rune(binary.LittleEndian.Uint32(a.Data[i:]))
			if r != 0 {
				runes = append(runes, r)
			}
		}
		return string(runes), nil
	}
	if strings.HasPrefix(a.Kind, "|S") {
		return strings.TrimRight(string(a.Data), "\x00"), nil
	}
	return "", fmt.Errorf("legacy text dtype %s", a.Kind)
}
func legacyFingerprints(path, digest string) ([]Fingerprint, error) {
	a, err := readNumpyArchive(path)
	if err != nil {
		return nil, err
	}
	d, err := a["digest"].text()
	if err != nil || d != digest {
		return nil, fmt.Errorf("stale legacy fingerprint digest")
	}
	v, err := a["version"].text()
	if err != nil || v != "opensync-autonomous-v1" {
		return nil, fmt.Errorf("unsupported legacy fingerprints")
	}
	h := a["hashes"]
	if h.Kind != "<i8" || len(h.Shape) != 2 || h.Shape[1] != 2 || len(h.Data) != h.Shape[0]*16 {
		return nil, fmt.Errorf("invalid legacy fingerprints")
	}
	out := make([]Fingerprint, h.Shape[0])
	for i := range out {
		out[i] = Fingerprint{int64(binary.LittleEndian.Uint64(h.Data[i*16:])), int64(binary.LittleEndian.Uint64(h.Data[i*16+8:]))}
	}
	return out, nil
}
func legacyEnvelope(path, digest string) ([3][]float32, error) {
	var out [3][]float32
	a, err := readNumpyArchive(path)
	if err != nil {
		return out, err
	}
	d, err := a["digest"].text()
	if err != nil || d != digest {
		return out, fmt.Errorf("stale envelope cache")
	}
	f := a["features"]
	if f.Kind != "<f4" || len(f.Shape) != 2 || f.Shape[0] != 3 || len(f.Data) != 3*f.Shape[1]*4 {
		return out, fmt.Errorf("invalid legacy envelope")
	}
	for b := range out {
		out[b] = make([]float32, f.Shape[1])
		for i := range out[b] {
			out[b][i] = math.Float32frombits(binary.LittleEndian.Uint32(f.Data[4*(b*f.Shape[1]+i):]))
		}
	}
	return out, nil
}
