# Native Go engine migration — 2026-11-11

The public `sync`, `export`, and `audit-import` commands now run entirely in
Go. FFmpeg and FFprobe remain external media tools. Python, NumPy, and SciPy
are not embedded, launched, installed, or shipped by the Windows packager.
The previous Python implementation remains in the source tree as a development
reference; it is not an execution fallback.

## Public workflow

```powershell
opensync sync --file-list media-files.json --output MyProject
opensync sync --files camera.mp4 recorder.wav --output MyProject
opensync sync --folders CameraFolder AudioFolder --output MyProject
# Explicitly opt in to persistent analysis and interrupted-run recovery:
opensync sync --file-list media-files.json --output CachedProject --cache
opensync sync --file-list media-files.json --output CachedProject --resume
opensync export MyProject/OpenSync.opensync --output Export
opensync export MyProject/OpenSync.opensync --output Export --unsynced separate-sequence
opensync audit-import Export/OpenSync.xml Premiere-roundtrip.xml --output import-audit.json
```

Synchronization saves the precise `.opensync` project and exports Premiere
FCP7 XML. Unresolved recordings are appended by default. The optional
`--include-provisional` comparison mode retains explicit review labels and
never promotes those proposals into validated graph edges or reference clocks.
No project-specific script is required.

## Architecture and concurrency

- Caching is **disabled by default**. No originals are copied, no decoded audio
  files or feature/evidence caches are written, and no temporary disk cache is
  substituted. FFmpeg streams from original sources into Go memory.
- Files, folders, and file lists may be combined. Folder inputs scan all nested
  directories and use FFprobe to identify readable media, regardless of extension.
  Overlapping selections are deduplicated. The output subtree and symbolic links
  are excluded. Non-media/unreadable files are listed in `input-report.json`.
- Initial source decoding has two I/O slots. PCM is processed in overlapping
  60-second windows; active decoded windows use up to one eighth of available RAM, capped at 4 GiB.
  Analysis stage documents have a 512 MiB in-memory limit. Neither spills to disk.
  Exceeding that document limit stops with an actionable error. Shared indexes
  and DSP tasks require additional RAM; these limits are not a process RSS cap.
- Verification uses retained audio windows. When windows are evicted, a two-session decoder pool continues forward reads without restarting for every window. Backward reads restart when necessary. The decoder and
  resampler origin is preserved for exact sample timing. Distributed feature extraction also consumes the RAM audio working set instead of rereading every source. Long compressed sources
  may therefore take longer with caching disabled; this path does not inherit the
  cached benchmark below. No 1 TB no-cache performance claim has been established.
- `--cache` explicitly saves analysis PCM, features and checkpoints. Even this
  option never copies original media. `--resume`, `--cache-index`, and the research
  `--stage cache` operation are also explicit cache requests. Ordinary sync does
  not silently reuse an old output directory.
- Normal outputs are the precise `.opensync` project, Premiere XML, input report,
  timing manifest, structural check and run timings. Required drift-corrected audio
  remains an export asset; it is not an analysis cache. Saving/re-exporting a
  project does not require enabling caching. Interrupted analysis is resumable
  only when persistence was explicitly enabled.
- Fingerprint work is distributed in 60-second blocks, rather than one worker
  per recording. Long recordings therefore use multiple CPU cores.
- DSP worker counts adapt to `GOMAXPROCS`, the work count, and available memory.
  The analysis pool budgets 96 MiB per task and caps its budget at 8 GiB.
  Cache reads and checkpoint writes have a separate four-slot I/O limit.
- STFT reuses immutable FFT plans. Local maxima use separable deque filters;
  percentiles use selection rather than a full sort. Voting uses bounded
  hash postings and parallel query processing.
- Envelope recovery searches every valid lag in bounded reference tiles.
  Multilevel verification, independent graph contexts, and final holdout
  measurement are parallelized. Result order and tie-breaking are explicit.
- In explicitly enabled cache mode, per-block checkpoints, verification results, and
  source-bound profile identities survive interruption. Array caches written
  by NumPy can be read by Go directly using ZIP/NPY format compatibility.

The per-task budgets are scheduling bounds, not a promise of an exact process
RSS ceiling: the shared fingerprint index, accumulated results, and Go's
collector also consume memory. The measured 107-recording discovery process
peaked at approximately 3.9 GiB working set on this 32-logical-core machine.

## Measured discovery performance

The supplied baseline was the 107-recording Aghaee discovery run, 9m22s.
Both the baseline and migration measurement reused decoded PCM. The native
run created a new run directory and rebuilt fingerprints and discovery;
it also reused source metadata guarded by source size and modification time,
and verified reused PCM hashes. This deliberately removes redundant original
media probes. It is not a cold decode benchmark.

| Stage | Existing baseline | Native complete discovery run |
|---|---:|---:|
| Cache preparation/validation | 114 s | 0.873 s |
| Fingerprint extraction/indexing | 368 s | 56.819 s |
| Candidate discovery | 77 s | 6.490 s |
| Total including overhead | 562 s | 64.183 s |

That is an 8.76x end-to-end discovery speedup. It does not claim a 9m22s
full synchronization baseline: the supplied measurement ended at discovery.
The initial experimental 33-second index/vote result did not yet include the
final checkpoint and numerical-alternative handling and is not the delivered
performance claim.

CPU profiling identified FFT transforms, local maximum filtering, log-energy
calculation, and percentile selection as the main analysis costs. Native
benchmarks write `cpu.pprof` and per-stage timing reports. Independent affine
graph contexts were subsequently parallelized: the saved-evidence graph
comparison fell from about 30.5 seconds to 3.2 seconds without changing its
selected edges or final timing.

## Numerical and reliability audit

- Reference discovery: 865 pairs and 4,982 candidate offsets. Native
  discovery retains all 4,982 offsets and adds six numerical alternatives;
  no baseline candidate was lost.
- Float32 spectral plateaus can contain neighbouring peaks whose ordering
  changes with FFT rounding. Go preserves the ordinary ranking and also
  searches a higher-precision tie interpretation. Alternative candidates
  face exactly the same controls, fitting, holdouts, and graph checks.
  This is intentionally not a claim of bit-identical floating-point spectra.
- All 919 saved multilevel hypotheses retained their status. The same 120
  graph relationships were selected. Every saved final clip offset and rate,
  every component, and all three optional review placements matched the
  existing Aghaee project exactly, including after native discovery replaced
  the saved discovery input.
- Direct real-audio waveform/phase comparisons agreed on measured offsets and
  decisions. Score differences in the initial comparison were approximately
  1e-13 for waveform correlation and below 2e-8 for spectral flux.
- Fresh Go verification recomputed all 865 fingerprint candidate pairs from
  the real PCM, without reusing verification results. It supported exactly
  the same 137 pairs as the reference engine. Maximum offset and slope
  differences were 2.28e-13 seconds and 2.43e-15 respectively.
- Synthetic fresh synchronization recovers a known 20.136-second relation
  under polarity inversion and leaves unrelated audio and silence unresolved.
- Tests cover competing repeated events, brief-excerpt false bridges,
  contradictory holdouts, bad graph cycles, and monotonic piecewise mapping.
- Video is never stretched. Video segments account for every source frame.
  Audio-only correction preserves channels and exact output sample counts,
  with tested event timing across piecewise joins. Original media is unchanged.
- Project schemas 1 and 2 remain readable. The previous project reader also
  accepts newly saved Go projects with their content hashes intact.
- Resume preserves the saved inventory order. Windows directory-component
  sorting and raw string sorting differ; reordering numeric recording IDs
  underneath existing evidence is explicitly prevented.

These comparisons establish migration regressions on the available tests,
not independent acoustic ground truth for every Aghaee relationship. The
existing distinction between validated evidence, provisional review proposals,
and unresolved recordings remains visible in the project and export.

## Six-recording export failure

The faulty arithmetic classified a rate such as `30031/1001` (approximately
30.001 fps) as NTSC merely because its denominator was 1001. Frame positions
were calculated at about 30.001 fps while the XML described 29.97 fps.
For the long CCTV recording this produced an approximately 8.494-second
duration discrepancy, correctly rejected by the integrity validator.

The native exporter canonicalizes supported rates first and uses that same
rate for all arithmetic and XML serialization. The CCTV source has 246,798
frames and is exported at 30 fps, without a video speed effect. The six-source
project exports all originals and preserves its unresolved recording at the
end. Its original acoustic analysis did not need to be repeated.

Regression tests include this exact metadata/model fixture, mixed integer and
NTSC rates, source boundaries, overlapping clips assigned to separate tracks,
fractional frame rounding, source timecode, links, and deliberately corrupted
XML. Internal positions remain full-precision seconds; FCP7 presentation is
quantized to its declared frame grid and the presentation error is accounted
for explicitly. No unsupported XML subframe tag is treated as reliable timing.

The source-address regression for C0572 is retained: 50 fps, source timecode
frame 16,638, and 21,528 source frames (430.56 seconds), at 100% video speed.
Local media URLs use `file://localhost/I:/...`, avoiding Premiere's incorrect
UNC interpretation of the earlier paths.

## Align review and licensing

Reviewed [FedyaLight/align's Premiere exporter](https://github.com/FedyaLight/align/blob/675927d51b10033256b51a045edc7b1ec362e60e/crates/align-core/src/export/premiere.rs)
at commit `675927d51b10033256b51a045edc7b1ec362e60e`, including rate
canonicalization, track allocation, source linking, and its treatment of
fractional audio placement. Align is GPL-3.0-only. No Align source code was
copied into OpenSync; the Go exporter is an independent implementation against
OpenSync's project model. OpenSync remains GPL-3.0-or-later. FFmpeg's license
and build information accompany packaged binaries.

## Delivery validation and remaining manual check

Run `go test ./...` and `go vet ./...`. FFmpeg integration tests are enabled
with `OPENSYNC_INTEGRATION=1`. Optional real-data parity tests require explicit
reference/output paths and are skipped on machines without the recordings.
Windows race-detector tests were run using a test-only import library for
three existing Windows synchronization APIs missing from the installed old
TDM-GCC import libraries; this is not a product dependency. Production builds
use `CGO_ENABLED=0`.

Premiere import acceptance remains pending the user's office test. Structural
validation and a self-roundtrip audit do not substitute for a real Premiere
import, and no GUI automation was used. The engine/CLI is ready for UI and
installer development while that explicitly deferred editor check remains open.

## No-cache runtime correction — r3

The r2 fixed 256 MiB audio working set evicted needed windows on the six-file
Zendegifakher project. Each missed window restarted FFmpeg from the beginning,
including windows more than two hours into the CCTV recording. This caused
repeated source reads and idle DSP workers. The correction uses an adaptive,
bounded RAM budget, continuing decoders for forward reads when data is evicted,
and the same decoded memory for distributed feature extraction. Disk caching
remains off by default. Stage names and elapsed-time heartbeat messages now
make ongoing work visible.

The corrected executable completed the user's actual six files in 38.651 seconds:
metadata 0.661 s, streaming decode/fingerprints 7.515 s, discovery 1.708 s,
initial verification 1.928 s, recovery search 5.806 s, recovery/validation
5.076 s, multilevel/distributed analysis 15.918 s, graph/export 0.040 s.
All six recordings were exported, one unresolved at the end. No audio windows
needed re-decoding after the initial read. Only project/XML and result reports
were written. This was a rerun after the slow process was stopped, not a
controlled cold-storage benchmark. Premiere import remains a manual check.

Regression tests force eviction and check that four consecutive windows use
one continuing decoder, preserve exact PCM, restart correctly for backward
reads, and recover after a stage's context is canceled. Direct/cached sample,
fingerprint and distributed-feature equivalence checks also pass.
## Shared compute pool and deployment correction — r4 (2026-11-12)

The command still in use pointed at the r2 package. That running process was
confirmed to be repeatedly decoding long prefixes of the CCTV file. It was
stopped, its executable backed up as `opensync.exe.r2-backup`, and the executable
at that same path replaced with r4. `opensync --version` and every sync startup
now print the build identifier so folder names cannot hide the running version.

Recovery feature extraction now submits independent 60-second spectral blocks
to one bounded shared CPU pool. Timing analysis submits independent measurement
windows to that same pool. Results are assembled by original index before
smoothing or model evaluation. Training, selection and audit roles, evidence
thresholds, candidate coverage and numerical reduction order are preserved.
The pool runs leaf operations only, preventing nested worker starvation.

The same six original recordings completed without disk caching in 18.948 s,
compared with 38.651 s in the preceding r3 run. Recovery discovery fell from
5.806 s to 0.825 s; multilevel/distributed analysis fell from 15.918 s to 4.038 s.
The shared pool completed 2,103 tasks with peak active task count 32. This is
measured task concurrency, not a claim of sustained 100% CPU utilization.
No additional audio-window decoding was required after the initial read.

The complete Premiere XML and precise project are each byte-for-byte identical
to the r3 outputs (SHA-256 equality). All six recordings remain included, with
one unresolved recording appended. These are observed full-run timings with
no OpenSync disk cache; OS file caching and other machine activity were not
controlled. The older 0.876 s six-file report is a saved-analysis resume and
cannot establish the speed of a fresh single-threaded run.

Tests compare one-worker and eight-worker feature arrays and timing evidence
exactly, and exercise concurrent parents submitting to the bounded shared pool.

## Bucket scheduling and exact native PCM — r6 (2026-11-12)

r5 reused the prepared query FFT and query statistics across independent
verification controls and dispatched verification windows through the shared
pool. r6 also divides full-reference recovery searches into existing
32,768-position tiles by frequency band, and distributed searches into existing
120-second reference tiles. Pair coordinators submit leaf jobs to the common
pool; they do not occupy pool workers while waiting for child work. Indexed
results and ordered reductions preserve scoring, tie-breaking, background
statistics, candidate coverage, and peak suppression. No acceptance threshold
was relaxed.

Fingerprinting dispatches overlapping 60-second windows at 50-second strides.
Long recordings enter the source queue first, while results retain input order.
A source does not own a CPU worker until its entire recording is finished.
The worker limit uses available memory and GOMAXPROCS (32 on this workstation).
Queues are bounded; the measurement pool has at most twice that many ready jobs.
Source conversion remains limited to two continuing FFmpeg streams. Native PCM
window reads have an independent eight-reader limit. Disk reads and conversion
are not multiplied to 32 indiscriminately, especially on slow media.

The native PCM shortcut accepts only validated RIFF/WAVE PCM16, mono, 16 kHz
payloads. It performs no resampling, downmixing, conversion, or approximate seek.
Other formats retain FFmpeg and its original resampler origin. Direct PCM
sources are read by exact sample range and do not consume the decoded-audio
working set. This leaves that bounded RAM for recordings requiring conversion.
There is no persistent analysis/audio cache unless explicitly enabled.

FFmpeg integration tests compare the entire native PCM byte stream and
concurrent sample windows exactly, including signed extremes, odd RIFF chunks,
window boundaries and EOF. Stereo, other rates, PCM24, and float WAVs are rejected
by the shortcut. Serial versus bucketed recovery rankings and distributed
statistics/peaks compare exactly. The full Go suite, FFmpeg integration suite,
go vet, and focused race-detector checks passed.

The user's 29-file list was run twice without OpenSync disk caching:

| Stage (seconds) | Existing r5 run | r6 first run | r6 repeat |
|---|---:|---:|---:|
| Fingerprint extraction | 31.722 | 61.852 | 32.143 |
| Candidate discovery | 4.495 | 6.162 | 4.849 |
| Initial verification | 81.925 | 15.911 | 8.180 |
| Recovery discovery | 105.604 | 22.766 | 13.230 |
| Recovery verification/validation | 52.780 | 28.263 | 14.479 |
| Multilevel/distributed analysis | 18.527 | 21.222 | 11.340 |
| **Total, including metadata/export** | **295.931** | **157.842** | **85.212** |

These are observed runs, not controlled cold-storage measurements. OS file
caching and other machine activity were not controlled. The slower first-run
indexing did not reproduce; no indexing speedup is claimed. Across both r6 runs,
the XML and precise project are SHA-256 identical to the r5 baseline. All 29
inputs and 18 unresolved recordings are preserved. The 173 initial pairs,
282 recovery pairs, 61 distributed searches and final timing results are
unchanged. Additional audio window decoding fell from 934 windows / 652 decoder
starts to zero; the new path performed 202,565 direct PCM window reads instead.
Those counters exclude the initial source conversion streams.

An opt-in skew test generates fifty 60-second WAVs and one 3,600-second WAV.
It completed fingerprint indexing in 1.896 seconds with 122 buckets (72 from
the long recording), peak 32 active workers. This is synthetic indexing only,
not an end-to-end media benchmark. CPU profiling of that test found FFT,
maximum filtering and logarithms dominating the work; it did not identify GC
as the principal bottleneck. Active task counts are not CPU utilization.

Stage heartbeats now expose active/ready/completed buckets and audio read/wait
counts. These distinguish a compute backlog from an I/O wait. A single source
conversion or an ordered reduction can still limit a stage; the engine does
not promise sustained 100% CPU utilization. Tests can reproduce the skew case
with OPENSYNC_SKEW_REPORT set to a destination JSON path and
`go test ./internal/autosync -run TestSkewedFingerprintBuckets -count=1 -v`.

## Short music excerpts — r7 (2026-11-12)

The five-file music test reproduced two resolved excerpts and two unresolved
excerpts. The 1.486-second recording remains unresolved as requested. The
4.290-second recording had a useful discovery hypothesis but the normal
12-region verifier reduced it to approximately 299 ms per region. Several
waveform matches then failed the spectral-flux gate or local peak selection.

An automatic fallback now investigates unsuccessful 2–12-second query pairs,
with a reference budget of one hour. It uses two longer disjoint regions with
a 40 ms guard. Each searches the complete reference with the existing waveform
representation and FFT backend, in bounded parallel tiles. All eight input
sample phases are searched so decimation phase does not unfairly suppress a
match or its competitors. Nearby timing hypotheses are retained for spectral
checking instead of letting one waveform maximum veto the entire event.

The existing waveform floor, 1.5x competitor ratio, .035 score margin and
spectral-flux threshold remain in force. Every distant reference position
competes, rather than only four sampled negative offsets. The accepted peak
neighborhood must be no wider than 25 ms. Only a constant timing model is fit
from the first region; the second must pass independently on disjoint query
samples and agree within 25 ms. This is an additional short-clip verification
mode, not twelve independent observations or physical-capture certification.
Evidence is saved in the project, including global competitors, peak spread,
and both measurements. Fingerprints, filenames and timestamps do not supply
the placement. Existing successfully validated profiles take precedence.

The recovered excerpt `20261112_105236.wav` is placed at 37.638375 seconds in
`music.wav`. Its two regions supported offsets 37.638375 and 37.6309375 seconds
(7.438 ms apart); first-region near-peak spread was 15.0625 ms. Existing
placements of `105217` and `105227` remain 18.892 and 28.586377267 seconds.
These are acoustic placements, not independently measured physical ground
truth. The 50 fps XML quantizes placement to its frame grid. The shortest file
is preserved at the end. All source media remain untouched and disk caching
remains off.

Regression tests cover known synthetic offsets, sample phase, polarity,
filtering/noise, tile and source boundaries; negatives include repeated
passages, different offsets in each half, only one matching half, sustained
tones, silence and eight unrelated noise seeds. Full Go/FFmpeg tests, vet and
focused race checks passed. The profile cache namespace was advanced to v2 so
old failures cannot silently bypass the new investigation on resume.
