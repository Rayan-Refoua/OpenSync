package featureindex

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"opensync/internal/audio"
	"opensync/internal/dsp"
)

const (
	HeaderSize = 88
	RecordSize = 24
	Version    = 1
)

const magic = "OSFIDX01"

var order = binary.LittleEndian

type Header struct {
	SourceFrames uint64
	Count        uint64
	SourceBytes  uint64
	SourceMTime  int64
	Digest       [32]byte
}

type Index struct {
	file   *os.File
	Header Header
}

func Build(source, destination string) (err error) {
	r, err := audio.Open(source)
	if err != nil {
		return err
	}
	defer r.Close()

	st, err := os.Stat(source)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(destination), ".osfi-*.tmp")
	if err != nil {
		return err
	}

	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	if _, err := tmp.Write(make([]byte, HeaderSize)); err != nil {
		return err
	}

	hash := sha256.New()
	writer := bufio.NewWriterSize(io.MultiWriter(tmp, hash), 256*1024)

	var count uint64
	var last int64 = -1

	err = dsp.Stream(r, 10*dsp.SampleRate, func(f dsp.Frame) error {
		if f.CenterSample <= last {
			return errors.New("nonmonotonic feature coordinates")
		}
		last = f.CenterSample

		var b [RecordSize]byte
		order.PutUint64(b[0:], uint64(f.CenterSample))
		order.PutUint32(b[8:], math.Float32bits(float32(f.Energy)))
		for i, value := range f.Flux {
			order.PutUint32(b[12+i*4:],
				math.Float32bits(float32(value)))
		}

		if _, err := writer.Write(b[:]); err != nil {
			return err
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}

	if err := writer.Flush(); err != nil {
		return err
	}

	var header [HeaderSize]byte
	copy(header[:8], magic)
	order.PutUint32(header[8:], Version)
	order.PutUint32(header[12:], dsp.SampleRate)
	order.PutUint32(header[16:], dsp.WindowSize)
	order.PutUint32(header[20:], dsp.HopSize)
	order.PutUint64(header[24:], uint64(r.Frames()))
	order.PutUint64(header[32:], count)
	order.PutUint64(header[40:], uint64(st.Size()))
	order.PutUint64(header[48:], uint64(st.ModTime().UnixNano()))
	copy(header[56:], hash.Sum(nil))

	if _, err := tmp.WriteAt(header[:], 0); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), destination)
}

// Open checks the complete feature payload before exposing random access.
func Open(path string) (_ *Index, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			f.Close()
		}
	}()

	st, err := f.Stat()
	if err != nil {
		return nil, err
	}

	var b [HeaderSize]byte
	if _, err := io.ReadFull(f, b[:]); err != nil {
		return nil, err
	}

	if string(b[:8]) != magic ||
		order.Uint32(b[8:]) != Version ||
		order.Uint32(b[12:]) != dsp.SampleRate ||
		order.Uint32(b[16:]) != dsp.WindowSize ||
		order.Uint32(b[20:]) != dsp.HopSize {
		return nil, errors.New("unsupported feature index format")
	}

	h := Header{
		SourceFrames: order.Uint64(b[24:]),
		Count:        order.Uint64(b[32:]),
		SourceBytes:  order.Uint64(b[40:]),
		SourceMTime:  int64(order.Uint64(b[48:])),
	}
	copy(h.Digest[:], b[56:])

	if h.Count > uint64((math.MaxInt64-HeaderSize)/RecordSize) ||
		st.Size() != HeaderSize+int64(h.Count)*RecordSize {
		return nil, errors.New("invalid feature index length")
	}

	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return nil, err
	}
	var actual [32]byte
	copy(actual[:], sum.Sum(nil))
	if actual != h.Digest {
		return nil, errors.New("feature index integrity failure")
	}

	return &Index{file: f, Header: h}, nil
}

func (idx *Index) Close() error {
	return idx.file.Close()
}

func (idx *Index) ValidFor(source string) (bool, error) {
	st, err := os.Stat(source)
	if err != nil {
		return false, err
	}
	return uint64(st.Size()) == idx.Header.SourceBytes &&
		st.ModTime().UnixNano() == idx.Header.SourceMTime, nil
}

func (idx *Index) ReadAt(position uint64) (dsp.Frame, error) {
	if position >= idx.Header.Count {
		return dsp.Frame{}, io.EOF
	}
	var b [RecordSize]byte
	offset := int64(HeaderSize) + int64(position)*RecordSize
	if _, err := idx.file.ReadAt(b[:], offset); err != nil {
		return dsp.Frame{}, err
	}

	position64 := order.Uint64(b[:8])
	if position64 > math.MaxInt64 {
		return dsp.Frame{}, fmt.Errorf("invalid sample coordinate")
	}

	out := dsp.Frame{
		CenterSample: int64(position64),
		Energy:       float64(math.Float32frombits(order.Uint32(b[8:]))),
	}
	for i := range out.Flux {
		out.Flux[i] = float64(math.Float32frombits(
			order.Uint32(b[12+i*4:])))
	}
	return out, nil
}
