# M24 — Adversarial synthetic matching and independent temporal integration

M23 recovered the top ranked ground truth on all six positive scenarios
with both 8 ms and 2 ms feature hops; the first real-media trial still
favored 8 ms on C0599. M24 tests **which assumption** breaks rather than
blindly increasing resolution or declaring a real recording unsynchronizable.

## Controlled A/B/C comparison

The same synthetic PCM WAV dataset is evaluated with:

- **8ms**: unchanged 8 ms hop, 8 ms local RMS integration (M22 baseline).
- **2ms**: unchanged 2 ms hop, 2 ms local RMS integration (M22 baseline).
- **2ms-integrated-8ms**: 2 ms hop, 2 ms local RMS values, with a
  four-hop causal moving-average integration of log-energy derivatives
  (8 ms smoothing span) before matching. This deliberately decouples
  **lag search grid** from **feature smoothing timescale**.
  It is not equivalent to genuine 8 ms RMS extraction or a multi-size
  STFT, and may sacrifice temporal sharpness.

No original match mode is changed. Use `research-match ... 2s8 ...` for
the stabilized 2 ms research mode on existing 16 kHz cached audio.

```bash
go run ./cmd/opensync research-synthetic experiments/m24_new_output
```

## Ground-truth adversity

Three new WAV fixture pairs supplement the seven M23 scenarios:

| Case | Ground truth | Intended failure mode |
| --- | --- | --- |
| repeated-content | two valid offsets: 0 s and +24 s | Distinct legitimate correspondences; retain alternatives |
| missing-70pct | +4.375 s | Independent noise replaces 70% of the query in 3s alternating blocks |
| short-overlap-8s | +30 s | Only 8 seconds of real audio overlap near a recording boundary |

The repeated case **must not** treat +24s as a false match just because
the ground truth includes 0s; both align identical source content.
Ranking considers both valid offsets without forcing only one answer.

The short-overlap case uses a 38-second reference and an 18-second
query: their only shared source interval is reference 30–38s versus
query 0–8s. A peak within a short boundary overlap may be accidental:
the matcher does not currently calibrate this risk.

The missing-70pct case preserves an unchanged global timeline and
substitutes independent noise during erased intervals. Gated silence
and omitted events are distinct from sample-clock drift.

## What counts as progress

Compare raw candidate peaks, their overlap durations, how many valid
alternative locations are retained, and whether the known truth is in
the top 12. Raw correlation values from different feature resolutions
are **not calibrated** or directly comparable.

A synthetic clean pass is necessary but insufficient for real success.
No experiment result certifies a placement, changes a source or enters
Premiere XML. Any real candidate still needs disjoint holdout acoustic
validation and graph consistency.

The first real positive-control comparison for the stabilized variant
is C3 2161 ↔ C0599 (+52.709s measured) alongside the prior M22
8ms and 2ms reports.
