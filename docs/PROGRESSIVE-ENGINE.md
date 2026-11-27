# Progressive Synchronization Engine

Status updated 2026-11-27. Discovery ranking, verification optimizations, adaptive timing, and graph-aware holdout scheduling are implemented.

## November 27 performance baseline

- Landmark discovery filters out buckets below the existing minimum vote count before ranking. Bounded selection returns the same eight-or-fewer distinct peaks as the previous full sort, with the same tie order. A 32,000-bucket Go benchmark measured approximately 3.76 ms before and 0.49 ms after. This is a ranking benchmark, separate from end-to-end synchronization.
- Verification still checks every competing hypothesis. Shifted controls compute the same strongest correlation and skip the local runner-up calculation, which only direct waveform measurements use. Prepared correlations reuse their FFT reference buffer across comparisons.
- The run profile now records candidate volume, verification reuse, prepared waveform and control comparisons, and separate counts for timing training, selection, and audit windows.
- A 16-recording chronological project produced identical timing models, validated relations, graph, project payload, and Premiere XML. The repeat baseline took 17.42 seconds and the optimized build took 17.22 seconds. This does not establish a substantial total-runtime improvement.
- The original C0559/2144 pair produced identical output and remained unresolved in both builds. The synthetic quick benchmark reported identical check outcomes across 126 runs and three local timing fixtures, including seven pre-existing constant-offset failures at 4.5 ms against a 4 ms limit.

The chronological run executed 75,233 prepared negative controls and started 12,847 timing windows.

## Adaptive timing experiment

The sparse policy starts with 7 fitting, 6 model-selection, and 12 audit windows selected from the existing schedule for strong single-prior overlaps of at least 180 seconds. A constant model must pass the established gates, agree with the prior within 8 ms, and have every sampled observation be precise, within 8 ms of the model, and supported by the narrow search. The policy measures remaining windows in the original schedule when a check fails and restores dense evidence ordering before evaluation.

On a 360-second PCM fixture with a known 20-second offset, adaptive completed with 25 windows versus 141, a 5.64× reduction, and returned the same constant offset. A dropout and a 20 ms timing discontinuity in a sampled region each escalated to the same dense evidence using 141 total windows. Sparse runs have different evidence counts and can miss brief changes between measured windows.

Automatic mode uses dense timing unless the project has at least 1,024 potentially avoidable windows across eligible overlaps. Each automatically eligible pair also needs three passing disjoint holdouts within 8 ms of its constant timing prior, distributed over at least 65% of its overlap. Full-resolution landmark discovery is automatic. The previous sparse voting priority option added work without reducing extraction or changing search coverage.

Six 340-second excerpts cut from one concert recording at known 10-second intervals exercised Automatic end to end. It measured 375 timing windows across 15 pairs, versus 1,855 for Dense. Both runs recovered all expected offsets and produced byte-identical Premiere XML. The timing stage took 2.17 seconds versus 11.02 seconds, and the complete runs took 6.15 seconds versus 15.28 seconds. These excerpts use the same underlying audio. Cross-device performance on the historical corpus remains unverified. The 16-recording chronological project stayed dense and reproduced the previous timing, graph, and XML.

## Graph-aware holdout scheduling

The holdout worker queue prioritizes high-confidence potential bridges and reacts to completed disjoint-window checks. A failed bridge leaves its endpoints disconnected in the queue, while passing checks may move pending cycle edges later. Workers remain available for those cycle edges whenever there are no pending bridge checks. Completion order does not alter report ordering, accepted edges, ambiguity policy, or graph solving. The scheduler intentionally runs every check and does not establish a total-runtime speedup by itself.

## Full-resolution fingerprint extraction

The extractor computes the existing percentile before filtering local peaks. Only bins strictly above that percentile can become landmarks, so it tests the same frequency/time neighborhood for those bins and discards a candidate as soon as it finds a strictly stronger neighbor. Equal-strength peaks still go through the original numerical tie path. This removes two spectrum-sized scratch arrays without changing the PCM rate, FFT or hop, percentile, landmark hashes, or feature-cache format.

A 50-second PCM benchmark went from about 167 ms to 127 ms and from 49.3 MB to 23.6 MB allocated per extraction. The six long concert excerpts and the 16-recording chronological project produced identical project payloads, graph results and Premiere XML in before/after runs. A stable end-to-end speedup has not been established. A separate lower-cost extractor and changes to posting suppression remain research options with different coverage implications.

## Resumable recovery

Explicitly cached runs now checkpoint full-range envelope searches by ordered pair and PCM identity, including completed searches with no candidates. Distributed recovery accepts an optional project-wide budget in reference hours and reports budget exclusions as unsearched. It reuses the existing distributed pair checkpoints after budget or preset changes. The forensic preset removes the standard per-query distributed quotas and occupied-offset exclusions for its supported short-query range. Balanced remains the default.

On the C0559 and 2144 research proxies, a 0.01-hour cap deferred the eligible distributed search. Resuming at 0.2 hours ran it. Resuming again at 0.3 hours reused the same checkpoint without rewriting it. The recordings remained unresolved. Component-level budgets and the comparison of staged recovery recall against the full historical corpus remain open.

## Global affine audit

The audit reuses the robust joint fitter from the anchor solver. It fits only training measurements and compares the candidate with production piecewise timing on separate holdout windows. It checks maximum disagreement as well as median residual, skips timing with meaningful nonlinear knots, and caps work at 24 recordings and 512 training windows per component. Placement promotion is experimental and requires `--global-timing`. The promotion gate holds the production reference fixed, requires a median holdout improvement of at least 1 ms, limits movement to 20 ms, and rejects components with known graph conflicts or excessive per-edge residuals.

A synthetic three-recording cycle with known clock drift detected lower held-out timing error. An 80 ms conflicting holdout blocked improvement, and nonlinear timing knots required the piecewise model. End-to-end regressions on the 16-recording project and six concert excerpts reproduced the timing models, validated relations, and Premiere XML.

## Core principle

Every new match contributes evidence to a global model.
New evidence may trigger targeted reanalysis of previously
unmatched or ambiguous recordings.

Never equate a failed fast search with an impossible match.

## Matching policy

1. Sparse fingerprint candidate discovery.
2. Contextual candidate ranking using known timing,
   metadata, device sequence, and neighboring clips.
3. Selective dense fingerprints for unresolved material.
4. High-resolution spectral/transient/correlation matching.
5. Graph-informed retry using predicted search windows.
6. Escalate until confidence is sufficient or the
   configured computational budget is exhausted.

Do not repeatedly perform equivalent analysis without
new evidence or an explicit change in strategy.

## Evidence graph

Each recording is a timing node.
Each pairwise match is an evidence-bearing constraint.

Store:
- Source and destination recording IDs
- Offset estimate and timing convention
- Clock-rate estimate where available
- Measurement anchors and residuals
- Matching strategy and evidence provenance
- Confidence, ambiguity and competing hypotheses

Global constraints must be validated for consistency.
For constant offsets, closed graph cycles should sum
to approximately zero within measurement uncertainty.

For drift, compare composed timing transformations,
not merely additive start offsets.

## Progressive inference

When new evidence arrives:
1. Update affected timing relationships.
2. Reevaluate graph conflicts.
3. Propagate supported timing constraints.
4. Identify newly searchable candidate pairs.
5. Schedule only useful additional analyses.
6. Revalidate affected predictions.

Use bounded scheduling, caching, and convergence
criteria to avoid uncontrolled retry loops.

## Temporal priors

Potential evidence:
- Recording sequence and filename numbering
- Container/device metadata
- Media creation timestamps
- Filesystem dates (lower reliability)
- Timecode and known session relationships
- Previously synchronized neighboring clips

Do not treat these as infallible.
Copied files, incorrect clocks and repeated audio
can invalidate otherwise plausible assumptions.

## Short recordings

Prioritize dense temporal features and adaptive windows.
Use all informative channels where appropriate.
Try alternate signal representations when justified.
Reject confident false positives from repeated sounds.

## Device and track organization

Source identification is separate from timing estimation.
Combine multiple device attributes with calibrated
confidence where sufficient test data exists.

Create an additional track if apparent source identity
conflicts with irreconcilable recording constraints.
Avoid unnecessary tracks but prioritize correct output.

## Drift

Estimate multiple synchronization anchors.
Fit robust relative clock models.
Check residual timing error throughout recordings.
Use piecewise models only where supported by evidence.
Revalidate after correction.

## Success criteria

- Ground-truth offset and drift accuracy
- False-positive and false-negative rates
- Short-clip recovery rate
- Graph consistency and error detection
- Runtime, memory and preprocessing cost
- Number of unnecessary retries
- Performance on real-world editing projects

## Next technical milestone

Build reproducible M1 benchmark data and evaluation
harness before choosing the production matching stack.

## Mandatory precision refinement

A successful draft match is NEVER the final timing result.

Every proposed synchronization must pass:
1. Candidate discovery or graph-supported prediction.
2. Independent overlap verification.
3. High-resolution offset refinement.
4. Multi-anchor drift and residual analysis, where feasible.
5. Global graph-consistency validation.

Precision refinement is mandatory even when the draft
match has high confidence.

Use fractional-sample peak interpolation or other
validated fine-timing estimators where justified.

Do not confuse numerical resolution with measured accuracy.
Report uncertainty when signal quality prevents a
reliable subframe estimate.

For a defined video frame rate, evaluate final residual
errors as fractions of one frame.

A provisional match may be shown early, but the UI must
distinguish provisional, refined, verified and unresolved
states. Never silently represent draft timing as final.

The expensive matching stage may be skipped when a fast
draft is independently verified, but fine timing refinement
must not be skipped.
