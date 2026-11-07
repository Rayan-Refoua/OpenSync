# Milestones

All milestones are provisional until research concludes.

## M0 — Research foundation [IN PROGRESS]

- [x] Identify packaged dependencies
- [x] Inspect executable import tables
- [x] Discover snippet matching diagnostics
- [x] Discover drift estimation diagnostics
- [x] Identify audio processing classes
- [x] Identify metadata graph diagnostics
- [x] Establish project Git repository
- [ ] Inspect key functions with Binary Ninja
- [ ] Document complete observable sync workflow
- [ ] Document supported inputs and outputs
- [ ] Capture reference performance and behavior

Exit: Sufficient evidence to propose architecture.

## M0.5 — Progressive engine design [DOCUMENTED]

- [x] Establish evidence-driven progressive matching policy
- [x] Define graph-consistency requirements
- [x] Define contextual retry and short-clip strategies
- [x] Separate source identity from synchronization timing
- [ ] Validate strategies experimentally in M1/M2

Reference: docs/PROGRESSIVE-ENGINE.md

## M1 — Requirements and test corpus

- [ ] Define functional requirements
- [ ] Define sync accuracy metrics
- [ ] Generate known-offset test recordings
- [ ] Generate known-drift test recordings
- [ ] Include noisy and distant microphones
- [ ] Include intermittent camera recordings
- [ ] Include silence and repeated audio
- [ ] Define objective benchmark versus PluralEyes

Exit: Repeatable and measurable reference tests.

## M2 — Audio analysis prototype

- [ ] Decode representative audio formats
- [ ] Normalize and downmix analysis signals
- [ ] Extract reusable acoustic features
- [ ] Implement candidate discovery
- [ ] Implement precise offset estimation
- [ ] Measure false-positive matching rates

Exit: Reliable offset matching on test corpus.

## M3 — Global synchronization

- [ ] Build pairwise match graph
- [ ] Resolve connected recording groups
- [ ] Detect inconsistent match cycles
- [ ] Reject outlier relationships
- [ ] Handle unmatched/ambiguous clips
- [ ] Preserve uncertainty/confidence information

Exit: Correct multi-recording timelines.

## M4 — Drift detection and correction [CRITICAL]

- [ ] Estimate offset at multiple time positions
- [ ] Fit relative recorder-clock drift
- [ ] Reject anomalous measurements
- [ ] Quantify residual alignment error
- [ ] Correct audio timing without changing originals
- [ ] Revalidate synchronization after correction
- [ ] Test one-hour and multi-hour recordings
- [ ] Evaluate nonlinear/piecewise drift support

Exit: Stable alignment across full recording duration.

## M5 — Application and workflow

- [ ] Batch media import
- [ ] Audio/video track organization
- [ ] High-density timeline visualization
- [ ] Waveform preview
- [ ] Matching diagnostics
- [ ] Manual alignment override
- [ ] Persistent project format
- [ ] Resume and cache management

Exit: Usable end-to-end desktop workflow.

## M6 — Professional export

- [ ] Define non-destructive timeline model
- [ ] Export FCP XML or compatible interchange
- [ ] Evaluate OTIO and other NLE formats
- [ ] Preserve clip timing and frame rates
- [ ] Handle drift-corrected audio deliverables
- [ ] Validate imports in target NLEs

Exit: Editing-ready synchronized projects.

## M7 — Open-source release

- [ ] Decide project license
- [ ] Audit third-party licenses
- [ ] Publish build instructions
- [ ] Add automated tests
- [ ] Document limitations
- [ ] Package Windows release
- [ ] Publish benchmark results

Exit: Public release with reproducible tests.

## Non-negotiable acceptance requirements

- No silent incorrect matches.
- No destructive changes to original files.
- Drift correction must be quantitatively tested.
- Long recordings must be included in benchmarks.
- Progress must survive interrupted sessions.
