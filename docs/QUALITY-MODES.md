# Quality modes

Quality modes select effort. They never relax the requirement that
unverified matches remain provisional. All modes preserve source media,
caches, alternative offsets and provenance for refinement.

| Mode | Acoustic probe windows | Controls | Intent |
| --- | ---: | --- | --- |
| Draft | 3 | Valid alternative offsets | Rapid approximate layout; unresolved allowed |
| Normal | 5 | Broader valid alternatives | Everyday balance with later basic drift verification |
| Rock Solid | 10 | Dense valid alternatives | Held-out, graph and drift investigations; reject insufficient evidence |

A preset's requested held-out checks and drift models are declared as
future orchestration requirements, NOT features claimed as completed.
The current `probe` command remains a diagnostic measurement, not
an independent hold-out verifier or graph certification procedure.

Inspect a preset:
```sh
opensync preset rock-solid
```

Run preset-aware diagnostics:
```sh
opensync probe project.json cache-index.json hypothesis-id normal
opensync probe project.json cache-index.json hypothesis-id rock-solid
```

The four-argument legacy invocation without a mode defaults to Normal.
`sock-solid` is an accepted alias for `rock-solid`.

Future automatic pipeline: mode-specific candidate breadth and recovery,
distinct training/held-out acoustic events, explicit wrong-offset
competition, constant/affine/nonlinear clock analysis, graph
consistency checks and a decision based on evidence rather than a
single arbitrary correlation threshold. All modes should reuse stored
evidence rather than rerunning unnecessary expensive stages.

## Chronological-first investigation

Use `opensync sequence-plan <project.json> <new-output.json>` before
broad candidate recovery to enumerate A → B → C filename-counter chains
within a consistent source directory, filename prefix and extension.
The output distinguishes available direct discovery hypotheses from
adjacent recordings with no discovery match. In `recover`, direct numbered
neighbors are investigated first, then the same numbered series, then
other references, within the Draft/Normal/Rock Solid candidate budgets.

This is **investigation priority only**. Numeric filename order is not
trusted capture time, gapless continuity, or acoustic correspondence.
A sequential pair with no acoustic overlap may still give useful
chronological ordering but supplies no synchronization offset. No implicit
A → C timing edge is synthesized from A → B → C without independently
supported timing and clock continuity.
