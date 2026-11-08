# M20 — Untruncated two-hop graph triage

The M19 run found 1,558 discovery hypotheses, with 1,554 of 1,558
edge audits truncated by the 24-path cap. That output cannot establish
how consistent or inconsistent the candidate graph actually is.

```bash
opensync graph-triage project.json graph-triage.json
```

M20 constructs one *investigative representative* per unordered pair
of recordings. All other hypotheses are **retained explicitly** as
alternatives, never deleted or discarded from the original project.
The ranking only sorts candidate hypotheses for tractable inspection:
number of distinct observation event IDs (not independent certification),
then discovery support windows, then discovery votes, then stable ID.

For every representative pair A–B the tool examines **every other
recording C** to find the two-hop A–C–B route, using the representative
hypothesis for each leg. It reports the signed difference in nanoseconds
between direct and indirect offsets, and the IDs of contributing
hypotheses. Thus the two-hop audit cannot be saturated by repeated
hypotheses on one pair. It has no arbitrary cap of 24 alternate paths;
every distinct intermediate source is inspected once.

The report distinguishes input hypotheses, unique source pairs,
retained alternative hypotheses, diagnostic two-hop paths, and isolated
components. Rejected hypotheses do not enter topology, but no project
candidate is upgraded to verified status.

**Limitations:** This is still a diagnostic topology built from
uncertified discovery matches. Its representative-selection policy is
not an acoustic quality threshold, and perfectly closed cycles can
still be false. Different two-hop paths can share correlated source
events. Two-hop inspection does not replace three-hop/global reasoning,
held-out acoustic comparisons, drift uncertainty or a cycle-verified
constraint solver. It must never authorize Premiere replacements.

The old `graph-audit` remains available for raw hypothesis path
enumeration and historical comparison. The final 87-video Premiere
XML remains governed by the baseline-preserving fallback policy and
the independent release audit.
