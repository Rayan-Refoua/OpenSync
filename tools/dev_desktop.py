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
    # Publish the executable only after a successful build. Go reuses its build
    # cache and embeds the current frontend, with no packaging or copy step.
    with tempfile.TemporaryDirectory(dir=out, prefix="build-") as staging:
        candidate = Path(staging) / exe.name
        subprocess.run(
            [go, "build", "-trimpath", "-tags", "desktop,production,debug,devtools",
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
        subprocess.Popen(command, cwd=out)


if __name__ == "__main__":
    try:
        main()
    except subprocess.CalledProcessError as exc:
        sys.exit(exc.returncode)
