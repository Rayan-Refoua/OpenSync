# Multilevel timing and Premiere import fixes — 2026-11-10

The automatic file-list pipeline now retains competing offsets, measures
separate training/selection/audit windows, compares constant/affine/piecewise
timing, and checks composed timing relations against graph evidence. Ambiguous
short takes require corroboration. A short shared excerpt cannot alone connect
two long recordings. No filename-specific acceptance rules are used.

Video is always referenced at its original frame rate; nonlinear timing can
produce source-referenced segments. Audio-only nonlinear correction produces
new, versioned PCM derivatives with bounded crossfaded joins. Original media
is unchanged. Native projects retain precise measurements separately from
export rounding and the unresolved-placement policy.

Camera source timecodes are media addresses, never acoustic timing evidence.
Container timecode or validated Sony sidecars are retained in the native
project and explicitly exported. Sidecars must agree with source frame count,
frame rate and end timecode; renamed-file association additionally requires a
unique exact creation timestamp.

The previous Aghaee export omitted this metadata. A Premiere round-trip showed
C0572 source In changing from zero to -16,637 frames, explaining the striped
region and footage jump. The fixed export preserves all 21,528 frames at 50
fps (430.56 seconds). The user confirmed on 2026-11-10 that the jump is fixed.
Machine-readable round-trip of the fixed file is still pending.

The false mrs2-2 association came through a roughly 21-second ZOOM0006 excerpt.
It is now unresolved rather than connected to DN-500R. The fixed delivery has
52 cameras and 5 audio recordings in the main group; 50 other recordings are
appended, explicitly marked unresolved. These counts are coverage, not truth
labels or proof of PluralEyes parity.

Validation: 32 Python tests, Go tests and Go vet passed. Tests cover source
timecode, negative source addresses, repeated-programme ambiguity, nonlinear
mapping composition, video frame preservation, rendered stereo audio timing,
and real file-list processing of synthetic short positives and negatives.

`opensync audit-import original.xml premiere-roundtrip.xml --output audit.json`
checks source and timeline ranges from Premiere's interpretation. It does not
assess acoustic accuracy.

## External implementation review

- [align, 675927d](https://github.com/FedyaLight/align/tree/675927d51b10033256b51a045edc7b1ec362e60e):
  phase-weighted fine alignment, independent timing checks, piecewise mapping
  composition, and explicit source timecodes informed independently written
  additions. GPL-3.0 source was reviewed; no source was copied.
- [Syncitol, 1638dcb](https://github.com/thinkvp/Syncitol/tree/1638dcbeb451defedcafcd5cb1562e7a48af17ab):
  search-boundary rejection and cross-reference corroboration informed the
  review. MIT source was reviewed; no source was copied.

Neither repository was benchmarked against OpenSync, so no relative accuracy
claim is made. The next recovery milestone is distributed temporal/spectral
candidate aggregation. The user's C0559/2144 research demonstrates discovery,
but explicitly does not establish independently certified exact timing.
