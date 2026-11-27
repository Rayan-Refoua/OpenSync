"""Build OpenSync with Windows WebView2 DevTools and optionally launch it."""
import argparse
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=ROOT / ".dev-build")
    parser.add_argument("--run", action="store_true", help="launch the built app")
    parser.add_argument("--project", type=Path, help="saved .opensync project to open")
    parser.add_argument("--ffmpeg-sdk", type=Path, help="build and launch the experimental shared FFmpeg decoder")
    args = parser.parse_args()
    if os.name != "nt":
        parser.error("this script builds the Windows desktop app")
    if args.project and (not args.run or not args.project.is_file()):
        parser.error("--project requires --run and an existing project file")
    go = shutil.which("go")
    if not go:
        parser.error("Go must be on PATH")
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=True)
    exe = out / "opensync-dev.exe"
    env = dict(os.environ, CGO_ENABLED="0")
    tags = "desktop,production,debug,devtools"
    if args.ffmpeg_sdk:
        sdk = args.ffmpeg_sdk.resolve()
        runtime = sdk / "bin"
        for required in (sdk / "include/libavfilter/avfilter.h", sdk / "lib", runtime / "ffmpeg.exe", runtime / "ffprobe.exe"):
            if not required.exists():
                parser.error(f"Missing SDK component: {required}")
        tags += ",ffmpeglib"
        env.update(
            CGO_ENABLED="1",
            CGO_CFLAGS=f'"-I{sdk / "include"}"',
            CGO_LDFLAGS=f'"-L{sdk / "lib"}"',
            OPENSYNC_EXPERIMENTAL_FFMPEG_LIB="1",
        )
        # Windows resolves linked DLLs beside the executable. Keep the matching
        # executable tools in OpenSync's normal fallback location.
        fallback = out / "runtime" / "bin"
        fallback.mkdir(parents=True, exist_ok=True)
        for binary in runtime.glob("*.dll"):
            shutil.copy2(binary, out)
            shutil.copy2(binary, fallback)
        for name in ("ffmpeg.exe", "ffprobe.exe"):
            shutil.copy2(runtime / name, fallback)

    # Publish the executable only after a successful build. Go reuses its build
    # cache and embeds the current frontend, with no packaging or copy step.
    with tempfile.TemporaryDirectory(dir=out, prefix="build-") as staging:
        candidate = Path(staging) / exe.name
        subprocess.run(
            [go, "build", "-trimpath", "-tags", tags,
             "-ldflags", "-H windowsgui -X main.openDevTools=true", "-o", str(candidate), "."],
            cwd=ROOT / "desktop", env=env, check=True,
        )
        try:
            candidate.replace(exe)
        except PermissionError as exc:
            raise SystemExit(f"Cannot replace {exe}. Close the running dev build and retry.") from exc
    print(exe, flush=True)
    if args.run:
        command = [str(exe)]
        if args.project:
            command.append(str(args.project.resolve()))
        subprocess.Popen(command, cwd=out, env=env)


if __name__ == "__main__":
    try:
        main()
    except subprocess.CalledProcessError as exc:
        sys.exit(exc.returncode)
