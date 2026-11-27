# Engine stages

The public synchronization pipeline runs in Go. Its exact sequence depends on settings, source material, and available evidence.

1. **Inspect inputs.** Collect files, probe readable media, and record source identity and metadata.
2. **Prepare audio and fingerprints.** Decode or stream audio and extract acoustic features in bounded blocks. Visualization peaks can reuse audio read during synchronization.
3. **Discover candidates.** Compare fingerprints and rank possible pairs and offset regions.
4. **Verify candidates.** Examine acoustic windows and compare competing timing explanations.
5. **Search unresolved recordings.** Use recovery strategies to investigate candidates missed by the first pass.
6. **Evaluate timing models.** Fit supported timing relationships, assess residuals and reserved observations, and retain ambiguity where evidence is limited.
7. **Solve graph relationships.** Combine pairwise evidence into components and check conflicts and independent paths.
8. **Recover and refine.** Use accumulated context for further distributed searches and refine supported timing.
9. **Save and export.** Write the native project, construct the export timeline, and validate the Premiere XML.

Discovery, support, and validation describe different levels of evidence. A repeated sound can make several offsets attractive, while graph consistency can still reflect a shared error. Independent observations and competing explanations matter.

The implementation starts in `internal/autosync/native_pipeline.go`. Historical design notes and detailed algorithms are listed in the [documentation reference](./reference.md).

## Landmark discovery

OpenSync always searches full-resolution landmarks. The previous sparse-priority mode ran the same complete search and added a preliminary voting pass. Its CLI values remain accepted for compatibility.

During independent holdout validation, OpenSync gives qualifying component bridges priority and validates cycle edges for corroboration as workers become available. Every edge receives the same acoustic checks. Only passing holdouts update the scheduler's connectivity, and the final solver receives the results in their original order.

## Timing profiles

Choose **Synchronize settings → Timing profiles** or pass `--timing-profiles auto`, `--timing-profiles dense`, or `--timing-profiles adaptive`. Automatic is the default. It stays dense unless independent holdouts support the timing across enough long overlaps to make at least 1,024 timing windows eligible for omission. Dense always runs the complete timing schedule.

Adaptive is experimental. It tries 25 windows spread across an overlap of at least three minutes when a single strong timing prior survives and predicts a nearly constant offset. All sampled windows must agree to within 8 ms of a constant model, and the model must agree with the prior within 8 ms. If a window is weak, absent, or contradictory, OpenSync fills in the original dense schedule and reuses the windows already measured. Custom verification thresholds and competing priors use the dense schedule.

Both modes use the same PCM and keep fitting, model selection, and audit windows separate. An isolated timing change between sampled windows can escape an adaptive pass. Choose Dense when intermittent timing changes are a concern.

## Distributed recovery coverage

**Synchronize settings → Acoustic recovery** includes a **Forensic** preset. It searches every eligible reference for an unresolved audio-bearing query of 1.2–299 seconds, including offsets the standard occupancy shortcut can exclude. Verification still uses the normal acoustic checks. The earlier full-range envelope search also remains available for longer recordings.

**Distributed recovery cap** limits the sum of reference durations scheduled by the distributed stage. Zero leaves the preset in control. A cap of one reference hour can cover six ten-minute comparisons. Deferred pairs are recorded as unsearched in the saved project. To extend a cached CLI run, use `--resume` in the same output directory with a larger `--recovery-reference-hours` value. Compatible completed searches are reused, including envelope searches that found zero candidates. New runs use run-local analysis by default.

## Global timing diagnostics

The project graph can include a `global_affine_audit` for components with redundant links. It compares a robust joint offset and clock-rate fit against production piecewise timing using reserved windows. **Advanced → Guarded global timing** (CLI `--global-timing`) optionally promotes the fit when the median held-out residual improves by at least 1 ms. The engine retains the existing reference clock and checks every active relationship before promotion. It rejects components with known graph conflicts, nonlinear timing, or proposed movement beyond 20 ms. The option defaults off.

## Synchronization setup

Open **Setup** beside **Synchronize** to load a continuous-event, repeated-takes, or difficult-audio preset and adjust its answers on one panel. The panel covers recording structure, matching confidence, recovery effort, timestamp estimates for continuous events, and when to review recorder grouping.

Balanced uses the standard verification profile. Strict is useful for repeated takes when similar sound appears at several positions. Loose admits weaker leads and can increase ambiguity. Select **Keep current verification values** to preserve custom thresholds. Storage handling and advanced timing stay under their existing settings.

Expand **Project exceptions** to choose a timing reference, configure recorder overlap review, guide searches with recorder order, decide how to show or export uncertain acoustic matches, and control drift-corrected audio. The reference file sets the clock for its connected group. A tightly bounded recorder sequence can suggest an acoustic search location even when timestamps are unreliable. It still needs acoustic verification. Source review lets you mark individual recorder groups as non-overlapping.

**Queue for review** keeps candidates out of tentative timeline placement while retaining them in Match Review. **Show flagged proposals** displays tentative placements in the app. **Separate sequence** keeps unresolved media outside the main Premiere export. **Use original recordings** skips audio WAV generation and flags audio that exceeds the export timing budget as unresolved.
