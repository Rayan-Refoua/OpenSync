# M21 — Precision acoustic evidence graph (strictly diagnostic)

M20 established that 17,262 / 17,262 discovery cycle residuals are
multiples of 128 milliseconds. Quantized discovery offsets are useful
for proposing candidate relationships but cannot substitute for precise
acoustic measurement.

M21 adds a **separate** measured graph:

```bash
opensync measured-graph project.json reports-manifest.json new-measured-graph.json
```

The manifest is JSON with absolute or current-working-directory paths:

```json
{
  "version": 1,
  "reports": [
    {"training": "/path/C0594-training.json", "holdout": "/path/C0594-holdout.json"},
    {"training": "/path/C0599-training.json", "holdout": "/path/C0599-holdout.json"}
  ]
}
```

The CLI accepts M17/M18 `probe-reserved` training files and their
`verify-holdout` outputs. It checks hypothesis identities against the
immutable project, matches training and heldout provenance, requires
non-overlapping pre-reserved independent windows and available negative
controls, and checks actual measured event bounds. The resulting event
offsets come from **held-out acoustic measurements**, never discovery
hypothesis offsets or graph-solved estimates.

Each acoustic window is one distinct event. The three spectral-band
correlations are stored *inside that same event*; they are not counted
as three independent observations. Raw measured offsets and full
per-event provenance remain in the diagnostic report.

For candidate constant-offset edges only, a median measured offset and
the observed maximum deviation are recorded in nanoseconds. Two-hop
graph paths are examined only when **two other measured pairwise
relationships** exist; missing paths are explicitly distinguished from
residuals outside the summed observed deviations. The empirical spread
is **not** a calibrated probabilistic uncertainty bound, and the
comparison is not a significance or certification test.

Affine candidates (e.g. short-overlap C0594) are retained with all
events as `deferred_candidates` and do not become static graph edges;
they require time-varying motion-aware graph modeling.

Multiple hypotheses for one recording pair are retained separately.
Only one provisional measured representative per pair is used for
path diagnostics, not for accreditation or export authorization.

**Fail closed:** Every report is marked
`MEASURED_GRAPH_DIAGNOSTIC_NOT_CERTIFIED`,
`acoustic_certification: not_assessed` and
`export_authorization: denied`. This command does not call
`syncgraph.Admit`, mutate project state or create a Premiere XML.
Existing fallback exporter accepts only separately externally reviewed
placements, so these measurement reports cannot authorize its edits.

Remaining work includes authenticated frozen report hashes, truly
independent feature families and event provenance across recording
pairs, calibrated timing uncertainties, robust model selection for
affine/nonlinear/segment discontinuities, full cycle consistency,
machine certification and the final real-media 87-video Premiere XML
audit and import.
