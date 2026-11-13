# OpenSync

**Synchronize audio and video recordings—openly, accurately, and without locking your work into one editor.**

OpenSync is a free, open-source project for aligning recordings from cameras, audio recorders, and other devices on a shared timeline. It is built for filmmakers, editors, event producers, researchers, and anyone working with multiple recordings of the same event.

The goal is simple: **spend less time lining up clips, and more time creating.**

> **Development status:** OpenSync currently has a native Go command-line synchronization engine. A desktop timeline and waveform interface are planned but **not yet available**. The Premiere XML export workflow is implemented; final hands-on import compatibility testing is still in progress. Downloads and packaged releases will be added as they become available.

## Why OpenSync?

- **Automatic synchronization** — discover relationships between recordings using their audio, even when cameras and microphones sound different.
- **Designed for real-world recordings** — account for differing start times, partial overlaps, clock drift, and difficult acoustic conditions.
- **No silent guesswork** — keep uncertain and unresolved recordings visible instead of pretending everything synchronized successfully.
- **Non-destructive by design** — leave your original media untouched, with synchronization decisions saved in an OpenSync project.
- **Editor-friendly output** — export a Premiere-compatible FCP7 XML timeline; additional editing workflows remain a future goal.
- **Offline and independent** — run locally, inspect the implementation, and help shape the roadmap.

Not every recording can be synchronized reliably. OpenSync aims to make its evidence and limitations clear, not to promise a match where there isn't one.

## Getting started

**Prebuilt downloads:** Coming soon. For now, OpenSync can be built from source.

You will need [Go](https://go.dev/dl/) **1.23 or newer**, [FFmpeg](https://ffmpeg.org/download.html), and FFprobe available on your PATH.

```bash
git clone https://github.com/Rayan-Refoua/OpenSync.git
cd OpenSync
go build -o opensync ./cmd/opensync

# Synchronize media from one or more folders.
./opensync sync --folders CameraFolder AudioFolder --output MyProject

# Or supply individual recordings.
./opensync sync --files camera.mp4 recorder.wav --output MyProject

# Re-export a saved OpenSync project.
./opensync export MyProject/OpenSync.opensync --output Export
```

On Windows, name the built executable `opensync.exe` (for example, `go build -o opensync.exe ./cmd/opensync`) and invoke it as `./opensync.exe` or `.\opensync.exe` in your shell.

The synchronization command saves an editable `.opensync` project and an XML editing timeline. Recordings without sufficient evidence remain identified as unresolved. Analysis caching is optional; use `--cache` when you want reusable analysis files and `--resume` for an explicitly cached interrupted run. Run `opensync --help` for current CLI options.

## Where we're heading

OpenSync is being built step by step, with reliability taking priority over a polished screenshot.

| Area | Current direction |
| --- | --- |
| Native synchronization engine | Available for development and testing |
| Evidence-based matching and drift handling | Implemented, with ongoing validation on difficult recordings |
| Saved project and editor export | Available; practical round-trip acceptance ongoing |
| Interactive audio waveform timeline | Planned next |
| Video preview and synchronized playback | Planned |
| Convenient desktop downloads | Planned |

This is an actively developed project, **not yet a finished replacement for every PluralEyes workflow**.

## Independent benchmarks

We intend to evaluate **OpenSync, [Align](https://github.com/FedyaLight/align), Red Giant PluralEyes 4, and Syncaila** against shared datasets, measuring synchronization accuracy, unresolved/incorrect matches, drift, runtime, memory, and export usability.

**No comparable cross-application benchmark results have been published yet.** We won't present internal optimization numbers as evidence that OpenSync outperforms another product.

We don't currently have a Syncaila license. **If you own Syncaila and can help run a reproducible benchmark, we'd love your help.** See the [benchmark plan, comparison table, and contribution instructions](docs/BENCHMARKS.md).

## Join the project

OpenSync is intended to benefit everyone who needs dependable synchronization tools. You can help by testing difficult recordings, reporting bugs, improving documentation, validating exports in editing software, proposing enhancements, or contributing code.

- [Report a bug or suggest a feature](https://github.com/Rayan-Refoua/OpenSync/issues)
- [Explore the source code](https://github.com/Rayan-Refoua/OpenSync)
- [Benchmark plan and volunteer opportunities](docs/BENCHMARKS.md)
- [Technical documentation and validation](docs/ENGINE-ACCEPTANCE.md)

For development, run `go test ./...` and `go vet ./...`. More detailed design notes, research, test procedures, and performance measurements live in [docs/](docs/) rather than on this project homepage.

## License

OpenSync is released under the **GNU General Public License v3.0 or later** ([GPL-3.0-or-later](LICENSE)). You are welcome to use, study, modify, and redistribute it under the terms of that license.

OpenSync is an independent project and is not affiliated with or endorsed by the makers of PluralEyes, Syncaila, or Align.
