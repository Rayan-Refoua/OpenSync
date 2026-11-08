# Pre-reserved acoustic validation (M17)

`probe-reserved` allocates non-overlapping training and independent
validation windows **before reading acoustic data**.

```sh
opensync probe-reserved project.json index.json hypothesis-id rock-solid > train.json
opensync verify-holdout project.json index.json train.json rock-solid holdout.json
```

A deterministic schedule holds back up to the quality preset's
validation budget (Draft 0 / Normal 1 / Rock Solid 3), keeping at least
two non-overlapping training regions where sufficient audio exists.
The algorithm reports `available_disjoint_regions` and
`reserved_holdout_query_seconds`. If even two training regions do
not exist, the command errors instead of inventing a valid model.
Rock Solid may legitimately reserve fewer than three holdouts on
short-overlap media; the verifier reports insufficient independent
duration without pretending to have rejected a match.

In `verify-holdout`, reserved positions are checked against every
attempted training interval, including failed attempts, and source
bounds. The verifier fits constant/affine **only from training data**;
the reserved windows are not consulted for model selection.

The legacy `probe` command is unchanged for compatibility. Existing
M9/M12 reports can be passed to `verify-holdout` with the existing
post-training allocation, but they cannot retroactively claim to have
pre-reserved validation audio.

This is an allocation/diagnostic stage, **not independent evidence
certification**. It does not validate hypotheses against the global
acoustic graph, resolve unrelated repeated sounds, perform nonlinear
drift fitting or authorize export replacements. PluralEyes fallback
remains in place for unresolved and genuinely unsyncable recordings.
