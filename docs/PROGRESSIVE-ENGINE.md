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
