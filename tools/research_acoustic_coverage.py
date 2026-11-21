"""Independently rank audio offsets from source media with SciPy (development only)."""

import argparse
import json
from pathlib import Path
import subprocess

import numpy as np
from scipy import fft, ndimage, signal


def features(path, start=None, duration=None):
    seek = ["-ss", str(start)] if start is not None else []
    limit = ["-t", str(duration)] if duration is not None else []
    pcm = subprocess.check_output([
        "ffmpeg", "-nostdin", "-v", "error", *seek, "-i", str(path), *limit, "-map", "0:a:0",
        "-ac", "1", "-ar", "8000", "-f", "f32le", "-",
    ])
    freq, _, spec = signal.stft(np.frombuffer(pcm, "<f4"), 8000, nperseg=512,
                                noverlap=352, boundary=None, padded=False)
    bands = [(100, 350), (350, 700), (700, 1200), (1200, 2200), (2200, 3500)]
    energy = np.stack([np.sum(abs(spec[(freq >= lo) & (freq < hi)]) ** 2, axis=0)
                       for lo, hi in bands])
    # Contrast local onsets against the preceding quarter second.
    log = np.log(np.maximum(energy, 1e-10))
    return (log[:, 12:] - log[:, :-12]).astype(np.float64)


class Search:
    def __init__(self, reference):
        self.frames = reference.shape[1]
        self.size = fft.next_fast_len(2 * self.frames)
        self.spectrum = fft.rfft(reference, self.size)
        self.sum = np.pad(np.cumsum(reference, axis=1), ((0, 0), (1, 0)))
        self.squares = np.pad(np.cumsum(reference ** 2, axis=1), ((0, 0), (1, 0)))

    def rank(self, query):
        frames = query.shape[1]
        if frames > self.frames or frames < 120:
            return []
        width = min(175, frames // 5)
        span = self.frames - frames + 1
        curves = []
        for start in np.linspace(0, frames - width, 5, dtype=int):
            section = query[:, start:start + width].copy()
            section -= section.mean(axis=1, keepdims=True)
            end = start + width
            sums = self.sum[:, end:end + span] - self.sum[:, start:start + span]
            sums2 = self.squares[:, end:end + span] - self.squares[:, start:start + span]
            centered = np.maximum(0, sums2 - sums ** 2 / width)
            norm = np.sqrt(centered * np.sum(section ** 2, axis=1, keepdims=True))
            numerator = fft.irfft(self.spectrum * fft.rfft(section, self.size).conj(), self.size)
            numerator = numerator[:, start:start + span]
            scores = np.divide(numerator, norm, out=np.zeros_like(numerator), where=norm > 1e-6)
            curves.append(np.median(scores, axis=0))
        evidence = np.stack(curves)
        ranking = np.sort(evidence, axis=0)[-3:].mean(axis=0)
        peaks = []
        for _ in range(4):
            at = int(np.argmax(ranking))
            if not np.isfinite(ranking[at]):
                break
            peaks.append({"offset": round(at / 50, 3), "score": round(float(ranking[at]), 5),
                          "sections": np.round(evidence[:, at], 4).tolist()})
            ranking[max(0, at - 90):at + 91] = -np.inf
        return peaks


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("project", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--reference", action="append", required=True)
    parser.add_argument("--query", action="append")
    parser.add_argument("--query-window", action="append", nargs=3, metavar=("NAME", "START", "DURATION"))
    args = parser.parse_args()
    project = json.loads(args.project.read_text(encoding="utf-8-sig"))["payload"]
    records = project["recordings"]
    by_name = {r["name"].casefold(): r for r in records}
    references = [by_name[name.casefold()] for name in args.reference]
    queries = [(by_name[name.casefold()], None, None) for name in args.query or []]
    for name, start, duration in args.query_window or []:
        queries.append((by_name[name.casefold()], float(start), float(duration)))
    if not queries:
        queries = [(records[m["recording"]], None, None) for m in project["timing_models"]
                   if m["status"].startswith("unresolved") and records[m["recording"]]["duration"] <= 300]
    decoded = [(r, start, features(r["source"], start, duration)) for r, start, duration in queries]
    output = []
    for reference in references:
        search = Search(features(reference["source"]))
        for query, start, query_features in decoded:
            if reference["id"] == query["id"]:
                continue
            output.append({"reference": reference["name"], "query": query["name"],
                           "query_window_start": start, "peaks": search.rank(query_features)})
        print(f"Searched {reference['name']} against {len(queries)} queries", flush=True)
        args.output.write_text(json.dumps(output, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
