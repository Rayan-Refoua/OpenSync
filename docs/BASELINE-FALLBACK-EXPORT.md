# Baseline fallback export (M14)

The exporter starts with the PluralEyes timeline, retaining all 87 clips
and their original linked audio. An unresolved or genuinely unsyncable
recording retains its original PluralEyes position and is labelled
`pluraleyes_baseline_unverified`.

```sh
opensync export-baseline-fallback pluraleyes.xml project.json approvals.json output.xml output-manifest.json
```

`approvals.json` starts as `{"version":1,"placements":[],"allowed_lost_overlap_pairs":[]}`.
That exports the unaltered baseline (provided all project recordings and
linked audio can be audited) and a per-recording provenance manifest.

To override a placement requires a fully identified recording, exact
original frame, new frame, an external evidence reference and explicit
`approval:"externally_reviewed"`. Example:

```json
{
  "version": 1,
  "placements": [{
    "recording_id": "project-local-id",
    "expected_start_frame": 10,
    "new_start_frame": 130,
    "evidence_reference": "human-reviewed-check-001",
    "approval": "externally_reviewed"
  }],
  "allowed_lost_overlap_pairs": []
}
```

The override is *not* automatically certified: the current engine
cannot produce independently verified acoustic evidence. The manifest
labels reviewed positions `externally_reviewed_not_independently_certified`.

Placement changes move source-linked video and audio together and must
pass exact identity, structural, timing and lane collision checks.
Loss of any PluralEyes timeline-overlap pair blocks export unless the
exact lost pair is explicitly whitelisted in
`allowed_lost_overlap_pairs` (each pair has sorted project recording
IDs `first` and `second`). Unused approvals are rejected to catch
stale input. Lost overlaps are *review flags*, not an objective proof
of worse synchronization. The exporter never treats newly overlapping
video clips as acoustically verified matches.

Output paths are exclusive-create; no original media, baseline XML or
project state is modified. Draft, Normal and Rock Solid share this
safety/provenance contract; evidence depth and search strategy are
mode-specific. Multi-threading remains deferred until validation,
drift and graph design are stable.

**Not yet implemented:** An OpenSync-produced independent verification
certificate and fully automatic evidence-gated placement. Before
enabling automatic replacement, implement held-out validation with
source-event provenance, drift model comparison and graph consistency,
then replace the external review input with signed/validated internal
evidence rather than relying on an arbitrary `verified` flag.
