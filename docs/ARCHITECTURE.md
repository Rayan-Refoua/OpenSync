# Architecture — Working Notes

Status: UNDECIDED

## Candidate stack

- Go: Application orchestration and concurrency
- FFmpeg: Media probing and audio extraction
- Native or web-based desktop interface
- SQLite: Potential persistent project/cache database
- Independent DSP and global matching engine

These are candidates, not commitments.

## Proposed logical stages

1. Media discovery and validation
2. Audio extraction/preparation
3. Acoustic feature generation
4. Candidate match discovery
5. Precise offset estimation
6. Global timeline consistency solving
7. Multi-point drift estimation
8. Non-destructive drift correction
9. Synchronization verification
10. Timeline presentation and NLE export

## Open technical decisions

- Native Go DSP versus specialized native DSP library
- Fingerprinting versus correlation versus hybrid matching
- Efficient search across hundreds of recordings
- L1/L2/robust drift fitting strategy
- Resampling and preservation of audio quality
- Nonlinear clock-drift support
- Timeline project schema and interchange format
- Windows-first packaging and future portability

## Design rules

Separate the matching engine from the interface.
Separate analysis features from original media.
Make every transform and estimated offset auditable.
Preserve original timebase and frame-rate metadata.
Use deterministic test fixtures where possible.
Benchmark both accuracy and throughput.

## Decision record

No final architecture decisions have been made.

## RAM-001 — Memory and cache policy

Status: Approved design direction
Date: 2026-11-07

- Adaptive, RAM-first processing.
- User-preferred configurable memory budget: 8 GiB.
- Default budget: auto-detected based on available RAM.
- Memory budget is a ceiling, not a preallocation.
- Stream decoding and reuse compact in-memory features.
- Avoid unnecessary intermediate media files.
- Disk spill permitted under memory pressure, with limits.
- Optional persistent analysis cache across sessions.
- Saved projects retain sync decisions without their cache.
- Validate cache entries against source and algorithm changes.
- Allow users to inspect, limit and clear disk caches.
- Benchmark peak RAM, disk writes and cache reuse.

Implementation details and automatic budget formula: TBD.
