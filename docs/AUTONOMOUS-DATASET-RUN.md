# Runtime update — 2026-11-11

Normal sync now reads originals directly, with caching OFF by default. Use
explicit --cache to retain analysis or --resume to reuse it. Folder inputs are
recursive. The copying, automatic reuse and approval checkpoints below are
historical research procedures, not the current product workflow.

The runtime dependency discussion below describes the historical reference
pipeline. Public synchronization, export, and import-audit commands now run
in native Go. See [NATIVE-GO-MIGRATION.md](NATIVE-GO-MIGRATION.md) for current
packaging, measured performance, compatibility, and validation.

# Autonomous dataset run and stage boundaries

User direction, 2026-11-09: finish the engine-produced Premiere test XML,
audit against the supplied PluralEyes sample using acoustic evidence, improve
the general algorithm when it falls short, report each stage and ask before
proceeding to the next stage. No agent-selected clip offsets or hand placement.

Additional delivery requirements, 2026-11-09: all clips without a reliable
connection to the main synchronized group belong in a clearly marked tail
section. The finished tool must accept media paths and automatically produce
a usable Premiere timeline without a project-specific script or intermediate
user decisions. These are production acceptance requirements, not a claim
that the current experimental exporter has already passed them.

## Current run

Directory: `experiments/autonomous-aghaee-20261108` (name reflects start date).

Inputs are the original media under `I:/Aroma/Aghaee Record And Edit`:
`D1-SD1`, `D2-SD1`, `D2-SD2`, `Sound`, `SOund Files Aghaee`.
The inventory contains 87 camera clips and 20 audio-only recordings.
Existing PluralEyes drift-corrected copies are excluded because their
originals are included. Rendered edits, project assets and photographs are
not original recording inputs.

The HDD is slow. Existing 87 PCM caches are reused only when source identity
metadata matches. New audio is copied sequentially onto the SSD once, then
decoded and analysed locally. Original media is read-only. Cache SHA-256
identities and feature versions are recorded. Original sources are checked
by path, size and mtime; no repeated full HDD hashing is performed.

## Command

```powershell
go run ./cmd/opensync sync `
  --input 'I:\Aroma\Aghaee Record And Edit' `
  --source D1-SD1 --source D2-SD1 --source D2-SD2 `
  --source Sound --source 'SOund Files Aghaee' `
  --cache-index experiments/m2_006/index.json `
  --output experiments/autonomous-aghaee-20261108 `
  --resume --stage discover
```

The Go CLI embeds the Python pipeline. This experimental command requires
Python 3.10+, NumPy, SciPy, FFmpeg and FFprobe. Tested installed DSP versions:
NumPy 2.2.6 / SciPy 1.15.3. These are explicit runtime dependencies, not a
claim of a finished standalone Go product.

The public file-list interface is now implemented:

```powershell
opensync sync --file-list media-files.txt --output MyTimeline
opensync sync --files 'D:\Camera\clip.mp4' 'E:\Audio\recording.wav' --output MyTimeline
```

Text lists contain one path per line; JSON arrays of path strings are also
accepted. Relative entries resolve beside the list. Repeating an identical
command resumes automatically; changed inputs/settings are rejected. The
default operation performs all engine stages without prompts. Stage flags
exist for development and the explicitly requested approval checkpoints.
Python/NumPy/SciPy/FFmpeg still need to be packaged into the distribution;
the file-list interface alone is not standalone-runtime completion.

The file-list integration test exercises actual FFmpeg caching, discovery,
verification, graph/export and an identical resumed invocation on synthetic
truth, including placement of unrelated audio after the synchronized pair.
It is skipped when FFmpeg is unavailable; it passed locally on Windows.

The current discovery algorithm uses bounded 60-second spectral-landmark
extraction, with overlapping chunks and duplicate removal. It examines the
collection without consuming a baseline XML or manual offsets. Up to eight
distinct offset hypotheses are retained per discovered pair. Fingerprint
votes are candidate evidence, never a probability or acceptance certificate.

`--stage cache` stops after inventory and caching.
`--stage discover` stops after candidate discovery.
`--stage verify` stops after automatic acoustic assessments and evidence files.
`--stage all` continues through graph planning and export.
Use `--resume` only with unchanged input selection and settings.

## Approval boundaries

1. Inventory, caches, acoustic discovery, and read-only integrity audit.
2. Acoustic verification, alternative comparison and drift measurements.
3. Global timing graph, unresolved-source accounting and Premiere XML export.
4. Structural and acoustic comparison with the supplied PluralEyes sample;
   report disagreements and propose general algorithm improvements if needed.

At each boundary report actual evidence and obtain user approval for the
next stage. Approval for one stage is not approval for subsequent stages.
Stages 2–4 have draft implementation but have not been run or accepted on
this full dataset at the end of the Stage 1 task.

## Stage 2 implementation

The initial verifier evaluates all retained offsets using local waveform
correlation, multiband spectral corroboration, wrong-offset controls and an
affine clock model. A bounded four-worker pool processes only SSD caches.
Results are serialized in deterministic input order and bound to candidate
content and PCM digests; old v1 evidence is retained separately.

Recovery targets recordings outside the largest supported component using
full-range, three-band log-energy-change correlation. It retains competing
peaks and submits them together with earlier hypotheses to the same acoustic
checks. Reference selection and offsets never use the PluralEyes timeline.

Pair models are frozen before validation on disjoint local audio windows.
At least two passing windows are required; any strongly supported timing
contradiction rejects the model. For overlaps of at least 30 seconds, acoustic
support, including measured window extents, must span at least 80% of the
overlap. A local waveform peak must be distinguishable from competing lags
more than 4 ms away; an ambiguous peak confirms neither agreement nor a
contradiction. This is a sampled consistency
check, not a guarantee across every frame or a calibrated false-match rate.
Full-recording discovery used the same media, so these holdouts are not
independent of candidate discovery. Models may be rejected at this stage.

Failed pair models are remeasured automatically with up to eleven longer
windows (up to twelve seconds), retaining their competing offset hypotheses.
The same validation thresholds are then applied. The initial and refined
evidence are kept separately. Synthetic tests cover positive/negative clock
drift, wrong-model rejection and recovery, periodic local ambiguity, silence,
repeated candidates, microphone filtering and serial/concurrent parity.

The separate read-only audit checks input metadata, evidence identity,
duplicate/ambiguous admissions, disjoint-window bounds, residual arithmetic,
temporal coverage, rejected models and connected-component coverage:

```powershell
python tools/audit_verification.py experiments/autonomous-aghaee-20261108 stage-2-audit.json
```

Global cycle consistency, final drift delivery, actual dataset XML construction,
PluralEyes comparison and Premiere import remain subsequent stage work. A
connected component at Stage 2 is not yet an accepted global timeline.

Baseline for later evaluation:
`C:/Users/Workstation/Documents/Red Giant/PluralEyes/CLIP.xml`.
It contains 87 video and 174 audio clip instances. Its timing positions must
not seed engine alignment. Timing agreement alone is not proof; disagreements
require listening/measurement of source events and drift. The 20 additional
audio recordings are outside that baseline's camera coverage.

Stage 1 audit:

```powershell
python tools/audit_discovery.py experiments/autonomous-aghaee-20261108 `
  'C:\Users\Workstation\Documents\Red Giant\PluralEyes\CLIP.xml' `
  stage-1-audit.json
```

Source metadata, all PCM cache digests, fingerprint schemas/version/bounds,
candidate identities/ranges and baseline inventory are checked. This audit
does not claim acoustic accuracy or accept candidate offsets.
