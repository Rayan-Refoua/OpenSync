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
  levels and fixed 512-bin tiles rather than giant full-timeline canvases.
- Playback auditions at most two selected lanes against one Web Audio clock.
  Decode short bounded blocks, map source time through project timing knots,
  and stop explicitly on underrun instead of allowing silent desynchronization.

Framework evaluation was bounded to API/build feasibility. Wails/Canvas is a
direct fit; a second renderer, CEF bundle or custom bridge is not justified for
this milestone. Official reference: https://v2.wails.io/docs/guides/dynamic-assets/
