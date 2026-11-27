# Testing and limitations

OpenSync is a public alpha. The engine acceptance benchmark is advisory and has known gaps. Premiere XML has structural checks but has not been certified for every Premiere version and media type.

Validation spans unit tests for timing, graph and DSP behavior, synthetic fixtures with known timing, FFmpeg integration tests, real-world projects, structural XML audits, and actual editor import checks. Each layer answers a different question. Another synchronizer's output is a comparison reference and may contain errors.

Repeated music, applause, short clips, quiet audio, room reflections, inaccurate clocks, and overwritten timestamps can produce ambiguous or misleading evidence. Inspect the evidence and review suspicious placements.

For decoder changes, compare sample boundaries and decoded PCM, fingerprints, candidate coverage, verification outcomes, timing models, exports, resource use, and end-to-end runtime. Include long sources and both sequential and random storage behavior.

See [Engine acceptance](../ENGINE-ACCEPTANCE.md), [Synthetic benchmark](../SYNTHETIC-BENCHMARK.md), and [Final audit contract](../FINAL-AUDIT-CONTRACT.md).
