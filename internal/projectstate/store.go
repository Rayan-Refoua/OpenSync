package projectstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const MaxProjectBytes int64 = 64 << 20

// Validate checks the complete object graph, including foreign keys.
// It never mutates the project.
func (p *Project) Validate() error {
	if p == nil || p.Version != 1 ||
		p.Recordings == nil ||
		p.Hypotheses == nil ||
		p.Observations == nil {
		return fmt.Errorf("%w: project schema", ErrInvalid)
	}

	check := New()

	for id, r := range p.Recordings {
		if id != r.ID {
			return fmt.Errorf("%w: recording key mismatch", ErrInvalid)
		}
		if err := check.AddRecording(r); err != nil {
			return err
		}
	}

	for id, h := range p.Hypotheses {
		if id != h.ID {
			return fmt.Errorf("%w: hypothesis key mismatch", ErrInvalid)
		}
		if err := check.AddHypothesis(h); err != nil {
			return err
		}
	}

	for id, o := range p.Observations {
		if id != o.ID {
			return fmt.Errorf("%w: observation key mismatch", ErrInvalid)
		}
		if err := check.AddObservation(o); err != nil {
			return err
		}

		h := p.Hypotheses[o.HypothesisID]
		ref := p.Recordings[h.Reference]
		query := p.Recordings[h.Query]

		if o.ReferenceNS > ref.DurationNS ||
			o.QueryNS > query.DurationNS {
			return fmt.Errorf(
				"%w: observation outside recording", ErrInvalid,
			)
		}
	}

	return nil
}

// Save validates and writes a complete project using a temporary file
// in the destination directory, then renames it into place.
// Failed writes leave the previously published project untouched.
// This does not protect against two processes saving concurrently.
func Save(path string, p *Project) error {
	if err := p.Validate(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if int64(len(data)) > MaxProjectBytes {
		return fmt.Errorf("project exceeds storage limit")
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".opensync-*.tmp")
	if err != nil {
		return err
	}

	name := tmp.Name()
	defer os.Remove(name)

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return os.Rename(name, path)
}

// Load rejects oversized, malformed, unsupported or structurally
// inconsistent projects. Unknown fields and trailing JSON are errors.
func Load(path string) (*Project, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, MaxProjectBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxProjectBytes {
		return nil, fmt.Errorf("project exceeds storage limit")
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	var p Project
	if err := dec.Decode(&p); err != nil {
		return nil, err
	}

	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing project data", ErrInvalid)
	}

	if err := p.Validate(); err != nil {
		return nil, err
	}

	return &p, nil
}
