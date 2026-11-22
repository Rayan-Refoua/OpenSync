package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const recentProjectLimit = 5

type recentProjects struct {
	mu    sync.Mutex
	paths []string
}

func recentProjectsPath() (string, error) {
	root, err := os.UserConfigDir()
	return filepath.Join(root, "OpenSync", "recent-projects.json"), err
}

func (s *Service) LoadRecentProjects() {
	path, err := recentProjectsPath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var paths []string
	if json.Unmarshal(data, &paths) == nil {
		s.recent.mu.Lock()
		defer s.recent.mu.Unlock()
		s.recent.paths = paths[:min(len(paths), recentProjectLimit)]
	}
}

func projectPeakDir(p *Project) string {
	if p == nil || p.Path == "" {
		return ""
	}
	path := p.Path
	if revision, ok := p.Graph["solution_revision"].(map[string]any); ok {
		if base, ok := revision["base_project"].(string); ok && base != "" {
			path = base
		}
	}
	return filepath.Join(filepath.Dir(path), "Waveform Peaks")
}

func (s *Service) RememberProject(p *Project) error {
	dir := projectPeakDir(p)
	if dir == "" {
		return nil
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	path, err := recentProjectsPath()
	if err != nil {
		return err
	}
	s.recent.mu.Lock()
	defer s.recent.mu.Unlock()
	if len(s.recent.paths) != 0 && strings.EqualFold(dir, s.recent.paths[0]) {
		return nil
	}
	next := []string{dir}
	for _, previous := range s.recent.paths {
		if len(next) < recentProjectLimit && !strings.EqualFold(dir, previous) {
			next = append(next, previous)
		}
	}
	s.recent.paths = next
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(parent, ".recent-projects-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(next)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (s *Service) recentPeakDirs() []string {
	s.recent.mu.Lock()
	defer s.recent.mu.Unlock()
	return append([]string(nil), s.recent.paths...)
}

func readRecentPeak(dirs []string, current, key string) *peakChunk {
	for _, dir := range dirs {
		if strings.EqualFold(dir, current) || !filepath.IsAbs(dir) {
			continue
		}
		if chunk := readPeakChunk(dir, key); chunk != nil {
			return chunk
		}
	}
	return nil
}
