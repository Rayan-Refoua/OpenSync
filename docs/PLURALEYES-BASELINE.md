# PluralEyes baseline and genuinely unsyncable recordings

PluralEyes is the **minimum timeline coverage baseline**, not an
independent acoustic ground truth. Preserve the positions and linked
audio of recordings that OpenSync cannot uniquely synchronize.
Do not fabricate a correspondence or assign false confidence simply
to fill out the 87-recording timeline.

## Compare before replacing placements

```sh
opensync baseline-compare pluraleyes.xml opensync-candidate.xml project.json comparison.json
```

The report counts all project recordings, video positions inherited or
changed, pairwise temporal **overlap** in the two Premiere timelines,
retained overlaps, newly overlapping pairs, and lost overlaps. It also
checks unchanged source in/out, tracks and audio/video movement.

**Important:** a temporal overlap pair is only a proxy for timeline
coverage. It is NOT a measured or independently verified acoustic match;
new overlaps are never claimed as newly synchronized pairs. Loss of a
PluralEyes overlap must be reviewed, but an independently justified
correction may legitimately remove a false baseline overlap.

## Product invariant

- `pluraleyes_baseline_unverified`: clip position retained as fallback,
  which may include recordings with no matchable audio.
- `opensync_independently_verified`: future placement backed by held-out
  acoustic evidence, appropriate timing model and graph consistency.
- `unresolved_without_baseline`: report openly; never guess an offset.

All three presets (Draft, Normal, Rock Solid) must preserve baseline
coverage rather than invent positions for unsyncable media. Presets
control investigation effort and the confidence/evidence contract, not
whether uncertainty may be hidden. No feature currently labels acoustic
verification as complete based solely on numeric score or inherited
PluralEyes position.

Future automated export should require explicit review of missing
baseline recordings and lost baseline overlap pairs, display provenance,
and expose coverage improvement and independently verified improvements
as *separate* measurements. Final verification requires actual media
evidence and ideally visual spot checks, not overlap counts alone.

Parallel execution is deferred until the deterministic single-worker
algorithm and evidence model are stable.
