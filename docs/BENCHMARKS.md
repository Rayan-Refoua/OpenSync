# Synchronization benchmarks

OpenSync is being developed as a practical, independently implemented alternative for synchronizing audio and video recordings. We intend to publish **reproducible comparisons** with other synchronization tools—not marketing scores.

**Status: cross-application benchmark suite planned. No comparable results have been published yet.** Results will be added after the primary OpenSync workflow and evaluation corpus are stable.

## Comparison matrix

| Product | End-to-end runtime | Correctly synchronized clips | Timing error / drift | Unresolved and false matches | Export / usability | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| [OpenSync](../README.md) | Not tested | Not tested | Not tested | Not tested | Not tested | Pending |
| [Align](https://github.com/FedyaLight/align) | Not tested | Not tested | Not tested | Not tested | Not tested | Pending |
| Red Giant PluralEyes 4 | Not tested | Not tested | Not tested | Not tested | Not tested | Pending |
| Syncaila | **Awaiting volunteer** | **Awaiting volunteer** | **Awaiting volunteer** | **Awaiting volunteer** | **Awaiting volunteer** | No license available to the maintainer |

**Help benchmark Syncaila:** We currently do not have access to a Syncaila license. If you use Syncaila, please [open a GitHub issue](https://github.com/Rayan-Refoua/OpenSync/issues) to volunteer a comparable run, or submit reproducible results, logs, and screenshots where licensing permits. We will credit contributors with their permission. A volunteer's results will be labeled *independently contributed* and will not be presented as a controlled same-machine comparison unless the environment actually matches.

An empty cell means **not measured**, not that a product failed or lacks the capability. Do not replace missing values with estimates.

## Fair comparison protocol (draft)

1. **Freeze the dataset first.** Include clean multitrack recordings, different microphones and acoustics, very short overlaps, interruptions, repeated sounds, long recordings, clock drift, and unrelated/unsynchronizable files. Include both audio-only and mixed audio/video cases. Identify datasets with permissions and share checksums; distribute media only when allowed.
2. **Establish independent ground truth.** Use known synthetic offsets and/or manually verified real-world event alignments. Report uncertainty and per-clip/per-segment timing errors rather than treating one application's timeline as ground truth.
3. **Use the same inputs.** Record exactly which media and time spans each application received. Do not silently omit difficult files. Where a tool cannot import a format, disclose any preprocessing and benchmark its cost separately.
4. **Measure the complete workflow.** Separately report import/preparation, synchronization, verification/manual intervention, and export. Include end-to-end wall time, peak resident memory and disk/cache use wherever measurable; indicate cold versus warm caches.
5. **Score quality before speed.** Report correctly aligned recordings, precision and coverage, falsely aligned recordings, unresolved recordings, maximum and median absolute timing error, and drift error measured at multiple timestamps. Record whether the exported editing timeline preserves these results.
6. **Repeat and disclose conditions.** Document app versions, settings, hardware, OS, source locations, cache state, run count, and timing method. Where possible run at least three trials per configuration and show individual results and median. Avoid assuming one universal winner.
7. **Preserve evidence.** Link structured results, anonymized manifests, relevant screenshots, hashes, procedures and known limitations. Keep privacy-sensitive recordings private unless explicitly cleared.

## Planned test tiers

| Tier | Goal | Acceptance evidence |
| --- | --- | --- |
| Small controlled | Accuracy, false matches, frame/subframe timing | Known-ground-truth offsets; explicit tolerance |
| Mixed field recordings | Different microphones, distance, compression and noise | Independently reviewed events and uncertainty |
| Large multitrack project | Scalability with hundreds of clips | Runtime, memory, coverage, completed export |
| Drift and discontinuities | Long takes, nonlinear clocks, CCTV timestamps | Timing errors at several points; clear treatment of discontinuities |
| Editor round trip | Usable results, not just a plausible on-screen timeline | NLE import and inspection, preservation of clip placement |

## What is already measured (not a product comparison)

OpenSync has internal tests and engineering benchmarks documented in [engine acceptance](ENGINE-ACCEPTANCE.md) and the [native Go migration report](NATIVE-GO-MIGRATION.md). These measure OpenSync's implementation and changes across versions; **they do not constitute a fair OpenSync vs. Align vs. PluralEyes vs. Syncaila comparison**, and must not be displayed as cross-product results.

## Contributing benchmark results

Please [open an issue](https://github.com/Rayan-Refoua/OpenSync/issues) with the subject **Benchmark contribution: <product>** and include the product/version, operating system, CPU/RAM, input clip counts and total duration, dataset identifier or hashes, settings, cache state, exact timing boundaries, output artifacts, and verification method. Do not upload copyrighted or private recordings without permission.

We welcome results that expose OpenSync's weaknesses as much as its strengths. Corrections to published measurements should be traceable to a specific run and documented openly.
