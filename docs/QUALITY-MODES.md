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
