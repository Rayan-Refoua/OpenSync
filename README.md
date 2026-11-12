# OpenSync

An independent, community-driven audio/video synchronization
application inspired by the capabilities of Red Giant PluralEyes.

Status: Native Go headless engine; Premiere import acceptance pending
Project name: Provisional
License: GNU GPL v3.0 or later (GPL-3.0-or-later)

## License

OpenSync is distributed under the GNU General Public License, version 3
or (at your option) any later version (`GPL-3.0-or-later`). See [LICENSE](LICENSE)
for the full text. Modifications and redistributions must comply with its
terms. Contributions should be submitted under this same project license.

## Mission

Build a free, reliable, high-performance synchronization tool
that meets or exceeds PluralEyes 4's practical capabilities.

Critical requirements:

- Automatic synchronization of many recordings
- Arbitrary recording start/stop times
- Multiple camera and audio recorder tracks
- Robust matching despite microphone/acoustic differences
- Accurate audio drift detection and correction
- Detection of ambiguous and unmatched clips
- Non-destructive workflows
- Timeline inspection and manual intervention
- Export to professional video editing applications
- Reproducible tests and measurable accuracy

## Engineering principles

1. Independent implementation, not copied proprietary code.
2. Evidence-based design and measurable performance.
3. Original media remains unchanged by default.
4. Drift correction is a core capability.
5. No silent synchronization errors.
6. Local/offline processing by default.
7. Small, maintainable development toolchain.
8. Portable architecture where practical.

## Documentation

- docs/ENGINE-ACCEPTANCE.md: Current test-first milestone, runnable benchmark,
  measured failures and the path to a usable engine before UI work
- docs/RESEARCH.md: Observations from PluralEyes 4
- docs/MILESTONES.md: Project roadmap and acceptance criteria
- docs/ARCHITECTURE.md: Architecture and open decisions

## Development

Build and test with Go 1.23 or newer. Install FFmpeg and FFprobe for media
decoding and the integration tests. The production runtime does not require
Python; historical Python reference code has a separate CI job.

```powershell
go test ./...
go vet ./...
$env:OPENSYNC_INTEGRATION = '1'
go test ./internal/autosync
```

Keep source recordings, caches, generated timelines and local experiments out
of Git. Synthetic tests generate temporary media and clean it up automatically.
Preserve implementation decisions and measured results in `docs/`.

## Current scope

The synchronization runtime is native Go, with FFmpeg/FFprobe for media I/O.
Python is only an optional historical development reference and is not shipped.

```powershell
go build -o opensync.exe ./cmd/opensync
./opensync.exe sync --file-list media-files.json --output MyProject
./opensync.exe sync --folders CameraFolder AudioFolder --output AnotherProject
./opensync.exe export MyProject/OpenSync.opensync --output Export
```

The engine saves a precise project and a Premiere XML timeline, preserves
unresolved recordings, and reads original media without copying it. Caching is
disabled by default: analysis stays in memory, with no disk spill. Folder inputs
scan recursively and report unreadable/non-media files. Use `--cache` only to opt
into persistent analysis; `--resume` explicitly reuses it. See
[the native migration and validation report](docs/NATIVE-GO-MIGRATION.md)
for performance, compatibility, and the remaining manual import check.

Large recordings and reference searches are split into bounded analysis buckets
shared across CPU workers. Progress reports show active and queued work plus
audio waits. Compatible PCM WAVs use exact native sample reads; formats requiring
conversion retain FFmpeg. The worker count follows available CPU and RAM, while
disk reads and source conversion have separate concurrency limits.

The older research benchmark below remains available as a separate diagnostic;
its historical readiness gates are not a substitute for the native pipeline's
integration and regression tests.

Validate the headless engine before building the UI. Run
`go run ./cmd/opensync benchmark --output experiments/acceptance-new --profile quick`
for an HTML/JSON acceptance report. A nonzero result currently exposes measured
failures and missing capabilities; passing unit tests alone is not sync accuracy.
Use `--profile full` for two-hour local timing tests (approximately 1 GB of fixtures).
See `docs/ENGINE-ACCEPTANCE.md` for the test contract and real-pair review workflow.
