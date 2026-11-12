#!/usr/bin/env python3
"""Prepare local stereo listening excerpts from a completed benchmark.

Python standard library only. Original caches are read-only. Every excerpt is
an unverified candidate, never a truth label or an export approval.
"""

import argparse
import array
import hashlib
import html
import json
import math
import os
from pathlib import Path
import random
import sys
import wave


def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def read_samples(path, start, seconds):
    with wave.open(str(path), "rb") as f:
        if (f.getnchannels(), f.getsampwidth(), f.getframerate(), f.getcomptype()) != (1, 2, 16000, "NONE"):
            raise ValueError(f"Unsupported PCM cache: {path}")
        count = round(seconds * 16000)
        first = round(start * 16000)
        if first < 0 or first + count > f.getnframes():
            raise ValueError("Listening excerpt outside source bounds")
        f.setpos(first)
        samples = array.array("h", f.readframes(count))
        if sys.byteorder != "little":
            samples.byteswap()
        if len(samples) != count:
            raise ValueError("Truncated cache")
        return samples


def duration(path):
    with wave.open(str(path), "rb") as f:
        return f.getnframes() / f.getframerate()


def stereo(path, left, right):
    if len(left) != len(right):
        raise ValueError("Unequal excerpt lengths")
    interleaved = array.array("h", [0]) * (len(left) * 2)
    interleaved[0::2], interleaved[1::2] = left, right
    if sys.byteorder != "little":
        interleaved.byteswap()
    with wave.open(str(path), "wb") as f:
        f.setnchannels(2)
        f.setsampwidth(2)
        f.setframerate(16000)
        f.writeframes(interleaved.tobytes())


def prepare(manifest_path, report_path, output):
    manifest_path, report_path, output = map(Path, (manifest_path, report_path, output))
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    report = json.loads(report_path.read_text(encoding="utf-8"))
    if manifest.get("version") != 1 or report.get("version") != 1 or report.get("status") != "completed":
        raise ValueError("Need a version 1 manifest and completed version 1 benchmark")
    identities = {os.path.normcase(str(Path(item["path"]).resolve())): item["sha256"] for item in report["inputs"]}
    # Freeze the exact reviewed inputs. A replaced cache or changed pair
    # manifest must be benchmarked again before a review packet is made.
    checked = set()
    paths = [manifest_path]
    for pair in manifest["pairs"]:
        for key in ("reference", "query"):
            path = Path(pair[key])
            if not path.is_absolute():
                path = manifest_path.parent / path
            pair[key] = str(path.resolve())
            paths.append(path)
    for path in paths:
        key = os.path.normcase(str(path.resolve()))
        if key in checked:
            continue
        if key not in identities or digest(path) != identities[key]:
            raise ValueError(f"Input is missing from benchmark or changed: {path}")
        checked.add(key)
    output.mkdir(exist_ok=False)
    media = output / "audio"
    media.mkdir()
    sections = []
    entries = []
    for pair_index, pair in enumerate(manifest["pairs"]):
        ref, query = Path(pair["reference"]), Path(pair["query"])
        candidates = []
        # Include the three best distinct hypotheses from every available
        # mode, rather than presenting only the primary mode's favourite.
        for run in report["runs"]:
            if run["dataset"] != "real" or run["scenario"] != pair["name"]:
                continue
            for peak in run["peaks"][:3]:
                offset = peak["offset_seconds"]
                if math.isfinite(offset) and all(abs(offset - p) > .25 for p in candidates):
                    candidates.append(offset)
        random.Random(7021 + pair_index).shuffle(candidates)
        cards = []
        for candidate_index, offset in enumerate(candidates):
            begin, end = max(0, -offset), min(duration(query), duration(ref) - offset)
            length = min(8.0, end - begin)
            if length < 2:
                continue
            positions = sorted(set(round(begin + (end - begin - length) * part, 6) for part in (.1, .5, .9)))
            clips = []
            for position_index, t in enumerate(positions):
                filename = f"pair-{pair_index+1:02d}-candidate-{candidate_index+1:02d}-region-{position_index+1}.wav"
                stereo(media / filename, read_samples(ref, t + offset, length), read_samples(query, t, length))
                clips.append({"file": "audio/" + filename, "query_start_seconds": t, "reference_start_seconds": t + offset, "duration_seconds": length})
            entry = {"pair": pair["name"], "candidate": candidate_index+1, "candidate_offset_seconds": offset, "clips": clips, "review": "unreviewed", "reviewer": "", "notes": "", "measured_events": []}
            entries.append(entry)
            players = "".join(f'<p>Query {c["query_start_seconds"]:.3f}s / reference {c["reference_start_seconds"]:.3f}s<br><audio controls preload="none" src="{c["file"]}"></audio></p>' for c in clips)
            cards.append(f'<article><h3>Candidate {candidate_index+1}</h3>{players}<details><summary>Candidate offset (unverified)</summary>{offset:+.6f} seconds</details></article>')
        sections.append(f'<section><h2>{html.escape(pair["name"])}</h2><p>Reference: <code>{html.escape(str(ref))}</code><br>Query: <code>{html.escape(str(query))}</code></p><div class="cards">{"".join(cards) or "No usable candidates; investigate manually."}</div></section>')
    packet = {"version": 1, "status": "unreviewed_listening_aid_not_ground_truth", "manifest_sha256": digest(manifest_path), "benchmark_sha256": digest(report_path), "entries": entries}
    (output / "review-notes.json").write_text(json.dumps(packet, indent=2) + "\n", encoding="utf-8")
    page = '''<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>OpenSync real recording review</title>
<style>body{background:#10151e;color:#e9edf3;font:16px/1.6 system-ui;margin:0}main{max-width:1200px;margin:auto;padding:32px}h1{font-size:32px}section{margin:36px 0;border-top:1px solid #41516c}code{overflow-wrap:anywhere;font-size:12px}.cards{display:flex;flex-wrap:wrap;gap:16px}article{padding:20px;background:#1b2636;border-radius:8px;max-width:340px}audio{max-width:100%}a{color:#a8caff}summary{cursor:pointer}</style><main><h1>Real recording review</h1>
<p>These are candidate alignments, not verified synchronization. Listen with headphones: <strong>reference on the left, query on the right</strong>. Channel levels are unchanged. Candidate order is shuffled and scores are hidden to reduce ranking bias.</p>
<ol><li>Listen for the same distinctive event in both channels at several separated regions.</li><li>Record whether content matches, differs, repeats, or remains uncertain. Listening alone does not establish millisecond accuracy.</li><li>For promising pairs, measure at least two widely separated event times in the original recordings, note the measurement uncertainty, and check for drift. Do not copy a candidate offset as measured truth.</li><li>Keep <a href="review-notes.json">review-notes.json</a> with reviewer, event times, uncertainty and observations. Only an independently measured result should later enter the labelled benchmark manifest.</li></ol>
<p>Candidate coverage is limited. “None of these” does not establish that recordings are unrelated. Short-overlap excerpts may overlap each other and are not independent evidence. These notes never authorize export changes.</p>'''
    (output / "index.html").write_text(page + "".join(sections) + "</main></html>", encoding="utf-8")
    return len(entries)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--report", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    count = prepare(args.manifest, args.report, args.output)
    print(f"Prepared {count} unverified candidate listening sets: {args.output}")


if __name__ == "__main__":
    main()
