"""Read-only Stage 2 audit. Does not fit models or assign timeline positions."""
import argparse
from collections import Counter, defaultdict
import hashlib
import json
import math
from pathlib import Path


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def audit(run):
    run=Path(run)
    def load(name):return json.loads((run/name).read_text(encoding="utf-8"))
    records=load("inventory.json")["records"]
    initial=load("evidence-v2-edges.json")["edges"]
    supported=load("supported-edges.json")["edges"]
    refined=load("refined-supported-edges.json")
    first_holdout=load("initial-holdout-validation.json")
    validated=load("validated-edges.json")["edges"]
    holdout=load("holdout-validation.json")
    recovery=load("recovery-discovery.json")
    issues=[];seen=set();residuals=[];adj=defaultdict(set)
    candidates=sum(len(json.loads(p.read_text(encoding="utf-8"))["candidates"]) for folder in ("evidence-v2","evidence-recovery-v2","evidence-refined-v1") for p in (run/folder).glob("*.json"))
    for rec in records:
        st=Path(rec["source"]).stat()
        if (st.st_size,st.st_mtime_ns)!=(rec["source_bytes"],rec["source_mtime_ns"]):issues.append("source changed: "+rec["name"])
    for edge in validated:
        r,q=edge["reference"],edge["query"]
        identity=(r,q)
        if identity in seen:issues.append("duplicate edge")
        seen.add(identity);adj[r].add(q);adj[q].add(r)
        model=edge["selected"];v=edge["validation"]
        if not math.isfinite(model["offset"]) or abs(model["slope"])>.001:issues.append("invalid model")
        if edge.get("ambiguous"):issues.append("ambiguous edge accepted")
        if v["status"]!="pass_disjoint_windows" or v["passed_windows"]<2 or v["contradictory_windows"]:issues.append("unvalidated edge admitted")
        if v["overlap_seconds"]>=30 and v["supported_span_fraction"]<.8:issues.append("insufficient temporal coverage admitted")
        for o in v["observations"]:
            for training in model["observations"]:
                if abs(o["query_time"]-training["query_time"])<(o["window"]+training["window"])/2-1e-8:issues.append("holdout overlaps fit window")
            expected=model["offset"]+model["slope"]*o["query_time"]
            if abs(expected-o["expected_offset"])>1e-8:issues.append("holdout model mismatch")
            if abs(o["offset"]-expected-o["residual_seconds"])>1e-8:issues.append("residual arithmetic mismatch")
            if o["passes"]:
                if not o["acoustically_supported"] or abs(o["residual_seconds"])>.025:issues.append("invalid accepted window")
                if not o.get("local_peak_distinct"):issues.append("ambiguous local timing accepted")
                residuals.append(abs(o["residual_seconds"])*1000)
    groups=[];visited=set()
    for i in range(len(records)):
        if i in visited:continue
        todo=[i];visited.add(i)
        for node in todo:
            for other in sorted(adj[node]):
                if other not in visited:visited.add(other);todo.append(other)
        groups.append(sorted(todo))
    groups.sort(key=lambda g:(-len(g),g[0]))
    connected=set(groups[0]) if len(groups[0])>1 else set()
    initial_nodes={i for e in initial for i in (e["reference"],e["query"])}
    def public(i):
        rec=records[i]
        return {"recording":i,"id":rec["id"],"name":rec["name"],"source":rec["source"],"duration_seconds":rec["duration"],"type":"camera" if rec["video"] else "audio"}
    rejected=[dict(reference=records[p["reference"]]["name"],query=records[p["query"]]["name"],status=p["status"],passed_windows=p["passed_windows"],contradictory_windows=p["contradictory_windows"],max_supported_residual_seconds=p["max_supported_residual_seconds"]) for p in holdout["pairs"] if p["status"]!="pass_disjoint_windows"]
    sorted_residuals=sorted(residuals)
    artifacts=("inventory.json","discovery.json","evidence-v2-edges.json","recovery-discovery.json","evidence-recovery-v2-edges.json","supported-edges.json","initial-holdout-validation.json","evidence-refined-v1-edges.json","refined-supported-edges.json","holdout-validation.json","validated-edges.json")
    return {
        "stage":"2_acoustic_verification_and_recovery","audit_status":"PASS_EVIDENCE_INTEGRITY" if not issues else "FAIL","issues":sorted(set(issues)),
        "recordings":len(records),"initial_supported_pairs":len(initial),"initial_recordings_with_support":len(initial_nodes),
        "recovery_target_recordings":len(recovery["target_recordings"]),"recovery_candidate_pairs":len(recovery["pairs"]),"assessed_candidates_including_recovery":candidates,
        "hypothesis_count_scope":"all saved verification, recovery and refinement evidence, including superseded refinement attempts",
        "supported_pairs_before_holdout":len(supported),"initial_holdout_accepted_pairs":first_holdout["accepted_edges"],"long_window_refinement_attempted_pairs":refined["retry_pairs"],"supported_models_after_refinement":len(refined["edges"]),"validated_pairs":len(validated),"final_holdout_rejected_pairs":rejected,
        "accepted_holdout_windows":len(residuals),"accepted_holdout_residual_ms":{"maximum":max(residuals,default=None),"median":sorted_residuals[len(residuals)//2] if residuals else None,"p95":sorted_residuals[min(len(residuals)-1,int(.95*len(residuals)))] if residuals else None},
        "connected_components":[[public(i) for i in group] for group in groups],
        "largest_component":{"recordings":len(connected),"camera":sum(records[i]["video"] is not None for i in connected),"audio":sum(records[i]["video"] is None for i in connected)},
        "recovered_into_largest_component":[public(i) for i in sorted(connected-initial_nodes)],
        "outside_largest_component":[public(i) for i in range(len(records)) if i not in connected],
        "pair_clock_drift_ppm_range":[min((e["selected"]["slope"]*1e6 for e in validated),default=None),max((e["selected"]["slope"]*1e6 for e in validated),default=None)],
        "artifacts":{name:digest(run/name) for name in artifacts},
        "baseline_used_for_alignment":False,"timeline_constructed":(run/"timeline-manifest.json").exists(),
        "limitations":["A connected component is not yet a cycle-audited global timeline.","Residuals describe supported sampled windows, not a bound across every source frame.","Holdouts are disjoint from local fitting; full-recording candidate discovery used the same media.","No calibrated false-match rate or independent human ground truth is available.","PluralEyes accuracy comparison and Premiere import validation remain pending.","Standalone runtime packaging and end-to-end production acceptance remain pending."]}


if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run");parser.add_argument("output")
    args=parser.parse_args();result=audit(args.run)
    with open(args.output,"x",encoding="utf-8") as f:json.dump(result,f,ensure_ascii=False,indent=2);f.write("\n")
    print(json.dumps({k:v for k,v in result.items() if k not in ("connected_components","artifacts","final_holdout_rejected_pairs","outside_largest_component")},ensure_ascii=True,indent=2))
