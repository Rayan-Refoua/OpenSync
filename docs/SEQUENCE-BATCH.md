# Chronology-first acoustic batch

After generating a sequence plan, run direct acoustic investigations for
the numbered adjacent pairs before unrelated cross-camera alternatives:

```sh
opensync sequence-probe project.json cache-index.json sequence-plan.json rock-solid new-output-directory
```

The three presets control hypotheses **per sequential pair**: Draft
tries one discovery alternative, Normal up to two, and Rock Solid all
available alternatives. These are investigation budgets, not acceptance
thresholds. The command writes each raw `sequence-NNN-hyp-NN.json`
and a `sequence-batch.json` summary, retaining failures and offsets
without changing the project or source media. The output directory
must not already exist.

The first 87-recording test set's uploaded sequence plan has **79**
adjacent numbered pairs, **37** with **56** discovery hypotheses and
**42** without any direct discovered acoustic hypothesis. The preceding
71-hypothesis sweep contains **zero** of these 56 exact hypotheses.
The 42 missing pairs require a separate chronological metadata or
bridging investigation, not fabricated acoustic offsets.

No sequence-based candidate, including a winning negative-control
comparison, is considered verified without independent held-out acoustic
validation and consistency checks. Recordings with no shared audio may
have chronology but not an acoustic offset. Original media remains
read-only.
