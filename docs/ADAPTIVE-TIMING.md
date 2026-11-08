# OpenSync — Adaptive Timing and Evidence Policy

Status: R&D design decisions, 2026-11-07.

## Matching strategy

- Choose the cheapest reliable method for each problem.
- Do not force a draft-first pipeline when direct precision is better.
- All accepted synchronization requires independent verification,
  precision refinement and assessment of timebase stability.
- Preserve weak and competing hypotheses rather than discarding
  them solely because their global similarity score is low.
- Local uniqueness, multiband agreement, transitive relationships
  and independent acoustic anchors may strengthen weak evidence.
- A flexible timing model fitting its own input anchors is not
  sufficient verification. Use held-out measurements.

## Timing models

Support constant offset, affine drift, smooth nonlinear time maps,
and piecewise models. Select complexity based on actual evidence.
Do not attribute relative drift to a specific device without proof.

The Office_ch5 CCTV export is a difficult reference case:
possible nonlinear clock drift, frame drops, timestamp irregularities
and discontinuities. Inspect audio timing and video PTS separately.

## Adaptive segmentation

- Offer optional, user-authorized segmentation.
- Introduce boundaries only where verified timing behavior requires.
- Prefer reversible, non-destructive logical source segments.
- Offer timeline/physical splitting separately.
- Independently synchronize affected segments when necessary.
- Permit later merging when stronger evidence supports continuity.
- Never alter original media.

## Audio processing robustness

Support cross-device acoustics, EQ, noise, compression, gating and
expansion. Favor amplitude-robust and locally distinctive features.
A gate may remove evidence; missing evidence is not automatically
negative evidence. Surviving events can still establish timing.

## Evaluation

Measure discovery, ambiguity, temporal uniqueness, precision,
drift, nonlinear residuals, held-out prediction and runtime
separately. Keep unresolved recordings separate from verified
timeline placements. Retest unresolved hypotheses as new evidence
arrives.

PluralEyes XML is a useful frame-quantized reference, not exact
ground truth. Its red, end-parked clips are unresolved, not synced.

Real-media corpus: zendegifakher, six MP4s. Original files read-only.
