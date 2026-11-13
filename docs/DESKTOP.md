# Desktop workstation

The first desktop slice uses Wails v2, vanilla JavaScript, Canvas 2D and Web Audio.
The nested `desktop` Go module isolates GUI dependencies from the Go 1.23
engine. Wails 2.11.0 is pinned: the 2.17.1 Go 1.26 toolchain download was
unavailable (HTTP 403) on the build host. No CLI subprocess, local TCP server, Python,
or WebSocket service is needed. Wails' in-process asset handler carries binary
waveform, thumbnail and audition data; bound Go methods handle application actions.

Two binaries share `opensync/internal/autosync`: `opensync.exe` (desktop) and
`opensync-cli.exe` (existing CLI). The existing CLI source and command syntax stay
compatible. FFmpeg/FFprobe are the only external media tools.

## Presentation contract

- Load through the engine's integrity-checking `.opensync` reader.
- Keep exact binary64 timing and timing knots; do not quantize the project to a
  display frame rate. Frame stepping is a navigation aid.
- Present disconnected components at the end with explicit unresolved labels.
- Distinguish evidence-supported, validated, provisional and unresolved states.
  Evidence-supported does not mean independently certified.
- Put overlaps on separate lanes; never silently trim or stretch video.
- Visualization is read-only. Waveform/thumbnail caches are bounded, in memory,
  separate from analysis evidence, and never persisted by default.
- Render only the visible time and track ranges. Use power-of-two waveform
  levels and at most 512-bin tiles rather than giant full-timeline canvases.
  Overview tiles shrink their bin count to cap decode spans near 120 seconds,
  so long recordings render progressively rather than monopolizing a worker.
- Playback auditions at most two selected lanes against one Web Audio clock.
  Decode short bounded blocks, map source time through project timing knots,
  and stop explicitly on underrun instead of allowing silent desynchronization.

Framework evaluation was bounded to API/build feasibility. Wails/Canvas is a
direct fit; a second renderer, CEF bundle or custom bridge is not justified for
this milestone. Official reference: https://v2.wails.io/docs/guides/dynamic-assets/

## Build and run (Windows)

Use Go 1.23+, FFmpeg/FFprobe on PATH, and the installed WebView2 Runtime.
Node is used for frontend tests only; no npm install or frontend bundler is
required to build or run the app.

```
./tools/build_desktop_windows.ps1 -Output C:\OpenSync-Desktop
C:\OpenSync-Desktop\opensync.exe "C:\Projects\OpenSync.opensync"
C:\OpenSync-Desktop\opensync-cli.exe sync --folders "C:\Recordings" --output "C:\NewProject"
```

The build script adds a per-monitor DPI manifest and includes external media
tools and dependency license notices. No installer or file association is
created yet. Existing CLI-only packaging remains available separately.

## First slice controls

- **Open project** / Ctrl+O: load the existing project with integrity checking.
- **Add files / Add folder → Synchronize**: call the Go engine directly. Select
  a parent output directory; a new timestamped project folder is created.
- **Locate media**: resolve missing media under a selected directory using
  unique names and source byte sizes. Relinking is session-only in this slice.
- Ctrl+wheel zooms at the pointer; Shift+wheel and the horizontal scrollbar pan.
  Middle-drag / Alt-drag pan in both axes; wheel scrolls lanes.
- F fits the project; +/- zoom; double-click focuses a clip; click seeks/selects.
- Space plays/stops, Esc stops, arrows step the chosen frame grid, Shift+arrows
  step one second, Home/End seek timeline boundaries.
- ON/OFF selects at most two lanes for audition. Audio uses the precise model,
  including affine and piecewise timing, at 48 kHz stereo. Both lanes share one
  clock and are averaged to leave mixing headroom.
- The inspector shows exact placement, fit residual, clock-rate difference,
  component identity, source metadata and raw pair evidence. Raw evidence is
  retained rather than converted into an invented confidence percentage.

## Live updates and limits

Stages and worker activity refresh during synchronization. Inventory appears
after probing; pair verification results update clip activity; the first solved
layout is explicitly provisional; the completed project replaces it only after
the engine finishes. Intermediate pair support is not final graph acceptance.
The activity log is bounded to 200 frontend / 256 backend entries.

This is a read-only inspection/audition workstation with engine launch controls,
not yet a manual timeline editor. New sync runs already save Premiere XML through
the engine. There is no separate GUI export-settings dialog yet. Video frames
are representative thumbnails, not synchronized moving-video playback. Source
relinking is not saved back to the project. Slow storage can cause audition to
stop with an explicit underrun message; it never lets one lane drift silently.

Peak and thumbnail requests have separate bounded pools; audio audition has
reserved slots. The backend visualization cache is capped at 64 MiB; frontend
peaks and decoded images each have a 24 MiB budget. Audio blocks are at most four
seconds and at most two lanes. WebView2 itself has additional process/GPU memory
and its normal runtime profile; these budgets describe media visualization data.

## Validation (2026-11-13)

- Real six-recording Zendegifakher project loaded through the production reader;
  original MP4 waveforms and thumbnails rendered; stereo audition decoded and
  the playback clock advanced through block boundaries.
- Hidden native Wails/WebView2 launch confirmed frontend bindings, real-project
  loading and successful dynamic waveform/thumbnail requests. No desktop UI
  automation is needed for this diagnostic (`OPENSYNC_DESKTOP_SMOKE` names an
  optional output JSON; unset during ordinary use).
- Automated tests cover fractional and piecewise mappings, separate overlap
  lanes, unresolved placement, stereo antiphase peaks, delayed audio origin at
  several seeks, stale-project requests, cancellation and bounded cache eviction.
- Renderer harness: 500 and 5,000 clips, overview and detail navigation. The
  final 5,000-clip overview showed 1,500 visible clips with 13.4 ms p95 draw
  time; detail showed 96 visible clips with 2.0 ms p95. These are headless Chromium
  CPU render measurements using synthetic peaks, not a guarantee of native
  WebView2 presentation FPS or an audible speaker review.

Reproduce core tests with `node --test desktop/frontend/core.test.js` and
`OPENSYNC_INTEGRATION=1 go test ./internal/desktop`. The optional development
preview server (`go run ./tools/desktop-preview --project ...`) binds loopback
on an ephemeral port; `tools/test_desktop.mjs` drives an isolated headless browser
against it. This server is not part of the shipped executable architecture.
