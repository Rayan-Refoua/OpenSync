"""Build and profile a repeatable no-cache OpenSync dataset run."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def compare(baseline: Path, candidate: Path) -> dict:
    report = {}
    for name in ("OpenSync.opensync", "OpenSync.xml"):
        old = baseline / name
        new = candidate / name
        if name.endswith(".opensync"):
            a = json.loads(old.read_text(encoding="utf-8"))
            b = json.loads(new.read_text(encoding="utf-8"))
            report["project_payload_identical"] = a["content_sha256"] == b["content_sha256"]
            for key in ("timing_models", "validated_relations", "graph"):
                report[key + "_identical"] = a["payload"].get(key) == b["payload"].get(key)
        else:
            report["premiere_xml_identical"] = (
                hashlib.sha256(old.read_bytes()).digest()
                == hashlib.sha256(new.read_bytes()).digest()
            )
    for name, folder in (("baseline", baseline), ("candidate", candidate)):
        report[name] = json.loads((folder / "native-run-profile.json").read_text(encoding="utf-8"))
    return report


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("media", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--mode", choices=["clips", "chronological"], default="clips")
    parser.add_argument("--profile-stage", default="Evaluating timing models")
    parser.add_argument("--only", action="append", default=[], help="include only these named files from the media folder")
    parser.add_argument("--anchor", action="append", type=Path, default=[], help="add a reference recording")
    parser.add_argument("--baseline", type=Path, help="previous benchmark directory")
    parser.add_argument("--compare-only", action="store_true", help="compare existing output")
    args = parser.parse_args()
    out = args.output.resolve()
    if args.compare_only and not args.baseline:
        parser.error("--compare-only requires --baseline")
    if not args.compare_only:
        if not args.media.is_dir():
            parser.error("media must point to an existing folder")
        if any(not anchor.is_file() for anchor in args.anchor):
            parser.error("every reference recording must exist")
        out.mkdir(parents=True, exist_ok=False)
        exe = out / "opensync.exe"
        subprocess.run(["go", "build", "-o", str(exe), "./cmd/opensync"], cwd=ROOT, check=True)
        env = dict(os.environ, OPENSYNC_PROFILE_STAGE=args.profile_stage,
                   OPENSYNC_PROFILE_PATH=str(out / "stage.pprof"))
        inputs = ["--folders", str(args.media.resolve())]
        if args.only:
            selected = [args.media.resolve() / name for name in args.only]
            if any(not path.is_file() for path in selected):
                parser.error("all selected media files must exist")
            inputs = ["--files", *map(str, selected)]
        if args.anchor:
            inputs = [*inputs, *([] if args.only else ["--files"]), *[str(path.resolve()) for path in args.anchor]]
        with (out / "sync.log").open("wb") as log:
            subprocess.run(
                [str(exe), "sync", *inputs, "--output", str(out / "project"), "--mode", args.mode],
                cwd=ROOT, env=env, stdout=log, stderr=subprocess.STDOUT, check=True,
            )
    if args.baseline:
        report = compare(args.baseline.resolve() / "project", out / "project")
        data = json.dumps(report, indent=2)
        (out / "comparison.json").write_text(data + "\n", encoding="utf-8")
        print(data)
        if not all(value for key, value in report.items() if key.endswith("_identical")):
            raise SystemExit("Benchmark outputs differ; see comparison.json")


if __name__ == "__main__":
    main()
