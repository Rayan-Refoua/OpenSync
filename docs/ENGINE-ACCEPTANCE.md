# Engine acceptance benchmark

The next product milestone is a demonstrably reliable headless engine. UI
work follows engine validation. `go test ./...` validates implementations;
it does not by itself establish that automatic synchronization is usable.

## Run locally

Requires Go 1.23 or later. No FFmpeg or network access is needed for generated
fixtures or existing 16 kHz mono PCM16 WAV caches.

```powershell
go test ./...
go vet ./...
go build -o build/opensync.exe ./cmd/opensync
./build/opensync.exe benchmark --output experiments/acceptance-new --profile full
```

The output directory must be new. `full` generates two-hour recordings and
uses approximately 1 GB of fixture storage. `quick` runs the same discovery
cases but uses 120-second timing fixtures. Both emit `report.html` and
`report.json`, checkpointed during execution, plus self-contained synthetic
PCM inputs and ground truth. Original recordings and projects are not edited.

The default `--gate readiness` returns a nonzero exit code if any primary
check fails OR required capabilities are missing. `--gate regression`
returns nonzero for measured failures; blocked capabilities remain clearly
reported but do not fail that narrower gate. An interrupted run stays
`running`/`incomplete`; it must not be interpreted as passing.

## Fixed test contract, version 1

- Three deterministic acoustic realizations: seeds 22026, 83021, 49157.
- Fourteen cases each: clean, microphone EQ/noise, gate/compression,
  250 ppm drift, unrelated noise, two additional triangle legs,
  repeated content with two valid alignments, 70% dropout, eight-second
  overlap, reversed offset, polarity reversal, sub-grid offset, silence.
- Three configurations run on identical PCM: 8 ms, 2 ms, 2 ms with
  8 ms integration. The preselected primary is `2ms-integrated-8ms`.
  Comparison failures remain visible but do not affect its regression gate.
- A fixed **4 ms** top-one error limit across all configurations. Recovering
  truth at a lower rank is insufficient. Every valid repeated alignment must
  be retained. Noise scores are not invented probabilities or rejections.
- Drift fixtures must satisfy the limit at both endpoints, not just the
  nominal initial offset. The current discovery engine returns a constant
  offset, so this test can legitimately fail before drift handling exists.
- Local timing component tests use -250, 0, +250 ppm clocks. Five one-second
  windows span the recording. Two early observations fit an affine predictor
  **in the harness**; three later observations assess it. Local measurement
  limit: **1 ms**. Prediction limit: **4 ms**. The initial search neighborhood
  is known fixture input. This is explicitly not blind long-recording
  discovery or production timing-model fitting/correction.
- Input SHA-256, source build metadata, platform, runtime and Go allocated
  bytes are recorded. Allocations are cumulative, not peak process memory.
  Runtime is observational until a hardware-specific budget is agreed.

The synthetic family is deliberately narrow, even with several seeds. These
published seeds are development regression fixtures, not a permanently blind
evaluation set. Do not tune thresholds to make a failing report green. A
future held-back release corpus needs diverse recordings, independently
measured truth, negative examples and a frozen candidate engine.

## Real pair manifest

```json
{
  "version": 1,
  "pairs": [{
    "name": "camera-a_camera-b",
    "reference": "path/to/reference.wav",
    "query": "path/to/query.wav",
    "label": "unreviewed"
  }]
}
```

Paths are absolute or relative to the manifest. Run:

```powershell
./build/opensync.exe benchmark --output experiments/acceptance-real-new --profile full --real-manifest real-pairs.json
python tools/prepare_review.py --manifest real-pairs.json --report experiments/acceptance-real-new/report.json --output experiments/review-new
```

`unreviewed` pairs are observation-only: no expected offsets, accuracy claims
or rejection claims. Operational failure (e.g. unsupported recording length)
still fails `measurement_completed`. Labels `matched`, `ambiguous` and
`unrelated` require an independent evidence reference. Matched/ambiguous labels
also require `valid_offsets_ns` and `tolerance_ms` in (0,20]. Ambiguous
alignments must be distinct. The sign is:

`reference_local_time = query_local_time + offset`.

PluralEyes placements, discovery scores and graph agreement are not sufficient
independent truth. Listening review can identify useful or implausible content,
but cannot establish millisecond timing by itself. Measure distinctive events
at multiple separated times, retain uncertainty and drift, and record who
reviewed them. A changing offset must not be entered as a fixed-offset label.
Time-varying real labels are a future manifest extension.

The review tool verifies input hashes against the completed benchmark before
writing. It creates local eight-second stereo excerpts: reference left,
query right, unchanged channel levels. It collects distinct top-three
hypotheses from all successful configurations, shuffles presentation order,
and hides scores. For short overlaps excerpts can share acoustic material.
`review-notes.json` starts unreviewed and cannot authorize timeline changes.

## Initial measured baseline (2026-11-08)

The initial full run evaluated 126 synthetic searches, 12 searches across four
unlabelled real pairs, and 15 local measurements across two-hour fixtures.

- All 33 primary-mode non-drift positive cases recovered their top alignment
  within 4 ms, including both repeated-content alternatives in each seed.
- One primary drift case (seed 83021) failed the full-interval limit:
  **4.5 ms** at an endpoint. The initial offset alone would conceal this.
- The real C3 2148 reference exceeds the **1,200,000 feature frame** limit
  of the 2 ms configurations. 8 ms processes it; this is an operational
  limitation, not an acoustic rejection.
- Local timing and the harness affine prediction stayed within **0.125 ms**
  on the two-hour constant-clock fixtures. This does not validate the
  application's drift correction, which remains unimplemented.
- The real pairs show large mode-dependent candidate disagreements. They
  have no reviewed ground truth and do not contribute an accuracy score.

The engine acceptance gate is **failing**, while harness/unit tests pass.
False accept/reject rates, automatic ambiguity decisions, production drift
correction, certified global graph/export and diverse real-data validation
remain blocked. These distinctions are intentionally visible.

## Next engine work, in order

1. Make the real-pair review corpus independently labelled with event times
   and uncertainty; include unrelated and repeated musical content.
2. Implement bounded long-recording search with cross-segment alternatives
   and complete coverage, retaining the current explicit size error until
   the replacement is tested.
3. Integrate timing-model fitting and reserved-event validation into the
   engine; assess whole-interval drift and correction artifacts.
4. Calibrate match/reject/ambiguous decisions on development data, freeze
   thresholds, and evaluate on separate media. Do not certify candidates by
   rank, score, chronology or graph agreement alone.
5. Test global timelines, disconnected recordings, resumability, packaging
   and Premiere round trips before calling the command-line tool complete.
6. Build the UI around that validated workflow.

CI runs harness/unit tests on Windows and Linux. The acceptance benchmark is
an advisory job while known engine gaps remain, with downloadable reports;
its failing result is retained, rather than changing tolerances or making
missing functionality pass. It should become a required release gate when
the engine satisfies the contract.
