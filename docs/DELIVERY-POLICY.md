# OpenSync — Timeline delivery policy

## Current selected policy (2026-11-08)

- Strategy: Balanced
- Maximum permitted residual drift: 150 milliseconds
- Audio-only time-corrected derivatives: Allowed
- Logical video segmentation: Allowed

The file-list CLI implements these export preferences, including
`--max-residual-ms`, video segmentation and optional audio derivatives.
They are delivery bounds, not acoustic acceptance thresholds or a claim
that the evidence establishes that accuracy for every recording.

## Architectural invariants

1. Preserve measured source timing independently of delivery decisions.
2. Retain original video frame rate and source media wherever possible.
3. Prefer source-referenced timeline edits to new video encodes.
4. An audio derivative is optional, versioned, and never replaces its source.
5. Evaluate predicted residual error across the complete placed interval,
   accounting for timing uncertainty; do not judge by average drift alone.
6. If tolerances cannot be met with permitted operations, report the
   unresolved error. Do not claim that a placement is exact.
7. Do not promote discovery evidence solely because delivery tolerances
   are permissive.
8. Preserve one output track per identified physical recording device.
9. Export target: Premiere-compatible editable timeline, with original
   media links and a validated import round trip.
10. Distinguish audio-leading from audio-lagging residuals where the
    output format and measured timing permit; perceptual preferences
    must never overwrite the actual measured alignment.
