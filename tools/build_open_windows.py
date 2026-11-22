"""Close OpenSync, replace its local Windows release package, build, and launch it."""
import argparse
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import zipfile

ROOT = Path(__file__).resolve().parents[1]
RELEASE_DIR = ROOT / "release"
PACKAGE_NAME = "OpenSync-0.1.0-alpha.4-windows-x64"
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


def remove_local_release(path: Path, release_root: Path) -> None:
    resolved = path.resolve()
    if resolved.parent != release_root:
        raise RuntimeError(f"Refusing to remove a path outside the release directory: {path}")
    if path.is_dir() and not path.is_symlink():
        shutil.rmtree(path, onerror=remove_readonly)
    elif path.is_file():
        os.chmod(path, stat.S_IWRITE)
        path.unlink()
    else:
        raise RuntimeError(f"Unexpected release output type: {path}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--project", type=Path, help="open this saved .opensync project after the build")
    args = parser.parse_args()
    if os.name != "nt":
        parser.error("this workflow builds and launches the Windows desktop app")
    if args.project and not args.project.is_file():
        parser.error("--project must name an existing .opensync project file")

    release_root = RELEASE_DIR.resolve()
    output = (release_root / PACKAGE_NAME).resolve()
    if output.parent != release_root or output == release_root:
        raise RuntimeError("Refusing to replace a package outside the repository release directory")
    archive = release_root / f"{PACKAGE_NAME}.zip"

    close_instances()
    for old_release in release_root.iterdir() if release_root.exists() else ():
        if old_release.name.startswith("OpenSync-") and (
            old_release.name.endswith("-windows-x64")
            or old_release.name.endswith("-windows-x64.zip")
        ):
            remove_local_release(old_release, release_root)

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
    archive_path = release_root / f"{PACKAGE_NAME}.zip"
    with zipfile.ZipFile(
        archive_path, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=3, allowZip64=True
    ) as package_zip:
        for package_file in sorted(output.rglob("*")):
            if package_file.is_file():
                package_zip.write(package_file, package_file.relative_to(release_root))
    command = [str(executable)]
    if args.project:
        command.append(str(args.project.resolve()))
    subprocess.Popen(command, cwd=output)
    print(f"Built package: {archive_path}", flush=True)
    print(f"Built and launched OpenSync: {executable}", flush=True)


if __name__ == "__main__":
    try:
        main()
    except (OSError, RuntimeError, subprocess.CalledProcessError) as exc:
        print(f"Build failed: {exc}", file=sys.stderr)
        sys.exit(1)
