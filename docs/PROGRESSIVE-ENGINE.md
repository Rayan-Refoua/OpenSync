# Progressive Synchronization Engine

Status: Approved design direction, unimplemented.
Date: 2026-11-07

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
