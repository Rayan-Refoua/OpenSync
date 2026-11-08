# OpenSync release audit contract — living checklist

This checklist separates IMPLEMENTED, PARTIAL, and NOT IMPLEMENTED.
No release or 87-file XML is called fully synchronized until every
applicable acceptance condition is evidenced by artifacts and real media.

## Input and provenance
- [x] Project source IDs, original paths, immutable source media and PCM cache identity checks
- [x] Original PluralEyes full Premiere XML as fallback coverage baseline
- [x] Complete 87-video / 174-audio structural preservation checks for the established fixture
- [x] Original timeline track/clip/audio link constraints and baseline overlap comparison
- [ ] Verify final real-data Premiere import, linked audio channels, exact clip timing and visual playback in Premiere

## Search and evidence
- [x] Quality profiles Draft / Normal / Rock Solid (including sock-solid alias)
- [x] Source-specific numeric chronological neighbor priority; no chronology inferred as continuous time
- [x] Alternative hypothesis recovery, no arbitrary admission based solely on fingerprint votes
- [x] Wrong-offset controls; retain weak plausible hypotheses and errors
- [x] Sequential probe batch of all existing direct numbered-neighbor discovery hypotheses
- [x] M15 independent acoustic windows separate from prior probe windows, with provenance and control comparisons
- [x] M15 training-only constant/affine model fitting with simple complexity penalty
- [ ] Dynamic adaptive search budget with evidence-based stop/retry rather than fixed quotas
- [x] M22 standalone research full-range 2 ms/8 ms three-band FFT cross-correlation (known real-data 8 ms match; 2 ms regression under study)
- [x] M23 deterministic synthetic PCM benchmark (clean, microphone, gate, drift, negative and three-way ground truth)
- [x] M24 synthetic adversarial tests (repetition, missing events, short overlap), with 2ms+8ms-integrated A/B/C diagnostic mode (real-data comparison pending)
- [x] M25 local acoustic per-peak support diagnostics (all alternatives, full-overlap windows and local lag residuals; NOT independent holdout evidence)
- [ ] Independent discovery from previously unrelated portions and recovery of the 42 missing sequential pairs
- [ ] Multiple frequency bands, gating/compression/EQ resilience, distinctive event corroboration and repeat ambiguity
- [ ] Robust outlier handling and uncertainty calibration (avoid equating a good Pearson score with ground truth)
- [ ] Validate holdout residuals against a calibrated error model and peer overlap/graph cycles
- [ ] Compare constant, affine, smooth nonlinear and piecewise models with held-out error and complexity penalty
- [ ] Detect discontinuous/dynamic time drift (especially CCTV PTS irregularities); validate audio/video timing separately
- [ ] Reversible source-referenced segmentation where justified by independent drift/discontinuity evidence

## Graph and export
- [x] Source-local and cross-source relationships are distinct investigation classes
- [x] Preserve genuinely unsyncable/unresolved recordings; no invented acoustic offset
- [x] Baseline-preserving XML exporter with externally reviewed placements and explicit loss-of-overlap authorization
- [x] Detailed provenance on inherited versus externally reviewed positions; no false certification
- [x] M19 diagnostic graph enumerates disconnected components and alternate candidate paths with raw cycle residuals (not acoustic certificates)
- [x] M21 measured-offset graph diagnostics from pre-reserved acoustic reports; spectral bands share event identity, affine offsets are deferred
- [ ] Certified global timing graph with calibrated cycle consistency, independently checked paths and uncertainty propagation
- [ ] Native machine-produced independent verification certificates consumed by the exporter (currently external review only)
- [ ] Output real dataset Premiere XML based on evidence-gated placements, not experimental graph guesses

## Final acceptance protocol
1. Make a frozen run manifest listing exact source XML/project/cache hashes, tool commit, mode and media fingerprint.
2. Verify all expected clips are present exactly once with correct original in/out, duration, audio links, no unauthorized edits and no collisions.
3. Compare every baseline PluralEyes relationship to the new layout. Distinguish temporal overlap from measured acoustic synchronization; justify all lost baseline overlaps.
4. For each claimed improvement, document candidate alternatives, independent audio events, wrong-offset controls, multiband evidence, timing model, residual/drift and graph consistency.
5. Include failure/unresolved cases and negative regressions (C0594 bad offset and C0599 promising candidate).
6. Verify special footage with timestamp/PTS anomalies; separately inspect audio/video synchronization.
7. Open exported XML in Premiere and perform manual image/audio spot checks before signing off.
8. Produce a machine-readable audit JSON plus human-readable report and final editable XML.

## Performance after algorithmic stability
- [ ] Bounded Go worker pool for CPU-intensive independent probes
- [ ] Deterministic ordering and output hashes regardless of worker count
- [ ] Cache reuse, I/O throttling, progress/resumability and cancellation
- [ ] Concurrency parity tests: single worker and N workers produce identical evidence decisions

M15 acoustic holdouts are diagnostic, **not graph certification**. A
holdout being disjoint from the earlier measurement windows does not
prove uniqueness when the initial discovery hypothesis and reference
were selected from the same global audio content. Acceptance requires
separate corroboration, drift consistency and graph validation.
