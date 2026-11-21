// Embed the app icon under resource ID 3, the ID used by Wails window/taskbar
// icon loading. Explorer also uses this multi-resolution icon group.
package main

import (
	"github.com/tc-hib/winres"
	"os"
)

func main() {
	f, err := os.Open("app.ico")
	if err != nil {
		panic(err)
	}
	defer f.Close()
	ico, err := winres.LoadICO(f)
	if err != nil {
		panic(err)
	}
	r := winres.ResourceSet{}
	if err = r.SetIcon(winres.ID(3), ico); err != nil {
		panic(err)
	}
	b, err := os.ReadFile("app.manifest")
	if err != nil {
		panic(err)
	}
	m, err := winres.AppManifestFromXML(b)
	if err != nil {
		panic(err)
	}
	r.SetManifest(m)
	out, err := os.Create("rsrc_windows_amd64.syso")
	if err != nil {
		panic(err)
	}
	defer out.Close()
	if err = r.WriteObject(out, winres.ArchAMD64); err != nil {
		panic(err)
	}
}
