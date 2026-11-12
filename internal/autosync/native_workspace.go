package autosync

// Analysis state is run-local by default. Only an explicitly enabled cache may
// persist it. Keeping this policy below the individual DSP stages prevents a new
// stage from accidentally turning the project output directory into a cache.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
)

type workspaceKey struct{}
type nativeWorkspace struct {
	mu                                                   sync.Mutex
	data                                                 map[string][]byte
	used, limit                                          int64
	audio                                                *directAudio
	indexActive, indexPeak, indexCompleted, indexWorkers atomic.Int64
}

func memoryWorkspace(ctx context.Context) *nativeWorkspace {
	w, _ := ctx.Value(workspaceKey{}).(*nativeWorkspace)
	return w
}
func workspaceRead(ctx context.Context, path string) ([]byte, error) {
	if w := memoryWorkspace(ctx); w != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		if data, ok := w.data[filepath.Clean(path)]; ok {
			return data, nil
		}
		return nil, &os.PathError{Op: "read", Path: path, Err: os.ErrNotExist}
	}
	return os.ReadFile(path)
}
func workspaceExists(ctx context.Context, path string) bool {
	if w := memoryWorkspace(ctx); w != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		_, ok := w.data[filepath.Clean(path)]
		return ok
	}
	_, err := os.Stat(path)
	return err == nil
}
func workspaceMkdir(ctx context.Context, path string, mode os.FileMode) error {
	if memoryWorkspace(ctx) != nil {
		return nil
	}
	return os.MkdirAll(path, mode)
}
func workspacePut(ctx context.Context, path string, data []byte) error {
	if w := memoryWorkspace(ctx); w != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		path = filepath.Clean(path)
		next := w.used - int64(len(w.data[path])) + int64(len(data))
		if next > w.limit {
			return fmt.Errorf("analysis working memory limit reached (%d MiB); use a smaller input set or explicitly enable --cache", w.limit>>20)
		}
		w.data[path] = data
		w.used = next
		return nil
	}
	temp := path + ".partial"
	if err := os.WriteFile(temp, data, 0600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}
func workspaceJSON(ctx context.Context, path string, v any) error {
	if memoryWorkspace(ctx) == nil {
		return writeJSON(path, v)
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return workspacePut(ctx, path, data)
}
func workspaceGlob(ctx context.Context, pattern string) ([]string, error) {
	if w := memoryWorkspace(ctx); w != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		result := []string{}
		for path := range w.data {
			match, err := filepath.Match(pattern, path)
			if err != nil {
				return nil, err
			}
			if match {
				result = append(result, path)
			}
		}
		sort.Strings(result)
		return result, nil
	}
	return filepath.Glob(pattern)
}
func workspaceForget(ctx context.Context, path string) {
	if w := memoryWorkspace(ctx); w != nil {
		w.mu.Lock()
		defer w.mu.Unlock()
		path = filepath.Clean(path)
		w.used -= int64(len(w.data[path]))
		delete(w.data, path)
	}
}
