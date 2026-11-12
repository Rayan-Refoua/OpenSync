"""Read-only audit of an autonomous discovery stage; never changes alignments."""
import argparse
from collections import Counter
import hashlib
import json
import math
from pathlib import Path
import xml.etree.ElementTree as ET

import numpy as np


def digest(path):
    h=hashlib.sha256()
    with open(path,"rb") as f:
        for block in iter(lambda:f.read(2**20),b""):h.update(block)
    return h.hexdigest()


def audit(run,baseline):
    run=Path(run);baseline=Path(baseline)
    inventory=json.loads((run/"inventory.json").read_text(encoding="utf-8"))
    discovery=json.loads((run/"discovery.json").read_text(encoding="utf-8"))
    records=inventory["records"];issues=[];feature_counts=[];cache_bytes=0
    ids=[r["id"] for r in records]
    if len(ids)!=len(set(ids)):issues.append("duplicate recording IDs")
    source_paths=[str(Path(r["source"]).resolve()).casefold() for r in records]
    if len(source_paths)!=len(set(source_paths)):issues.append("duplicate source identities")
    for rec in records:
        source=Path(rec["source"]);cache=Path(rec["cache"])
        if not source.is_file():issues.append("missing source: "+rec["name"])
        else:
            st=source.stat()
            if (st.st_size,st.st_mtime_ns)!=(rec["source_bytes"],rec["source_mtime_ns"]):issues.append("source identity changed: "+rec["name"])
        if not cache.is_file():issues.append("missing cache: "+rec["name"]);continue
        cache_bytes+=cache.stat().st_size
        if digest(cache)!=rec["cache_sha256"]:issues.append("cache SHA256 mismatch: "+rec["name"])
        try:
            with np.load(run/"features"/(rec["id"]+".npz")) as feature:
                hashes=feature["hashes"]
                if str(feature["digest"])!=rec["cache_sha256"]:issues.append("feature/cache identity mismatch: "+rec["name"])
                if str(feature["version"])!=inventory["version"]:issues.append("feature algorithm version mismatch: "+rec["name"])
                if hashes.ndim!=2 or hashes.shape[1]!=2 or hashes.dtype!=np.int64:issues.append("invalid fingerprint schema: "+rec["name"])
                elif len(hashes) and (np.any(hashes<0) or np.max(hashes[:,1])>math.ceil(rec["duration"]*125)):issues.append("fingerprint outside source bounds: "+rec["name"])
                feature_counts.append(len(hashes))
        except (OSError,ValueError,KeyError) as e:issues.append("unreadable fingerprint: "+rec["name"]+": "+str(e))
    covered=set();pair_ids=set();candidate_count=0;by_source=Counter()
    for pair in discovery["pairs"]:
        ref,q=pair["reference"],pair["query"]
        if not (0<=ref<len(records) and 0<=q<len(records)) or ref==q:issues.append("invalid pair identity");continue
        identity=tuple(sorted((ref,q)))
        if identity in pair_ids:issues.append("duplicate recording pair")
        pair_ids.add(identity);covered.update(identity)
        for i in identity:by_source[i]+=1
        candidates=pair["candidates"]
        if not 1<=len(candidates)<=8:issues.append("unexpected alternative count")
        for c in candidates:
            candidate_count+=1
            offset=c["offset"]
            if not math.isfinite(offset) or not -records[q]["duration"]-.256<=offset<=records[ref]["duration"]+.256:issues.append("candidate outside possible overlap")
            if not isinstance(c["votes"],int) or c["votes"]<6:issues.append("invalid discovery vote count")
    baseline_root=ET.parse(baseline).getroot();sequence=baseline_root.find(".//sequence")
    videos=sequence.findall("./media/video/track/clipitem");audios=sequence.findall("./media/audio/track/clipitem")
    camera_names={r["name"].casefold() for r in records if r["video"] is not None}
    baseline_names=[c.findtext("name","").casefold() for c in videos]
    missing=sorted(camera_names-set(baseline_names))
    extra=sorted(set(baseline_names)-camera_names)
    if len(baseline_names)!=len(set(baseline_names)):issues.append("baseline has duplicate camera names; full identity resolution needed before comparison")
    no_candidates=[{"id":r["id"],"name":r["name"],"source":r["source"],"duration_seconds":r["duration"]} for i,r in enumerate(records) if i not in covered]
    return {
        "stage":"1_inventory_and_discovery","status":"PASS" if not issues else "FAIL","issues":issues,
        "run_directory":str(run.resolve()),"recordings":len(records),"camera_recordings":len(camera_names),"audio_only_recordings":sum(r["video"] is None for r in records),
        "source_duration_hours":sum(r["duration"] for r in records)/3600,"reused_caches":sum(r["cache_reused"] for r in records),"cache_bytes_audited":cache_bytes,
        "fingerprints":sum(feature_counts),"feature_files_audited":len(feature_counts),"candidate_recording_pairs":len(pair_ids),"offset_hypotheses":candidate_count,
        "recordings_with_candidates":len(covered),"recordings_without_candidates":no_candidates,"excluded_derived_copies":inventory["excluded"],
        "baseline":{"path":str(baseline),"sha256":digest(baseline),"video_clipitems":len(videos),"audio_clipitems":len(audios),"video_tracks":len(sequence.findall("./media/video/track")),"audio_tracks":len(sequence.findall("./media/audio/track")),"camera_names_missing_from_baseline":missing,"baseline_camera_names_absent_from_dataset":extra,"identity_check":"unique camera basename coverage only; path-level export audit remains a later-stage requirement"},
        "artifacts":{"inventory_sha256":digest(run/"inventory.json"),"discovery_sha256":digest(run/"discovery.json"),"run_config_sha256":digest(run/"run-config.json")},
        "stage_boundaries":{"acoustic_verification_started":any((run/"evidence").glob("*.json")),"timeline_export_exists":(run/"timeline-manifest.json").exists(),"baseline_positions_used_for_discovery":False,"accuracy_vs_pluraleyes":"not_assessed_at_discovery_stage"},
        "limitations":["Candidates and fingerprint votes are not accepted alignments or confidence probabilities.","Sources without candidates must remain unresolved or receive an automated recovery search.","Independent acoustic verification, drift assessment, graph consistency, XML export and accuracy comparison remain later stages."]}


if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run");parser.add_argument("baseline");parser.add_argument("output")
    args=parser.parse_args()
    result=audit(args.run,args.baseline)
    with open(args.output,"x",encoding="utf-8") as f:json.dump(result,f,ensure_ascii=False,indent=2);f.write("\n")
    print(json.dumps({k:result[k] for k in ("status","recordings","feature_files_audited","fingerprints","candidate_recording_pairs","offset_hypotheses","recordings_with_candidates","recordings_without_candidates","issues")},ensure_ascii=True,indent=2))
