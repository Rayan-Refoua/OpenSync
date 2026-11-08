# M19 — Graph redundancy diagnostic (not certification)

```bash
opensync graph-audit project.json new-graph-report.json
```

This command reviews every non-rejected project hypothesis, using only
project-recorded offset candidates. It builds bidirectional timing arcs
under the convention `start(query) - start(reference) = offset_ns`.

It reports:
- All connected components, including isolated (unsyncable/unresolved) recordings.
- For each hypothesis, alternate **two- and three-hop** paths that do
  not reuse the tested hypothesis or a parallel hypothesis between the
  same two recordings.
- The IDs and statuses of all contributing hypotheses; the predicted
  offset and signed residual against the direct hypothesis.
- Whether the alternative-path list was truncated (cap: 24 paths per
  direct candidate; no conclusion should be drawn from an incomplete set).

For example, A→B = 1.000 s, B→C = 2.000 s, A→C = 3.005 s
produces a path prediction of 3.000 s and a +5 ms residual
for the A→C candidate. No arbitrary residual cutoff converts this
difference into an accepted/rejected match.

**CRITICAL:** The graph is currently built from discovered/corroborated
project hypotheses, *not certified edges*. A cycle supported by three
wrong pairwise acoustic matches can still look perfectly consistent.
Different paths may also share source events and cannot automatically
be counted as statistically independent evidence. This command must
not admit edges to the synchronization engine, update the project, or
modify any Premiere XML.

This is an investigative stage toward graph-based falsification and
auditable cross-recording corroboration. A future graph certificate
requires independently verified event provenance, uncertainty bounds,
drift/discontinuity modeling and cycle consistency on held-out evidence.

The final release audit still requires the 87-video/174-audio complete
timeline, PluralEyes fallback retention, independent verification,
negative controls and real Premiere import testing.
