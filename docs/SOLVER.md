# OpenSync — temporal graph solver (first production integration)

This is application code in `internal/syncgraph`, not a standalone research experiment.

## Coordinate contract

- Constraint: `start(Query) - start(Reference) = OffsetNS / 1e9`.
- The first lexicographic recording in each connected component is at relative time zero; independently connected components have no known relative placement.
- Each graph constraint has a strictly positive finite relative weight. Inverse-variance weights are appropriate only when independently calibrated.
- Multiple edges from the *same* acoustic event must not be treated as independent evidence.
- Cycle rank `edge_count - node_count + 1` counts redundant connections. A tree's zero fit residual proves nothing about whether an edge is acoustically true.
- Output residual is `predicted_offset - observed_offset` in seconds; there is **no production accept/reject threshold** in this solver.

## Verification boundaries

`Graph.Admit` remains fail-closed on externally supplied verification fields. The new `Graph.Solve` reads the admitted graph without modifying it; it does not validate those fields or certify timing. `SolveOffsets` accepts an explicit diagnostic set of candidate constraints, not necessarily admitted edges.

`opensync solve <constraints.json>` prints diagnostic JSON, with `DIAGNOSTIC_ONLY_NOT_ACOUSTICALLY_CERTIFIED` and `timeline_modified=false`. It does not write project state or media.

This initial implementation fits **constant relative offsets only**. Linear and nonlinear drift, model selection, robust outlier rejection, independent holdout validation, and evidence provenance belong to upcoming engine increments. An isolated false bridge is unobservable to graph consistency; acoustic evidence must validate it.

## Development path

1. This integration: production native Go graph offset solver, deterministic components and residual diagnostics, read-only CLI, regression tests.
2. Persistent evidence ledger: nodes, acoustic observations, competing hypotheses, explicit external verification provenance; preserve original measurements and rejections.
3. Connect existing Go feature index and discovery/verification to the graph; no automatic admission from fingerprint counts.
4. Graph-guided adaptive discovery and bounded waveform matching with resource budgets.
5. Risk-prioritized independent verification, drift-aware timing models, then Draft → Standard → Rock Solid upgrades on the same cached project.
6. Timeline export and minimal WebView2 interface only after certification and reproducible fixtures are established.

Synthetic regression tests validate numerical behavior, not real-audio correctness or application-level certification. Existing `inspect` workflow is preserved.
