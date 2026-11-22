"""Close OpenSync, replace its local Windows release package, build, and launch it."""
import argparse
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
RELEASE_DIR = ROOT / "release"
PACKAGE_NAME = "OpenSync-0.1.0-alpha.1-windows-x64"
PROCESS_IMAGES = (
    "opensync.exe",
    "opensync-cli.exe",
    "opensync-dev.exe",
    "opensync-audio-refinement.exe",
    "opensync-project-loader.exe",
)


def close_instances() -> None:
    for image in PROCESS_IMAGES:
        result = subprocess.run(
            ["taskkill", "/F", "/T", "/IM", image],
            capture_output=True,
            text=True,
            check=False,
        )
        if result.returncode and "not found" not in (result.stdout + result.stderr).lower():
            raise RuntimeError(f"Could not close {image}: {(result.stderr or result.stdout).strip()}")


def remove_readonly(function, path, _error) -> None:
    os.chmod(path, stat.S_IWRITE)
    function(path)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", type=Path, help="open this saved .opensync project after the build")
    args = parser.parse_args()
    if os.name != "nt":
        parser.error("this workflow builds and launches the Windows desktop app")
    if args.project and not args.project.is_file():
        parser.error("--project must name an existing .opensync project file")

    release_root = RELEASE_DIR.resolve()
    output = (RELEASE_DIR / PACKAGE_NAME).resolve()
    if output.parent != release_root or output == release_root:
        raise RuntimeError("Refusing to replace a package outside the repository release directory")
    archive = output.with_suffix(".zip")

    close_instances()
    if output.exists():
        shutil.rmtree(output, onerror=remove_readonly)
    if archive.exists():
        archive.unlink()

    powershell = shutil.which("pwsh") or shutil.which("powershell")
    if not powershell:
        raise RuntimeError("PowerShell is required for the Windows release build")
    subprocess.run(
        [
            powershell,
            "-NoProfile",
            "-ExecutionPolicy",
            "Bypass",
            "-File",
            str(ROOT / "tools" / "build_desktop_windows.ps1"),
            "-Output",
            str(output),
        ],
        cwd=ROOT,
        check=True,
    )

    executable = output / "opensync.exe"
    if not executable.is_file():
        raise RuntimeError(f"Build completed without the desktop executable: {executable}")
    command = [str(executable)]
    if args.project:
        command.append(str(args.project.resolve()))
    subprocess.Popen(command, cwd=output)
    print(f"Built and launched OpenSync: {executable}", flush=True)


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, subprocess.CalledProcessError) as exc:
        print(f"Build failed: {exc}", file=sys.stderr)
        sys.exit(1)
