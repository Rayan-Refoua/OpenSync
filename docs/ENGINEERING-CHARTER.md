# OpenSync Engineering Charter

Status: Direction approved; architecture exploratory.
Updated: 2026-11-07

## Mission

Build a free, community-owned, professional-grade
multicamera audio/video synchronization system.

Target: At least PluralEyes-class reliability, with
modern algorithms, fast processing and near/subframe
accuracy when supported by the recorded signal.

## Non-negotiable principles

1. Accuracy and reliability before feature count.
2. Never silently accept ambiguous or bad matches.
3. Drift estimation and correction are core features.
4. Measure residual error, not just sync success.
5. Original recordings remain untouched.
6. Headless core, independent of GUI.
7. CLI, GUI and NLE integrations share the same engine.
8. Multiple matching strategies compete/cooperate.
9. Design for ordinary hardware; GPU optional.
10. Real recordings and synthetic tests are mandatory.
11. Preserve checkpoints, milestones and evidence.
12. Keep dependencies justified and installation simple.

## Research strategy

PluralEyes provides a behavioral benchmark, not
a blueprint whose old implementation must be copied.

Study original documentation and observed behavior.
Research modern DSP, acoustic fingerprints, robust
optimization and optional learned representations.

Require benchmark evidence before selecting algorithms.

## Accuracy targets

- Target subframe timing error for suitable recordings.
- Measure in milliseconds at multiple timeline positions.
- Report residual drift separately from initial offset.
- Detect unreliable/ambiguous results explicitly.
- Establish thresholds per test scenario and frame rate.
- Do not claim guaranteed subframe accuracy for all media.

## Performance targets

- Avoid quadratic full-signal pairwise comparisons.
- Use candidate indexing and coarse-to-fine matching.
- Cache reusable analysis features.
- Stream processing where practical.
- Parallelize without sacrificing determinism.
- Benchmark CPU, memory and throughput.

Concrete performance budgets: TBD after corpus design.

## UI

Windows-first, minimal, editor-oriented, responsive.
WebView2 is a candidate, not an architecture decision.
The application must remain functional without a GUI.

## Testing

Include:
- Known offsets and clock drift
- Multi-hour recordings
- Bad microphones and distant acoustic sources
- Silence, noise and repeated audio
- Short clips and disconnected recordings
- Incorrect timecode and misleading metadata
- Frame-rate and sample-rate variations

Track accuracy, false positives, false negatives,
runtime, memory use and correction artifacts.

## Development continuity

- Commit at completed, coherent milestones.
- Maintain concise research and decision records.
- Update milestone status when evidence changes.
- Keep executable tests as a permanent reference.
- Record blockers and the next concrete action.
- Do not rely on chat context for project state.

## Current next action

Continue PluralEyes behavioral and algorithm research.
Then define benchmark corpus before implementation.

No production stack or algorithm has been finalized.
