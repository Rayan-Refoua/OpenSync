# Autonomous dataset run and stage boundaries

User direction, 2026-11-09: finish the engine-produced Premiere test XML,
audit against the supplied PluralEyes sample using acoustic evidence, improve
the general algorithm when it falls short, report each stage and ask before
proceeding to the next stage. No agent-selected clip offsets or hand placement.

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
