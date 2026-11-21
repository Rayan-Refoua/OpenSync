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
Project actions can export the currently opened result to a new Premiere XML
folder. Unresolved clips are appended; advanced export settings remain in the CLI.

## Review workflow (2026-11-16)

- Choose Clips / takes (default) or Chronological event in the header. The latter
  enables bounded, explicitly provisional metadata estimates. Changing the selector
  affects the next sync or explicit re-evaluation, not an already opened timeline.
- Next attention visits unresolved/provisional recordings. The inspector can reject
  a recording's placement or reject a particular relationship, and restore either.
- Project actions → Re-evaluate saved evidence solves the saved relationships in
  the selected mode without extracting audio. It retains manual rejections and
  conservatively drops old provisional acoustic proposals whose graph may change.
  It cannot discover hypotheses missing from the saved evidence; use Resynchronize
  current recordings for fresh analysis, which automatically carries corrections.
- Review changes create immutable `.opensync` revisions in `Solutions` beside the
  original project. Undo opens the parent revision; Solution history opens any
  saved revision or the original. Keep the original project and Solutions folder
  together: revisions reference its integrity-checked evidence rather than copying it.
- Export current result saves a new XML from the opened revision. Open output folder
  opens the original project directory, including Solutions. Rejections are explicit
  project state, not a hidden global rule affecting unrelated new projects.
- Corrections bind to recording IDs and source size/modification time. Fresh analysis
  refuses corrections for missing/changed sources instead of silently applying them.
- Playback displays separate audio/video buffer reserves. New seeks supersede an
  unfinished seek as soon as video metadata is available; an old seek cannot block
  playback merely because its target has already been removed from the RAM buffer.

Validation on the six original Zendegifakher recordings: fresh no-cache sync took
14.77 seconds (five supported, one unresolved). An isolated Chromium test exercised
rapid forward/backward seeks in the actual synchronized project, including the
long Office_ch5 video and two simultaneous active cameras. Startup took 2.68/3.05
seconds; each subsequent 20-second sample had zero rebuffering, corrective seeks,
or catching-up overlays, with at least 7.89 seconds audio and 10.67 seconds video
reserve. This is bounded playback evidence on this workstation, not a guarantee
for every codec/storage combination or a Premiere import certification.

`tools/test_seek_playback.mjs` reproduces the obsolete-seek regression against the
desktop-preview server. `tools/test_review_controls.mjs` checks UI wiring with
mocked native bindings; Go `TestSolution*` tests separately exercise real revision
creation, restore, source guards, finalization, export, and integrity failures.

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

The monitor uses an explicit MediaSource queue of H.264 display segments in RAM.
It fills at least eight seconds of video/audio before starting, replenishes video
toward twelve seconds ahead, and schedules audio toward eight seconds. A five-second
reserve (with a small scheduling margin) triggers a coordinated refill if necessary.
Targets are measured in playback seconds and adapt to speed and actual clip ends.
Small timing errors use bounded rate correction rather than repeated seeking;
only sustained larger errors require a shared realignment. Reverse shuttle remains
seek-based frame sampling rather than continuous reverse video decoding.

Previews are 640×360, generated by FFmpeg through the bounded media worker pool;
the browser decodes them with hardware support where available. Source frame times,
including 50 fps and VFR, are retained. Guarded segment overlaps and an explicit
first-frame timestamp preserve source-time positioning across segment boundaries.
The preview label makes the reduced resolution visible. Original media, sync
models and Premiere exports are unchanged. No preview files are written to disk.

Each feed has at most one in-flight 12-second segment; old buffered ranges are
removed, and every encoded response has a 16 MiB ceiling. The browser's media/GPU
allocations still fall outside a strict whole-process RAM cap. The original-byte
endpoint remains bounded for direct-source inspection; the monitor does not rely
on its short byte ranges or the browser's paused-video preload heuristic.

A decode failure or refill timeout is explicit. Four-feed playback is tested;
sixteen-feed performance is not certified. This is a review monitor, not a genlocked
output.

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

## Buffering regression (2026-11-15)

Four real-camera playback started with 8.07 seconds of scheduled audio and about
14.2 seconds of video per feed. A 16-second steady run observed minimum reserves
of 6.4 seconds audio / 8.8 seconds video, zero refill pauses, zero corrective seeks
and zero catching-up samples. Injecting a 6.5-second audio request delay produced
one coordinated refill and successful resumption. A separate 9-second video
request delay also produced one coordinated refill and successful resumption;
the lowest observed video reserve remained above 5.2 seconds. Neither injected
stall produced catching-up samples. This is a controlled I/O-stall
regression, not a measured whole-HDD throughput guarantee. CFR 50 fps and VFR
segment tests compare every preview frame timestamp with its original source.
