# M18: Adaptive short-window and heldout spectral diagnostic

`probe-reserved` now chooses the **longest** viable acoustic window
from **8, 4, and 2 seconds** (subject to the mode's maximum), based only
on the known recording durations, proposed offset and the need to
reserve up to the requested independent holdout regions.

The schedule is frozen **before any acoustic samples are inspected**.
Both the training JSON and the verification report record
`effective_window_ns`. The verifier rejects unsupported overrides,
out-of-bounds reservations and validation events overlapping training.
For old M17 reports with no declared window duration, the verifier
retains the mode's original 8-second behavior.

This makes short-overlap recordings such as C0594 investigable without
automatically lowering the standard for C0599, which has enough duration
for 8-second windows. If even two disjoint training regions are
unavailable, the command fails closed.

The holdout report now includes spectral correlations for **three
spectral-flux bands** aligned at the measured acoustic offset, plus
explicit `spectral_error` and counts of spectral extraction failures.
These bands inspect the **same held-out event** as the waveform:
therefore they provide complementary acoustic corroboration, **not
independent statistical samples** and not an admission certificate.
A weak Pearson coefficient is not upgraded by counting the bands as
three independent wins.

For comparison:
```sh
opensync probe-reserved project.json index.json hypothesis-id rock-solid > train.json
opensync verify-holdout project.json index.json train.json rock-solid holdout.json
```

**Not implemented by M18:** globally distinctive event selection,
multiband *alternative-hypothesis search*, robust nonlinear drift,
cycle-consistent graph certification, automatic evidence-backed export
or source-referenced segments. Unresolved or genuinely unsyncable media
still inherits PluralEyes placements, and no unverified graph edge
can automatically replace them.
