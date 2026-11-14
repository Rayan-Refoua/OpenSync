# OpenSync desktop workstation

The desktop uses Wails 2.11, vanilla JavaScript, Canvas 2D, Web Audio and native
WebView2 video. The nested `desktop` Go module isolates GUI dependencies from the
Go engine. `opensync.exe` (desktop) and `opensync-cli.exe` (headless) share the same
native engine. No CLI subprocess, Python runtime or WebSocket bridge is used.
FFmpeg/FFprobe remain external media tools.

## Build and run (Windows)

Requires Go 1.23+, FFmpeg/FFprobe on PATH, and WebView2 Runtime. Node is only for
development tests and artwork generation; no frontend bundler is required.

```
./tools/build_desktop_windows.ps1 -Output C:\OpenSync-Desktop
C:\OpenSync-Desktop\opensync.exe "C:\Projects\OpenSync.opensync"
C:\OpenSync-Desktop\opensync-cli.exe sync --folders "C:\Recordings" --output "C:\NewProject"
```

The build script includes the user-supplied SVG-derived Windows icon, DPI
manifest, external media tools and dependency license notices. This is a portable
build; installer and file associations are not implemented yet.

## Presentation and controls

- Projects load through the integrity-checking engine reader. Binary64 timing,
  timing knots, source media and synchronization evidence remain unchanged.
  Display frame stepping never quantizes the stored project.
- Open project / Ctrl+O loads a saved project. Add files or recursively add a
  folder, then Synchronize to choose an output parent and call Go directly.
  Disk caching remains off. Open output folder reveals the saved project/XML.
- Locate media searches a chosen folder for unique filename/size matches.
  Relinking is session-only; no original file is moved or modified.
- Ctrl+wheel zooms at the pointer. Shift+wheel/horizontal scrollbar pan;
  middle/Alt-drag or H pans freely. V selects; double-click focuses a clip.
  Backslash fits the project, F focuses the selection, +/- zoom.
- Space plays/pauses, K/Esc stops, J/L shuttle backward/forward (1/2/4x), arrows
  step one display frame, Shift+arrows five frames, Up/Down visit edit boundaries,
  Home/End visit sequence bounds. Drag the playhead or ruler to scrub.
  Settings / Ctrl+Alt+K configures shortcuts; tooltips/reference stay in sync.
- At most two selected lanes are auditioned at 48 kHz stereo through the precise
  affine/piecewise timing model. Mixing is averaged for headroom.
- Supported, validated, provisional and unresolved placements retain distinct
  labels/colors. Selection uses a separate pale outline. Raw engine status,
  residual, clock rate, timing knots, source metadata and evidence remain in the
  inspector. Acoustic support never means independent capture-time certification.
- The header retains a color-coded state report. During sync it shows the stage,
  pair counts where available and next stage. Completion reports placement and
  offline counts with a next-step hint. The bar represents pipeline stages,
  not an invented ETA or work percentage.
- Inventory and provisional layouts appear during sync; pair activity updates
  clips. Completed projects replace provisional views after export. Diagnostics
  have severity filtering and links to recordings. Panels resize/collapse.

This is a synchronization/review workstation, not yet a manual timeline editor.
There is no separate GUI export-settings dialog; engine runs save Premiere XML.

## Waveforms and thumbnails

Only visible time/track ranges render. A reusable 8 ms min/max peak pyramid is
built in 32.768-second source chunks through bounded workers. Zoom levels and
project reopening reuse completed chunks by source path/size/modification time.
Navigation cancels obsolete viewport requests; admitted source chunks finish so
rapid navigation does not repeatedly restart the same useful decode. Changing
projects cancels active work. No visualization data contributes sync evidence.

PNG thumbnails replace the failing MJPEG path. End requests respect the video
stream's duration rather than a longer audio/container duration. Bounded earlier
frame fallback handles VFR last-frame gaps; unrelated errors remain visible.

## Multicamera monitor

Four feeds default to the right in one column/four rows. Settings retain the
above-timeline layout and configure rows, columns (including auto expansion), and
up to 16 feeds. Pinning prefers a camera when active; out-of-bounds cameras are
omitted. Surviving video elements retain decoders when another recording ends.

Native video preserves source FPS and uses hardware decoding where supported.
Unsupported native formats can use bounded 20-second, 640-pixel, 25-fps software
previews, explicitly labeled. Preview FPS never changes source/project/XML timing.
A shared buffering barrier holds audio/timeline playback while a camera catches
up; timeout or failure stops explicitly. Reverse shuttle samples frames by seek.
This is a review monitor, not a genlocked output. Four-feed playback is tested;
sixteen-feed performance is not certified.

Original video responses are bounded to 4 MiB byte ranges. This prevents Wails'
Windows buffered response writer retaining multi-GB media, and prevents long
HTTP responses starving new camera requests. The frontend/engine still communicate
through Wails' in-process bindings and asset handler.

## Resource management and storage

CPU/RAM indicators measure OpenSync and its child processes, with 60 CPU samples.
Settings apply Go worker concurrency, a soft Go memory target, bounded analysis
and visualization working sets, waveform/playback/thumbnail pools and background
intensity. Browser/GPU allocations are not a hard total-RAM cap. Process-tree
working-set sums may include shared pages. No persistent media cache is enabled.

Windows volume extents identify physical disks across partitions/drive letters;
StorageDeviceSeekPenaltyProperty identifies seeking storage. Each HDD gets one
active source-read lease independent of other HDDs. Confirmed SSDs retain direct
concurrent access. Unknown/network/virtual storage is labeled unknown and uses a
conservative per-volume lease, never an invented HDD classification.

For external decoders on serialized storage, a private token-scoped loopback byte
source lets Go schedule up to 4 MiB contiguous reads without restarting FFmpeg or
changing its resampler origin. Only context-registered originals are accessible;
there is no arbitrary path endpoint, directory listing, conversion or disk copy.
Read-ahead buffers are bounded at 32 MiB. Leases end before CPU/network consumption,
so an idle decoder cannot hold the HDD. Requests between files still require
seeks; fragmented files are not magically made physically contiguous. This is a
source-I/O adapter, not an engine/GUI or CLI bridge.

On the tested host D: and G: are distinct HDDs; C:/E:/F:/H: report SSD behavior.
R:'s virtual driver exposes neither extents nor a seek-penalty descriptor and
therefore uses the unknown-volume policy despite its RamDisk label. A volume
label alone is not evidence of hardware. I: was unavailable during this check.
No whole-project HDD speedup is claimed without a representative benchmark.

## Validation (2026-11-14)

Ten real files were used read-only: nine H.264/HEVC videos including long CCTV
and variable-frame-rate captures, plus ADTS AAC audio. All ten produced waveforms;
all 27 start/middle/end thumbnail checks passed. Five zoom changes over a warmed
chunk required zero additional chunk decodes. Nine videos passed native seeking
in isolated Chrome. Four-feed playback passed clip-end replacement, reverse
shuttle, reopening, layout settings and playhead dragging with no JavaScript
errors. One sampled four-feed frame error was within about 16 ms of the common
clock; this is a sample, not a worst-case native WebView2 guarantee.

The artificial media-subsystem fixture is explicitly provisional and is not a
synchronized production project. Synthetic 500/5,000-clip tests use synthetic
peaks and measure Canvas work separately from native presentation FPS.

Whole-file and trimmed PCM were byte-identical between file and scheduled-source
FFmpeg inputs for WAV/AAC/MP3. Storage tests cover same-disk partitions, independent
disks, cancellation, bounded classification and source ranges. Integration tests
retain original engine PCM/fingerprint parity and timing/export checks.

```
go test ./...
go vet ./...
node --test desktop/frontend/core.test.js desktop/frontend/monitor.test.js desktop/frontend/status.test.js
# In desktop/: go test ./...; go vet ./...
# Set OPENSYNC_INTEGRATION=1 for FFmpeg integration tests.
```

Development-only `tools/desktop-preview`, `tools/test_workstation.mjs` and
`tools/test_desktop.mjs` exercise real assets through an isolated headless browser,
without controlling the user's desktop. `OPENSYNC_DESKTOP_SMOKE` names an optional
hidden native launch diagnostic file; unset during ordinary use.
