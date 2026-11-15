# Component evidence and chronological reconstruction

2026-11-15, engine 2026.11.15-r8. Syncaila symbol names supplied by the user
are architectural hypotheses, not verified implementation or performance data.
No Syncaila code was copied. This change builds on OpenSync's existing engine.

## Audit of the existing implementation

Already present: multiple fingerprint candidates, a hypothesis reservoir,
separate training/selection/audit regions, wrong-offset controls, constant /
affine / monotonic piecewise timing models, graph path checks and conflicting
edge removal, query-excluded anchor layouts, distributed spectral discovery,
short-clip full-reference competition, and digest-guarded optional resume.

Actual gaps found:

- Corroboration counted reference recordings even when their observations
  described one shared moment. An exact duplicate source could add a vote.
- Weaker audited relationships were considered per query, but complementary
  links between two established components could remain disconnected.
- Filename order / calibrated dates were not a distinct reconstruction mode.
- Saved legacy projects contain selected relationships and summaries of
  alternatives, not the complete unsuccessful search reservoir. Persistent
  rejected-pair/manual-anchor semantics and immutable alternative solution
  revisions are still incomplete. Optional analysis resume is not equivalent
  to a durable correction ledger. This revision does not claim otherwise.
- Anchor layout / repeated query-excluded solves remain a measurable cost:
  replaying the 107-recording saved graph takes about 41 seconds and allocates
  about 22.7 GB cumulatively (NOT peak resident memory). No DSP work is included
  in that measurement. Optimizing it needs placement-equivalence tests.

## Distributed corroboration

Existing reference corroboration now needs at least two separated supported
regions per observer; overlapping windows do not multiply evidence. Identical
PCM digests count as one observer. Model predictions replace discovery priors
when a fitted model exists.

A bounded component pass combines existing `corroboration_required` models
only after their existing audit gate passes. It requires two distinct source
identities on each side and at least three separated acoustic regions. Each
candidate's component transformation is checked against every competing
candidate at measured interval endpoints, within 50 ms. Contradictory
transforms are deferred, not outvoted. More than 64 candidates for a component
pair are deferred rather than launching an unbounded hypothesis search.

Transforms retain piecewise breakpoints; agreeing midpoint offsets cannot hide
incompatible rates. The original per-recording models remain the constraints
of the final graph solve. No new audio reads, threshold reductions or metadata
votes are introduced into acoustic verification. The pass is skipped if fewer
than two eligible profiles exist. It is one pass, not an iterative self-support
loop. `event_decisions` records component support / reasons for deferral.

Separated events are not necessarily statistically independent: repeated music
can recur across an entire interval. Neither this gate nor cycle consistency
certifies physical capture time. Placements retain `acoustically_supported_not_certified`.

## Explicit timeline modes

```
opensync-cli.exe sync --folders "D:\Event" --output "D:\Result" --mode chronological
opensync-cli.exe sync --folders "D:\Takes" --output "D:\Result" --mode clips
```

`clips` is the default and preserves acoustics-only placement, including takes
that legitimately overlap. The desktop currently uses that default; no new
GUI mode selector is included in this engine change.

`chronological` adds a final placement-estimation pass, never acoustic edges.
It groups an explicitly selected event's recordings by source folder and
numeric filename series; this is a series hypothesis, not proof of device
identity. Duplicate sequence numbers or overlapping acoustic anchor order
disable the series. Only unresolved singleton clips immediately bracketed by
two acoustic anchors are considered. Estimates never become new anchors.

For timestamp placement it needs at least three acoustically placed anchors
in the main component, a consistent clock offset and distinct dates. Container
or sidecar creation dates and filesystem modification dates are evaluated.
Modification dates compete as recording-start or recording-end conventions.
An incorrect but constant camera date can therefore be calibrated. Clock
resets / inconsistent anchor dates fail calibration. Agreeing timestamps do
not prove that they were never rewritten: the result remains conditional.

With absent or identical timestamps, a single consecutive missing numbered
clip may fit between its neighbors only when the spare gap is at most two
seconds. Larger gaps, missing sequence numbers, multiple unknown clips and
competing-event decisions are not filled. It never stretches a clip to fit.

Estimates carry `provisional_chronological_estimate`, their anchor IDs, timing
basis and `placement_uncertainty_seconds`. The uncertainty is at least one
second and can be larger; it is a conditional estimate, not a statistical
guarantee. It is separate from export rounding / presentation error. Project
models, XML review labels/descriptions, export manifests and desktop inspector
preserve that distinction. Existing acoustic components are retained separately
as `acoustic_components`; the updated layout components include the estimates.

## Experiments and limitations

Controlled evidence tests with known relationships:

- Two established groups joined by two weaker audited links: baseline retained
  two internal edges and left two components; revised engine retains four edges
  and reconstructs the known offsets in one component.
- Two references observing only one repeated moment: baseline admitted two
  additional links; revised engine retains only the established internal edge.
- Duplicate PCM, conflicting offsets, incompatible drift and failed audits
  do not establish a component bridge. Nonlinear clocks retain their bends.
- Wrong constant camera dates and completion timestamps calibrate; clock resets,
  isolated rewritten dates, wrong series, ambiguous acoustic placement and large
  gaps stay unresolved. Audio-less clips can receive explicitly provisional
  chronological placement. Saved-project / XML tests preserve source duration
  and the uncertainty annotation.

Saved evidence replay, baseline commit `2759c65` versus this selector:

| Dataset | Recordings | Stored edges | Changed placements |
|---|---:|---:|---:|
| Aghaee | 107 | 120 | 0 |
| Zendegifakher (includes Office_ch5) | 6 | 4 | 0 |
| Music shorts | 5 | 3 | 0 |
| Audio research set | 29 | 24 | 0 |

Aghaee selection + graph solve: 40.864 s baseline / 41.109 s revised in one run
each; allocations effectively unchanged. These are saved-evidence replays, not
full extraction-to-XML throughput measurements or independent correctness labels.
Legacy replay cannot test discarded hypotheses no longer present in the saved
project. No new real-media recovery or corpus false-positive-rate claim is made.
The four projects produced no eligible chronological additions under these gates.

The shifted-landmark experiment used 36 known-positive and 36 unrelated cases,
durations 1.2/2.4/4.8 s, low (200–1000 Hz), high (2500–5500 Hz) and broad bands,
and query-grid shifts 0/2/4/6 ms against the same reference index. Existing
landmarks use a 64 ms magnitude STFT window and 8 ms hop. Each band recovered
8/12 positives with the original grid and 8/12 using any shifted grid; no
unrelated case produced a candidate. Nine additional repeated-passage controls
retained both competing regions at 2.4/4.8 s for every phase; 1.2 s generated no
candidate. Shifting did not resolve the repeated-event ambiguity. This small synthetic set shows no recall
gain, not proof that shifting never helps. Extra shifts are research-only and
are NOT added to runtime. Different windows of the same audio are correlated
discovery views, not independent certification evidence.

Opt-in reproducible research tests (native Go, no Python):

- `TestEngineSavedEvidenceReplay`: `OPENSYNC_RESEARCH_PROJECTS` JSON path list,
  `OPENSYNC_RESEARCH_REPORT` report destination.
- `TestLandmarkPhaseResearch`: `OPENSYNC_PHASE_REPORT` report destination.
- Component and chronological falsification tests run in the normal test suite.

Next priorities are a versioned correction/search ledger with durable manual
rejections, richer frozen real-media hypothesis fixtures, and profiling the
repeated anchor solves. Metadata should guide selective acoustic investigation
only after its uncertainty is represented; it must not become false proof.
