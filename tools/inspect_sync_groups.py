"""Summarize saved synchronization components and missing prior bridges."""
import argparse
from collections import Counter
import json
from pathlib import Path


def load_project(path):
    return json.loads(path.read_text(encoding="utf-8-sig"))["payload"]


def summarize(project, earlier=None, coverage=False):
    records = project["recordings"]
    graph = project["graph"]
    names = {record["name"].casefold() for record in records}
    groups = [
        {"number": number, "recordings": [records[i]["name"] for i in members]}
        for number, members in enumerate(graph["components"], 1)
    ]
    result = {
        "recordings": len(records),
        "placement_statuses": dict(Counter(model["status"] for model in project["timing_models"])),
        "components": groups,
    }
    if coverage:
        result["long_reference_coverage"] = long_reference_coverage(project)
    if earlier:
        def status_by_name(payload):
            items = payload["recordings"]
            return {
                items[m["recording"]]["name"].casefold(): m["status"]
                for m in payload["timing_models"]
            }

        before, after = status_by_name(earlier), status_by_name(project)
        result["changed_placements"] = [
            {"name": record["name"], "previous": before[key], "current": after[key]}
            for record in records
            if (key := record["name"].casefold()) in before
            and key in after and before[key] != after[key]
        ]
        result["comparison_association"] = "case-insensitive filename, for diagnosis only"
        if coverage:
            result["earlier_long_reference_coverage"] = long_reference_coverage(earlier)
        missing = {}
        for i in earlier["graph"]["active_edges"]:
            edge = earlier["validated_relations"][i]
            a, b = (earlier["recordings"][edge[key]]["name"] for key in ("reference", "query"))
            if (a.casefold() in names) != (b.casefold() in names):
                absent, present = (a, b) if b.casefold() in names else (b, a)
                missing.setdefault(absent, set()).add(present)
        result["missing_prior_references"] = {
            name: sorted(matches) for name, matches in sorted(missing.items())
        }
    return result


def long_reference_coverage(project):
    records = project["recordings"]
    discovery = project["graph"].get("candidate_discovery") or {}
    searches = {}
    seconds = 0.0
    for pair in discovery.get("pairs") or []:
        reference, query = (records[pair[key]] for key in ("reference", "query"))
        if reference["duration"] <= 3600:
            continue
        entry = searches.setdefault(query["id"], {"query": query["name"], "references": []})
        entry["references"].append(reference["name"])
        seconds += reference["duration"]
    return {
        "queries_searched": len(searches),
        "reference_seconds_searched": round(seconds, 3),
        "searches": list(searches.values()),
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("project", type=Path)
    parser.add_argument("--earlier", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--coverage", action="store_true", help="include blind searches of references longer than one hour")
    args = parser.parse_args()
    data = summarize(load_project(args.project), load_project(args.earlier) if args.earlier else None, args.coverage)
    rendered = json.dumps(data, indent=2)
    if args.output:
        args.output.write_text(rendered + "\n", encoding="utf-8")
    else:
        print(rendered)


if __name__ == "__main__":
    main()
