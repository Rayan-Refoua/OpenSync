# PluralEyes 4 Research Ledger

## Source

Local installed PluralEyes 4, Windows x64.
Installation:
C:\Program Files\Red Giant\PluralEyes 4

Evidence collected from:
- File inventory
- PE import tables
- Embedded binary strings
- MSVC RTTI class names

No source code was obtained.
No binaries were modified.

## Confirmed packaged dependencies

- FFmpeg: avcodec-57, avformat-57, avutil-55
- GStreamer 1.x and GStreamer Editing Services
- Chromium Embedded Framework (libcef)
- libsndfile
- libsamplerate
- Boost C++ components
- Intel IPP-related FFT symbols
- Xerces-C XML
- NLog

Package presence alone does not prove runtime use.

## Executable observations

PluralEyes 4.exe:
- Native x64 application
- Imports CEF, FFmpeg, libsndfile, Xerces
- Contains synchronization-related RTTI and diagnostics

PEServer.exe:
- Native x64 console application
- Contains matching, drift and sync diagnostics
- Similar internal classes to main executable

grinder64.exe:
- Imports FFmpeg and libsndfile
- Contains localhost and media-related strings

PluralEyesPlayer.exe:
- Imports GStreamer and GStreamer Editing Services

PEJumpstart.exe:
- Contains localhost and Boost-related strings

Exact process responsibilities are not yet confirmed.

## Synchronization evidence

Observed strings:

"##### Using snippet-matching algorithm #####"
"***** SoundfileMatchEqualLength *****"
"FindPeaks: nAfter = %u, nDataPerBin = %u"
"Estimated offset: %12s"
"Median offset: %s (%I64d)"
"Weighted offset: %s"
"Match extends to left of f_lBoundary; match rejected"
"Match extends to right of f_rBoundary; match rejected"

Interpretation:
- Snippet matching exists.
- Peak and threshold processing exists.
- Candidate matching uses boundary checks.
- Offset estimation includes median/weighted terminology.

Unknown:
- Fingerprinting representation
- Similarity metric
- FFT/correlation implementation
- Snippet selection strategy
- Matching thresholds and confidence estimation

## Drift evidence — HIGH PRIORITY

Observed strings:

"Drift was detected"
"Apply drift correction"
"DriftCorrectionOperation"
"DriftDetected"
"DriftCorrected"
"driftSlope"
"Filtered Slope: %g"
"Slope : %.10f (%.3f%%)"
"Total drift: %s audio frames (%s ms)"
"Position, Offset, L1 Fit, L2 Fit"
"offset_slope = %f offset_slope_l2 = %f"
"offset_intercept = %lld offset_intercept_l2 = %lld"
"Re-starting sync after drift corrected"
"pluraleyes_drift_corrected"

Interpretation:
- Drift detection and correction are distinct stages.
- There is evidence of slope/intercept-based modeling.
- L1/L2 fitting terminology appears in diagnostics.
- Synchronization can restart after correction.
- Corrected temporary assets appear to be supported.

Unverified:
- Whether drift is constant-rate or locally variable.
- Exact regression and outlier estimation methods.
- Resampling versus time stretching.
- Whether video timing is altered.
- Numerical precision and export behavior.

## Audio processing classes

- SndFirFilter
- SndDecimator
- SndConditionalPhaseInverter
- SndMonoDownMixer
- SndNormalizeBlocks
- PDNSoundProcessingBlock
- ExtractAudioOperation
- DecimateAudioOperation
- LevelAudioOperation

These are class-name observations, not a proven
execution order.

## Metadata/timing subsystem

Observed TimingMetaData::Offsets diagnostics:

- GraphGetShortestPath
- OutliersFind
- TrackOffsetCompute
- Before/After Outlier elimination Graph
- Transitive outlier
- Timing relationship outlier
- Clip content outlier
- Media creation time
- File creation time
- Timecode metadata

Interpretation:
- Metadata timing relationships appear graph-based.
- Multiple kinds of outlier detection exist.
- Audio matching and metadata alignment must not
  be assumed to use the same graph.

## Research discipline

CONFIRMED: Directly supported by observations.
INFERRED: Plausible interpretation of evidence.
UNKNOWN: Not yet investigated or demonstrated.

Never promote an inference to fact without evidence.

## Legal/engineering boundary

Use legacy software to study observable behavior,
interchange formats, and general engineering concepts.

Develop independent algorithms and source code.
Do not copy proprietary code, assets or protected
implementation material into the public repository.

## Next research targets

1. Locate snippet matcher call sites in Binary Ninja.
2. Locate drift correction call sites.
3. Determine actual signal-processing pipeline.
4. Determine how drift is estimated and corrected.
5. Examine project/export interchange formats.
6. Establish reference behavior using test media.

## M0 Audio Engine Investigation — 2026-11-07

Binary: PEServer.exe, image base 0x140000000.
Addresses below are from this installed binary version.

### Confirmed function relationships

sub_14028e340
    -> sub_140288810 (19,667 bytes)
        -> sub_1402c8b20 (15,780 bytes)

sub_1402c3700
    -> sub_1402c26c0 (1,615 bytes)

The first call chain is confirmed through Binary Ninja
cross-references and HLIL. The peak-detector path has
not yet been connected to the main matching path.

### Main matching function

sub_140288810 contains:

- SoundfileMatchEqualLength diagnostic
- Using snippet-matching algorithm diagnostic
- At least two calls to sub_1402c8b20
- Weighted offset diagnostic

The two calls occur around HLIL lines 938 and 2328.
HLIL line numbers are analysis-specific, not stable IDs.

### Offset-fitting function

sub_1402c8b20 contains:

"Position, Offset, L1 Fit, L2 Fit"

It is directly called by sub_140288810.

Its mathematical operation, input representation,
output representation and drift role remain UNKNOWN.

### Peak detector

sub_1402c26c0 contains:

"FindPeaks: nAfter = %u, nDataPerBin = %u"

Caller: sub_1402c3700.

Connection to main matching path: UNKNOWN.

### Relevant earlier discoveries

- Drift correction is a distinct operation.
- A diagnostic explicitly announces restarting sync
  after drift correction.
- Audio processing classes include FIR filtering,
  decimation, downmixing and block normalization.
- Metadata-based timing uses graph/outlier concepts.
- Source-track creation and synchronization are
  separate concerns for the OpenSync design.

### Current priorities

1. Determine what sub_1402c8b20 fits and returns.
2. Trace snippet selection and matching features.
3. Determine how drift observations are generated.
4. Map drift correction and re-synchronization.
5. Develop a ground-truth benchmark corpus.

### Research caution

Diagnostic names do not establish algorithms.
Decompiled variables and types may be inaccurate.
All OpenSync algorithms will be independently written.
