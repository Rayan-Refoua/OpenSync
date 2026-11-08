# M25 — Per-peak acoustic support diagnostics

The M24 benchmark recovered all 27 positive configurations, while real
C0599 showed a +52.718s correct leading peak at stabilized 2ms and
near-top competing peaks from ~9-second overlaps. The unrelated synthetic
negative case had a numerically larger raw score than C0599's real
correct peak. Therefore a global threshold is not appropriate.

M25 introduces a **non-certifying diagnostic** to explain these peaks:

```bash
opensync research-support project.json index.json discovery-report.json output.json
```

This reads a prior `research-match` JSON and the verified source
cache identities. It never changes recorded audio, project hypotheses,
or Premiere XML. It accepts discovery reports from M22/M24 (8ms, 2ms,
2s8) and examines **all retained peaks**, not just the leading one.

For each peak, determine the actual shared interval from both recording
durations and the proposed offset. Then partition that interval into
at most twelve nonoverlapping windows, spread across the entire overlap.

- >= 16s overlap: use up to twelve 8s windows.
- 8–16s overlap: use 4s windows (at least two for an 8s match).
- Within each window, report exact candidate-offset three-band
  Pearson correlation and local ±250ms lag refinement.
- Report the per-window offset residual and local correlation,
  including uninformative/silent windows rather than hiding them.
- Different frequency bands inside one window do **not** count as
  independent acoustic events.
- Do not reject short matches merely because they have fewer
  windows, or infer calibrated probabilities from raw Pearson scores.

**Critical limitation:** these measurements reuse the *same acoustic
material used for full-range discovery*. Nonoverlapping windows are
separate acoustic regions but **not independent heldout validation**.
The diagnostic cannot certify a match, override export positions,
or automatically reject weak/short genuine candidates.

The M23/M24 benchmark is also expanded to report the rank and presence
of **each** declared valid ground-truth alignment for repeated content:
0s and +24s. Previously the output could report rank #1 while silently
missing the second legitimate match.

## Suggested comparison

Use the same C3 2161 (ref) ↔ C0599 (query) reports:

- M22 original 8ms: `known-8ms.json`
- M22 original 2ms: `known-2ms.json`
- M24 2ms + 8ms integration: `known-C0599-2s8.json`

Inspect per-window support at +52.7s and near the ±440s/950s
boundary peaks. Repeated-content synthetic fixture has two **valid**
placements. None of these diagnostics is independent acoustic
verification or a global graph certificate.

Future work: reserve new unused acoustic events *before* selection,
separate discovery from truly heldout verification, calibrate
negative-control distributions conditional on overlap duration,
compare time-varying drift models, and certify only with multiple
independent sources of evidence.
