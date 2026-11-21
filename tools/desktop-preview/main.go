// desktop-preview is a development-only renderer harness. The shipped desktop
// uses Wails' in-process asset handler and does not start an HTTP listener.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"opensync/internal/autosync"
	"opensync/internal/desktop"
	"os"
	"path/filepath"
)

func main() {
	project := flag.String("project", "", "real .opensync project")
	assets := flag.String("assets", "desktop/frontend", "frontend directory")
	manifest := flag.String("manifest", "", "read-only media manifest for explicitly provisional GUI test layout")
	writeProject := flag.String("write-project", "", "optional dedicated test project output")
	flag.Parse()
	s := desktop.NewService()
	defer s.Close()
	if *project != "" {
		p, e := desktop.LoadProject(*project)
		if e != nil {
			panic(e)
		}
		s.SetProject(p)
	}
	if *manifest != "" {
		var m struct {
			Files []struct {
				Path         string  `json:"path"`
				Duration     float64 `json:"duration"`
				Video, Audio []map[string]any
			}
		}
		b, e := os.ReadFile(*manifest)
		if e != nil {
			panic(e)
		}
		if e = json.Unmarshal(b, &m); e != nil {
			panic(e)
		}
		records := []autosync.MediaRecord{}
		models := []autosync.TimeModel{}
		indices := []int{}
		for i, f := range m.Files {
			st, e := os.Stat(f.Path)
			if e != nil {
				panic(e)
			}
			r := autosync.MediaRecord{ID: fmt.Sprintf("validation-%d", i), Name: filepath.Base(f.Path), Source: f.Path, LocalSource: f.Path, Duration: f.Duration, SourceBytes: st.Size(), SourceMtime: st.ModTime().UnixNano(), SourceGroup: filepath.Dir(f.Path)}
			if len(f.Video) > 0 {
				r.Video = f.Video[0]
			}
			if len(f.Audio) > 0 {
				r.Audio = f.Audio[0]
			}
			records = append(records, r)
			models = append(models, autosync.TimeModel{Recording: i, Component: 1, ComponentSize: len(m.Files), Rate: 1, Resolved: true, Status: "provisional_media_validation_ONLY_not_synchronized"})
			indices = append(indices, i)
		}
		payload := map[string]any{"name": "Media subsystem test — PROVISIONAL, NOT SYNCHRONIZED", "engine_version": "GUI media validation fixture", "recordings": records, "timing_models": models, "graph": map[string]any{"components": []any{indices}}, "validated_relations": []any{}}
		b, e = json.Marshal(payload)
		if e != nil {
			panic(e)
		}
		var raw any
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		if e = d.Decode(&raw); e != nil {
			panic(e)
		}
		b, e = json.Marshal(raw)
		if e != nil {
			panic(e)
		}
		hash := sha256.Sum256(b)
		native := autosync.NativeProject{Format: "opensync.project", Schema: 1, Payload: b, Hash: hex.EncodeToString(hash[:])}
		if *writeProject != "" {
			b, e = json.MarshalIndent(native, "", "  ")
			if e != nil {
				panic(e)
			}
			if e = os.WriteFile(*writeProject, b, 0600); e != nil {
				panic(e)
			}
			p, e := desktop.LoadProject(*writeProject)
			if e != nil {
				panic(e)
			}
			s.SetProject(p)
		} else {
			s.SetProject(desktop.BuildProject(autosync.NativePayload{Name: payload["name"].(string), Records: records, Models: models}))
		}
	}
	mux := http.NewServeMux()
	mux.Handle("/media/", s.Handler())
	mux.HandleFunc("/test/project", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Project())
	})
	mux.HandleFunc("/test/stats", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Stats())
	})
	mux.Handle("/", http.FileServer(http.Dir(*assets)))
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		panic(e)
	}
	address := "http://" + l.Addr().String()
	fmt.Println(address)
	if out := os.Getenv("OPENSYNC_PREVIEW_ADDRESS"); out != "" {
		abs, _ := filepath.Abs(out)
		if e = os.WriteFile(abs, []byte(address), 0600); e != nil {
			panic(e)
		}
	}
	if e = http.Serve(l, mux); e != nil {
		panic(e)
	}
}
