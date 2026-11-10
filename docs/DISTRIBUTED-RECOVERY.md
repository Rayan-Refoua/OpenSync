# Distributed acoustic recovery — 2026-11-10

The supplied C0559/2144 research was preserved with both original stereo WAV
extracts under `OpenSync-research/distributed-consensus-handoff-20261110` on
persistent storage. Its own classification is discovery success, not exact
timing certification. The RAM-disk files are no longer the only copies.

## Implemented extension

`internal/researchmatch/distributed.go` extends the existing native Go research
matcher and reuses its FFT correlation and normalization primitives. FFmpeg
decodes/resamples media; feature extraction, ranking and search are native Go.

- Preserved-channel, 8 kHz, 512-sample periodic Hann STFT, 80-sample hop.
- Five spectral-energy-change bands; channel combinations pooled within each
  band rather than counted as independent corroborating observations.
- Duration-adaptive separated temporal regions; fixed median spectral
  percentile and trimmed temporal aggregation at a shared clip offset.
- Full-reference searches in bounded 120-second tiles, including negative
  offsets/partial overlap; top competing peaks retained deterministically.
- Background score contrast is diagnostic, not a calibrated probability.
- Streamed feature files; query stage currently bounded to 60 seconds.

The normal `opensync sync --files ...` path automatically escalates unresolved
short recordings. Per query, the current deterministic budget is eight
partners and 3,600 seconds of searched reference material. Coarse scores order
partners; they do not supply offsets to the native search. Searches outside
the budget are recorded as deferred, not rejected. Cached mono is used first
to avoid rereading slow source drives. Preserved-channel native analysis is
available, but automatic stereo escalation is not yet implemented.

Twelve discovery hypotheses are retained per searched pair; three are sent
through the existing fine verification. A discovery candidate earns zero
corroboration weight until acoustic verification supports it. Raw discovery,
deferred work and unresolved timing hypotheses survive in the native project.

The relative acoustic relationship map distinguishes proposed relationships
from verified placements, and records stable offsets, smooth arrival-time
trends, and changing/conflicting precise measurements. It does not estimate
physical camera coordinates or identify movement as the unique cause of a
trend. See `ACOUSTIC-PROPAGATION.md`.

## Measured results and limitations

- Native stereo blind search: first candidate 262.130 seconds.
- All five leave-one-band-out native searches retained first rank near
  262.130–262.140 seconds.
- Native automatic-section ablations: first rank in two of four cases;
  fifth rank in the other two. This is weaker than the supplied prototype's
  temporal ablation result and must not be described as equivalent robustness.
- Cached-mono full-reference result retained 262.140 seconds for the hard pair.
- The previous envelope recovery had already discovered 262.128 seconds.
  Fine verification, not just discovery, is the bottleneck for this pair.
- Dataset escalation: 239 pair searches, 2,889 pair searches deferred by the
  budget; 409 pairs with 919 fine hypotheses examined in the combined reservoir.
  No additional verified placements. All previous exported placements remain
  identical, including the unresolved status of C0559 and mrs2-2.

No PluralEyes offset or filename-specific rule is used by the matcher. The
reported target region is used only when reporting benchmark rank. The full
real-world perturbation/falsification matrix from the research handoff remains
unfinished. There is no claim of production certification or improved
certified recall from these search results alone.

## Regression coverage

Go tests cover centered Pearson agreement with direct calculation, competing
events across tile boundaries, negative partial overlaps, silence, and stereo
polarity cancellation. Python tests cover deterministic scheduling, preserved
alternatives without free corroboration weight, and distant-microphone arrival
delay with reflections/noise through the actual file-list CLI. Context tests
keep clock drift, movement, and ambiguous measurements distinct.

The immediate next algorithmic milestone is resolving or bounding competing
acoustic paths using complementary measurements and reserved validation,
without converting uncertainty into a permissive placement threshold.
