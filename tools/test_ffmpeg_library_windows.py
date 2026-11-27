"""Run OpenSync's PCM parity tests with a Windows shared FFmpeg SDK."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("sdk", type=Path, help="extracted SDK containing include, lib, and bin")
    parser.add_argument("--benchmark", action="store_true", help="time both decoders on the same AAC fixture")
    parser.add_argument("--video-folder", type=Path, help="compare both backends on a folder of real video recordings")
    args = parser.parse_args()
    sdk = args.sdk.resolve()
    required = [
        sdk / "include/libavfilter/avfilter.h",
        sdk / "lib",
        sdk / "bin/ffmpeg.exe",
    ]
    for path in required:
        if not path.exists():
            parser.error(f"Missing SDK component: {path}")
    env = os.environ.copy()
    env.update(
        CGO_ENABLED="1",
        CGO_CFLAGS=f'"-I{sdk / "include"}"',
        CGO_LDFLAGS=f'"-L{sdk / "lib"}"',
        PATH=str(sdk / "bin") + os.pathsep + env["PATH"],
        OPENSYNC_INTEGRATION="1",
        OPENSYNC_EXPERIMENTAL_FFMPEG_LIB="1",
    )
    if args.video_folder:
        folder = args.video_folder.resolve()
        if not folder.is_dir():
            parser.error(f"Video folder does not exist: {folder}")
        compare_project(folder, env)

    command = ["go", "test", "-tags", "ffmpeglib", "./internal/autosync", "./internal/desktop"]
    if args.benchmark:
        command.extend(["-run", "^$", "-bench", "^BenchmarkDecoderFirstBlock$", "-benchtime=10x", "-count=3"])
    else:
        command.append("-count=1")
    return subprocess.call(
        command,
        cwd=ROOT,
        env=env,
    )


def compare_project(folder, env):
    files = sorted(p for p in folder.iterdir() if p.is_file() and p.suffix.lower() == ".mp4")
    if len(files) < 2:
        raise ValueError("Expected at least two MP4 videos for the project comparison")
    with tempfile.TemporaryDirectory(prefix="opensync-library-parity-") as temp:
        root = Path(temp)
        executable = root / "opensync-cli.exe"
        subprocess.run(
            ["go", "build", "-tags", "ffmpeglib", "-o", str(executable), "./cmd/opensync"],
            cwd=ROOT, env=env, check=True,
        )
        results = []
        for backend, enabled in (("executable", "0"), ("library", "1")):
            output = root / backend
            run_env = dict(env, OPENSYNC_EXPERIMENTAL_FFMPEG_LIB=enabled)
            start = time.perf_counter()
            subprocess.run(
                [str(executable), "sync", "--files", *map(str, files),
                 "--mode", "chronological", "--name", "Decoder parity", "--output", str(output)],
                cwd=ROOT, env=run_env, check=True,
            )
            print(f"{backend}: {time.perf_counter() - start:.2f}s", flush=True)
            payload = json.loads((output / "OpenSync.opensync").read_text(encoding="utf-8"))["payload"]
            results.append((
                payload["timing_models"], payload.get("validated_relations"),
                (output / "OpenSync.xml").read_bytes(),
            ))
        if results[0] != results[1]:
            raise RuntimeError("Decoder backends produced different timing models, evidence, or Premiere XML")
        print("Timing models, validated evidence, and Premiere XML match.", flush=True)


if __name__ == "__main__":
    raise SystemExit(main())
