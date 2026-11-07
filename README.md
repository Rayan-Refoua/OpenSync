# OpenSync

An independent, community-driven audio/video synchronization
application inspired by the capabilities of Red Giant PluralEyes.

Status: Research / Discovery
Project name: Provisional
License: To be selected before publishing code

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

- docs/RESEARCH.md: Observations from PluralEyes 4
- docs/MILESTONES.md: Project roadmap and acceptance criteria
- docs/ARCHITECTURE.md: Architecture and open decisions

## Development protocol

Use the Human Execution Interface (HEI):

- AI handles research, reasoning, planning and verification.
- Human executes native commands and GUI operations.
- Prefer deterministic MSYS/Git Bash commands.
- Preserve important findings in Git.
- Do not rely on chat history as project documentation.

## Current scope

Research first. No production architecture is finalized.
