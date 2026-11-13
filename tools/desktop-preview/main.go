// desktop-preview is a development-only renderer harness. The shipped desktop
// uses Wails' in-process asset handler and does not start an HTTP listener.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"opensync/internal/desktop"
	"os"
	"path/filepath"
)

func main() {
	project := flag.String("project", "", "real .opensync project")
	assets := flag.String("assets", "desktop/frontend", "frontend directory")
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
	mux := http.NewServeMux()
	mux.Handle("/media/", s)
	mux.HandleFunc("/test/project", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Project())
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
