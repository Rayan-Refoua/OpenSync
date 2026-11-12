package autosync

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"opensync/internal/researchmatch"
	"os/exec"
	"strings"
)

func openDistributedWorkspace(ctx context.Context, path string) (*researchmatch.DistributedFile, error) {
	if memoryWorkspace(ctx) == nil {
		return researchmatch.OpenDistributed(path)
	}
	data, err := workspaceRead(ctx, path)
	if err != nil {
		return nil, err
	}
	return researchmatch.OpenDistributedReader(bytes.NewReader(data), int64(len(data)))
}

type boundedFeatureBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedFeatureBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, fmt.Errorf("distributed features exceed working memory limit; explicitly enable --cache for this input")
	}
	return b.Buffer.Write(p)
}
func distributedFeaturesForRecord(ctx context.Context, r MediaRecord, path string) error {
	if memoryWorkspace(ctx) == nil {
		return distributedFeatures(ctx, r.Cache, path)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	tool, err := mediaTool("ffmpeg")
	if err != nil {
		return err
	}
	// Preserve the original two-stage conversion: source -> mono PCM16/16k ->
	// float32/8k. Collapsing the conversion would change quantization and peaks.
	cmd := exec.CommandContext(ctx, tool, "-nostdin", "-v", "error", "-threads", "1", "-f", "s16le", "-ar", "16000", "-ac", "1", "-i", "pipe:0", "-ar", "8000", "-ac", "1", "-f", "f32le", "-")
	input, writer := io.Pipe()
	defer input.Close()
	cmd.Stdin = input
	var stderr strings.Builder
	cmd.Stderr = &stderr
	output, err := cmd.StdoutPipe()
	if err != nil {
		writer.Close()
		return err
	}
	if err = cmd.Start(); err != nil {
		writer.Close()
		return err
	}
	decoded := make(chan error, 1)
	go func() {
		var err error
		if r.Audio == nil {
			zero := make([]byte, 32000)
			remaining := roundFrame(r.Duration*16000) * 2
			for remaining > 0 && err == nil {
				n := min(remaining, int64(len(zero)))
				_, err = writer.Write(zero[:n])
				remaining -= n
			}
		} else {
			total := roundFrame(r.Duration * 16000)
			for first := int64(0); first < total && err == nil; first += 50 * 16000 {
				var pcm []int16
				pcm, err = memoryWorkspace(ctx).audio.read(ctx, r, first, min(int64(50*16000), total-first))
				if err != nil {
					break
				}
				raw := make([]byte, len(pcm)*2)
				for i, v := range pcm {
					binary.LittleEndian.PutUint16(raw[2*i:], uint16(v))
				}
				_, err = writer.Write(raw)
			}
		}
		writer.CloseWithError(err)
		decoded <- err
	}()
	data := &boundedFeatureBuffer{limit: 64 << 20}
	featureErr := researchmatch.WriteDistributed(output, data, 1)
	if featureErr != nil {
		cancel()
		input.CloseWithError(featureErr)
	} else {
		_, featureErr = io.Copy(io.Discard, output)
	}
	waitErr := cmd.Wait()
	input.Close()
	decodeErr := <-decoded
	if featureErr != nil {
		return featureErr
	}
	if decodeErr != nil {
		return decodeErr
	}
	if waitErr != nil {
		return fmt.Errorf("distributed conversion: %w: %s", waitErr, stderr.String())
	}
	return workspacePut(ctx, path, data.Bytes())
}
