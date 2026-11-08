# M23 — Deterministic synthetic synchronization laboratory

M22 observed an important real-data regression: the known C3 2161 ↔
C0599 relationship appeared as the **top** 8 ms peak (+52.712 s,
approximately 3 ms from an independently measured +52.709 s offset),
but did **not** appear among the displayed top-five 2 ms peaks. The
leading 2 ms scores came from short-overlap regions near boundaries.

This suggests either unstable 2 ms transient-envelope derivatives or
short-overlap candidate ranking bias, or both. M23 establishes
controlled, reproducible source-timeline ground truth, rather than
assuming that the real recordings form a valid benchmark.

## Generate and run the benchmark

```bash
go run ./cmd/opensync research-synthetic experiments/m23_synthetic_new_dir
```

The output directory must not exist beforehand. All generated audio is
16 kHz mono PCM WAV that can be opened by the *same* OpenSync audio
reader used on the real project. All sources are created deterministically
from seed 22026, using a common 48-second underlying soundscape with
irregular transients, tonal variation and broadband noise.

Seven cases, each tested at **8 ms and 2 ms** feature hops:

| Case | Ground truth | Purpose |
| --- | --- | --- |
| clean | +4.375 s | Exact shared content, differing start and end |
| microphone | +4.375 s | EQ, gain and independent mic noise |
| gate-compression | +4.375 s | Gate, nonlinear saturation and noise |
| drift-250ppm | +4.375 s at recording origin | Simulated clock skew: one offset increasingly inadequate |
| unrelated-negative | no valid match | Independent-noise negative control |
| triangle-A-C | +8.125 s | Third recorder from common soundscape |
| triangle-B-C | +3.750 s | Third edge: +4.375 + 3.750 = +8.125 s |

All offsets follow OpenSync's local-time convention:
`reference_local = query_local + offset`.

`results.json` records every scenario, ground-truth offset, peak rank,
nearest top-12 error, top score and complete candidate peaks. A negative
control may still have incidental correlation peaks; it is **not**
treated as verified or even auto-rejected based on one score.
The output includes no project mutation or Premiere exporter changes.

## Interpretation

- A clean fixture missed at 2 ms indicates a problem in the feature
  extraction, search or ranking—not real-world microphone chaos.
- Clean success but noisy/gated failure identifies feature robustness.
- A true offset excluded from the top 12 while strong short-overlap
  peaks dominate is evidence for overlap-aware ranking experiments.
- The drift case helps distinguish a broken constant-offset search
  from a time-varying clock model; its instantaneous offset varies over
  the recording and cannot be certified from one scalar.
- Compare 8 ms and 2 ms under the *same features and input audio*.

The synthetic waveforms exercise one controlled family of sounds; they
are not a validated substitute for speech, acoustically different rooms,
music repetition, surveillance discontinuities or real recordings.
Do not promote any peak to a certified edge based on synthetic results.

Follow-on experiments should add repeated content, missing events,
nonlinear/segmented drift, unequal gain/compression, clock resampling and
dataset ground-truth withheld events. The release audit remains blocked
on real-media acoustic evidence and the 87-video Premiere XML audit.
