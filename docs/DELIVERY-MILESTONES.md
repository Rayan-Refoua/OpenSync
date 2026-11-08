# OpenSync delivery roadmap

These are product acceptance milestones, not feature-count milestones.
The baseline is the existing Go acoustic/discovery engine and the
Premiere-compatible PluralEyes FCP7 XML export.

## M9 — 87 recordings to a Premiere-editable project (next priority)

**Input:** the real 87-file dataset and its cached audio, with no required
manual camera/track assignments. The existing PluralEyes-generated XML is a
compatibility reference, not automatically trusted synchronization truth.

**Output:** a single Premiere-importable sequence retaining all 87 original
media references, original media untouched, appropriate video/audio track
structure, and complete per-recording placement provenance.

Acceptance checks:

1. All source identities and paths accounted for; duplicate basenames
   cannot silently identify the wrong source.
2. No unexpected missing clips, deleted media references, broken audio
   links, or track collisions.
3. Every derived position labeled verified, diagnostic/provisional,
   unresolved, or template-preserved. No unmeasured position is claimed
   to be acoustically verified.
4. Premiere import/roundtrip on the user's workstation, with an
   inspectable timing report and spot checks for actual sync.
5. Automatic input-to-timeline processing, retaining alternative matches
   and multiple takes. Competing content-time placements (e.g. repeated
   choruses) must not be collapsed based on recording chronology.

**Current gap:** the export path can patch a subset of verified/measured
placements and preserve the original project, but it cannot yet derive
defensible positions for all 87 recordings. The first read-only gate is
`opensync audit-premiere <pluraleyes.xml> <project.json> <audit.json>`.
This counts source coverage, ambiguous filenames, missing video instances,
and overlapping same-lane placements. A clean structural audit is not
proof of synchronization.

Next engineering stages: provenance-aware source identity mapping;
batch candidate measurement and independent verification; connected
source/content time domains; multi-recording timeline planning with
non-destructive overlap/lane allocation; Premiere roundtrip.

## M10 — CCTV drift/discontinuity and segmentation

**Input:** difficult CCTV recording with possibly irregular PTS,
nonlinear drift and frame discontinuities.

**Output:** independently assessed constant/affine/smooth/piecewise
timing models, with optional logical source-referenced segmentation,
reversible and mergeable boundaries, and audio/video accuracy assessed
separately. Preserve original video, warn about discontinuities and
delivery compromises. Segmentation must be evidence-driven, not an
automatic response to a noisy probe.

## M11 — UI technology decision

Build a small proof of concept with realistic interaction and performance:
large recording inventories, graph evidence, disagreement review,
progress/cancellation and playback/preview integration. Choose a stack
based on packaging, native filesystem/media capabilities, deployment
size and complexity; do not commit to a heavy framework prematurely.

## M12 — Product UI

Drag/drop import, one-action automatic sync, progress and resource
profiles, confidence/provenance inspection, manual override with
reversibility, source grouping, timeline preview, and Premiere export.

## M13 — Release readiness

Reproducible Windows builds, installer or portable binary, versioning,
logging/crash reports, resumable caches, upgrade/migration paths,
performance/correctness benchmarks, compatibility matrix and an
end-user release smoke test.

## Cross-cutting invariants

- Physical recording chronology and repeated acoustic-content alignment
  are distinct coordinate systems.
- Discovery is not verification. Graph consistency is not independent
  acoustic certification.
- Weak hypotheses remain candidates until evidence eliminates them.
- Prefer long, distinctive and well-connected recordings as anchors,
  but reroute automatically when they fail.
- Source recordings are never destructively rewritten for synchronization.
- User-facing sync quality includes measured uncertainty, drift and
  any delivery compromises.
