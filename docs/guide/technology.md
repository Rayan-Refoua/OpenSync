# Technology and code map

| Area | Implementation | Start here |
| --- | --- | --- |
| Engine and CLI | Go 1.23 | `internal/autosync/`, `cmd/opensync/` |
| Signal processing | Go FFT and feature code | `internal/dsp/`, `internal/featureindex/` |
| Timing graph | Go graph and project state | `internal/syncgraph/`, `internal/measuredgraph/`, `internal/projectstate/` |
| Desktop | Wails v2, JavaScript, DOM, Canvas and CSS | `desktop/`, `internal/desktop/` |
| Media tools | FFmpeg and FFprobe | `internal/autosync/media_process.go`, `internal/desktop/` |
| Native project | JSON-based `.opensync` project and solution revisions | `native_project.go`, `native_solution.go` |
| Premiere export | FCP7 XML | `internal/premierexml/`, `native_export.go` |
| Research and automation | Go benchmarks and Python developer scripts | `docs/`, `tools/`, `tests/` |

The shipped synchronization engine runs in Go. Python source remains in the repository for research and development workflows. FFmpeg tools are external dependencies in the public alpha. Experimental CGO library work is separately gated.

For Windows development, `Build and Open OpenSync.cmd` builds and opens the app. Add `--debug-ui` to launch the same executable with WebView2 DevTools, or choose **Restart with GUI debug** in Workstation settings. `python tools/build_open_windows.py` builds the packaged release. See `AGENTS.md` and the scripts for current options.

## Documentation site

Install the site dependencies with `npm ci`, then run `npm run docs:dev` for local development or `npm run docs:build` for the static output in `docs/.vitepress/dist`. The Markdown files can also be opened directly as an Obsidian vault by selecting the `docs` folder.

The documentation workflow builds the site on pushes to `main` and deploys it with GitHub Pages. The site uses the `/OpenSync/` base path for the repository site. A repository administrator must configure GitHub Pages to use GitHub Actions before that deployment can become public.

The project is GPL-3.0-or-later. Release packaging must account for the license and configuration of bundled FFmpeg binaries and libraries.
