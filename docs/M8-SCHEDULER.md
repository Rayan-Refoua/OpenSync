# M8 — Progressive Acoustic Discovery

The scheduler produces **investigation priorities**, not synchronization proof.

`opensync schedule <project.json> <new-schedule.json>` inspects an existing
project file without opening the original media. The output includes a
candidate anchor order and a speculative traversal over discovery
hypotheses. Discovery windows, fingerprint votes, directory proximity and
recording length are not independently validated acoustic correspondences.

## Reference strategy

Long recordings with many *distinct candidate neighbors* are preferred as
initial anchors. Duration by itself never proves that a recording is the
master; a later recording may be a better acoustic reference. Competing
offsets (including repeated choruses or repeated performances) remain in
the queue. Physical recording time and shared-content position are separate
coordinate systems and must not be equated.

A frontier expansion in the static discovery plan is hypothetical: reaching
a recording via discovery does not make it verified. Only the external
evidence-verification stage may change project hypothesis admission status.

## Next step

After each attempted probe, a feedback-aware scheduler should retain
inconclusive candidates, avoid repeating an unchanged failing probe, and
reconsider which source to investigate. No automatic edge promotion is
permitted from waveform correlation alone. Recovery strategies (alternate
offsets, longer context, multiband features, drift/piecewise models) will
be added as independently tested capabilities.
