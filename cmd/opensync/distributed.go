package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"opensync/internal/researchmatch"
)

func distributedCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("distributed requires features or search")
	}
	flags := flag.NewFlagSet("distributed "+args[0], flag.ContinueOnError)
	input := flags.String("input", "", "source media or cached WAV")
	output := flags.String("output", "", "output path")
	channels := flags.Int("channels", 1, "number of source channels to preserve")
	ref := flags.String("reference", "", "reference feature file")
	query := flags.String("query", "", "query feature file")
	omitBand := flags.Int("omit-band", -1, "diagnostic band ablation (0..4)")
	omitSection := flags.Int("omit-section", -1, "diagnostic temporal ablation")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *output == "" {
		return fmt.Errorf("output is required; unexpected arguments are not accepted")
	}
	outAbs, err := filepath.Abs(*output)
	if err != nil {
		return err
	}
	for _, p := range []string{*input, *ref, *query} {
		if p != "" {
			a, e := filepath.Abs(p)
			if e != nil {
				return e
			}
			if strings.EqualFold(a, outAbs) {
				return fmt.Errorf("output cannot overwrite an input")
			}
		}
	}
	if err = os.MkdirAll(filepath.Dir(outAbs), 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(outAbs), "distributed-*.partial")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	switch args[0] {
	case "features":
		if *input == "" || *channels < 1 || *channels > 8 {
			return fmt.Errorf("source and 1..8 preserved channels are required")
		}
		ffmpeg := "ffmpeg"
		if executable, e := os.Executable(); e == nil {
			p := filepath.Join(filepath.Dir(executable), "runtime", "bin", "ffmpeg.exe")
			if _, e = os.Stat(p); e == nil {
				ffmpeg = p
			}
		}
		command := exec.Command(ffmpeg, "-nostdin", "-v", "error", "-i", *input, "-map", "0:a:0", "-vn", "-ar", "8000", "-ac", fmt.Sprint(*channels), "-f", "f32le", "-")
		command.Stderr = os.Stderr
		pipe, e := command.StdoutPipe()
		if e != nil {
			return e
		}
		if e = command.Start(); e != nil {
			return e
		}
		e = researchmatch.WriteDistributed(pipe, temp, *channels)
		if e != nil {
			command.Process.Kill()
			command.Wait()
			return e
		}
		// Drain any final decoded samples before waiting for the encoder.
		io.Copy(io.Discard, pipe)
		if e = command.Wait(); e != nil {
			return e
		}
	case "search":
		r, e := researchmatch.OpenDistributed(*ref)
		if e != nil {
			return e
		}
		defer r.Close()
		q, e := researchmatch.OpenDistributed(*query)
		if e != nil {
			return e
		}
		defer q.Close()
		result, e := researchmatch.SearchDistributed(r, q, *omitBand, *omitSection)
		if e != nil {
			return e
		}
		encoder := json.NewEncoder(temp)
		encoder.SetIndent("", "  ")
		if e = encoder.Encode(result); e != nil {
			return e
		}
	default:
		return fmt.Errorf("unknown distributed operation %q", args[0])
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), outAbs)
}
