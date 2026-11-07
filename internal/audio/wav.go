package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

type Info struct {
	SampleRate uint32
	Channels   uint16
	Bits       uint16
	Frames     uint64
	Bytes      uint64
}

// Inspect reads WAV headers only. It does not decode audio or video.
func Inspect(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()

	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return Info{}, err
	}
	if string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return Info{}, fmt.Errorf("unsupported WAV container: %s", path)
	}

	var out Info
	var format uint16
	var blockAlign uint16
	var foundFmt, foundData bool

	for {
		var chunk [8]byte
		_, err := io.ReadFull(f, chunk[:])
		if err == io.EOF {
			break
		}
		if err != nil {
			return Info{}, fmt.Errorf("incomplete WAV chunk: %w", err)
		}

		size := binary.LittleEndian.Uint32(chunk[4:])
		id := string(chunk[:4])

		switch id {
		case "fmt ":
			if size < 16 {
				return Info{}, fmt.Errorf("invalid fmt chunk")
			}
			var buf [16]byte
			if _, err := io.ReadFull(f, buf[:]); err != nil {
				return Info{}, err
			}
			format = binary.LittleEndian.Uint16(buf[0:])
			out.Channels = binary.LittleEndian.Uint16(buf[2:])
			out.SampleRate = binary.LittleEndian.Uint32(buf[4:])
			blockAlign = binary.LittleEndian.Uint16(buf[12:])
			out.Bits = binary.LittleEndian.Uint16(buf[14:])
			foundFmt = true
			if _, err := f.Seek(int64(size)-16, io.SeekCurrent); err != nil {
				return Info{}, err
			}
		case "data":
			out.Bytes += uint64(size)
			foundData = true
			if _, err := f.Seek(int64(size), io.SeekCurrent); err != nil {
				return Info{}, err
			}
		default:
			if _, err := f.Seek(int64(size), io.SeekCurrent); err != nil {
				return Info{}, err
			}
		}
		if size&1 != 0 {
			if _, err := f.Seek(1, io.SeekCurrent); err != nil {
				return Info{}, err
			}
		}
	}

	if !foundFmt || !foundData || format != 1 ||
		blockAlign == 0 || out.SampleRate == 0 ||
		out.Bytes%uint64(blockAlign) != 0 {
		return Info{}, fmt.Errorf("unsupported or invalid PCM WAV")
	}

	if uint32(blockAlign) !=
		uint32(out.Channels)*uint32(out.Bits)/8 {
		return Info{}, fmt.Errorf("inconsistent WAV block alignment")
	}

	out.Frames = out.Bytes / uint64(blockAlign)
	return out, nil
}
