package sourcetracks

import (
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"opensync/internal/projectstate"
)

var numberedFile = regexp.MustCompile(`^(.*?)([0-9]+)(\.[^.]+)$`)

type sequenceFile struct {
	id, directory, family string
	number, width         int
}

// CrossDirectorySeams proposes possible consecutive filenames across
// directory boundaries. It never asserts common device or continuity.
func CrossDirectorySeams(p *projectstate.Project) ([]Neighbor, error) {
	if p == nil {
		return nil, projectstate.ErrInvalid
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}

	files := make([]sequenceFile, 0, len(p.Recordings))
	for id, rec := range p.Recordings {
		source := normalizedSource(rec.SourcePath)
		name := path.Base(source)
		m := numberedFile.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		files = append(files, sequenceFile{
			id:        id,
			directory: strings.ToLower(path.Dir(source)),
			family:    strings.ToLower(m[1] + "|" + m[3]),
			number:    n,
			width:     len(m[2]),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		a, b := files[i], files[j]
		if a.family != b.family {
			return a.family < b.family
		}
		if a.width != b.width {
			return a.width < b.width
		}
		if a.number != b.number {
			return a.number < b.number
		}
		return a.id < b.id
	})

	var seams []Neighbor
	for i, a := range files {
		for j := i + 1; j < len(files); j++ {
			b := files[j]
			if b.family != a.family || b.width != a.width ||
				b.number > a.number+1 {
				break
			}
			if b.number != a.number+1 ||
				a.directory == b.directory {
				continue
			}
			seams = append(seams, Neighbor{
				TrackID:    "",
				Before:     a.id,
				After:      b.id,
				Basis:      "cross_directory_consecutive_filename",
				Continuity: "unknown",
			})
		}
	}
	if seams == nil {
		seams = []Neighbor{}
	}
	return seams, nil
}
