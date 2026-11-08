# M22 — Exhaustive high-density acoustic matching experiment

The 17,262 quantized discovery-cycle residuals did **not** come from
the 8 ms hop of the existing spectral extractor. This standalone
research command deliberately takes a different approach: exhaustive
full-range cross correlation of acoustic transient envelopes, with a
2 ms hop (32 samples at 16 kHz) and a comparable 8 ms option.

```bash
opensync research-match project.json index.json reference-id query-id 2 new-result.json
```

## Mechanism

- Reads source-identity-checked cached 16 kHz mono PCM; no source
  transcoding, media changes, or project mutation.
- Uses continuous two-stage one-pole filter crossovers (~300/1800 Hz)
  to derive three frequency-region energy envelopes. Each 2 ms frame
  contributes log-energy difference, reducing sensitivity to constant
  microphone gain and silent/background energy.
- Uses an FFT convolution to compute the dot product for **every
  possible offset**, including negative shifts. Lag-specific mean and
  variance come from exact prefix sums over the available overlap,
  producing normalized Pearson correlations separately for each band.
- Requires **8 seconds** minimum overlap; zero-variance or silent
  segments cannot produce a match.
- Ranks peaks by mean positive band agreement. Returns up to 12
  alternative offsets separated by at least one second, including
  overlap duration and all three signed band correlations.
- Reports candidate **discovery** only. A high peak is not proof of a
  true correspondence; repeated audio, reverberation, gating, multiple
  microphones, content reuse, and drift may produce misleading peaks.
- The current research command is intentionally bounded to
  approximately 40 minutes per audio cache at 2 ms. It fails clearly
  rather than silently truncate source audio.

`2` and `8` are valid hop sizes in milliseconds. They are provided
for an apples-to-apples *search density* experiment using exactly the
same features and peak selection. This first investigation does **not**
implement original-source 48 kHz sampling, multi-resolution spectral
FFTs, or sub-sample waveform interpolation. These are follow-on
experiments if the full-range test demonstrates value.

## First real-media evaluation

Run both hops on the known positive pair:

- Reference `31f08e7b45fa7893dd31` (C3 2161)
- Query `c6bcec6397572eea2d44` (C0599)
- Prior independently measured constant offset is ~52.709 s.

Then investigate the candidate triangle without using quantized discovery
offsets as search bounds:

- C3 2161 ↔ C0600 (`6ac95182bb08c938b204`)
- C0599 ↔ C0600

Inspect top-12 alternative peaks, full-band agreement, and the
difference between coarse/fine positive-control rankings. A pair with
no convincing peak remains unresolved, not declared unsyncable.

For any apparently promising new match, run *independent* held-out
acoustic verification against that new proposed offset. M22 alone does
not construct a verified graph edge or justify XML replacement.

## Validation constraints

No automatic exporter integration, no self-certification and no project
mutation. Original PluralEyes placements remain the fallback.
The final audit contract still requires calibrated drift, graph
corroboration, 87-recording completeness and Premiere import.
