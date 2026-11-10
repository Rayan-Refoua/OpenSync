"""OpenSync autonomous dataset synchronization, version 1.

No PluralEyes positions, per-clip overrides or human-selected offsets are inputs.
All source reads are sequential during caching; DSP uses local SSD caches.
"""
import argparse
from collections import Counter, defaultdict
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time
import wave
import xml.etree.ElementTree as ET

# The pipeline uses a bounded outer worker pool; avoid nested BLAS pools.
os.environ.setdefault("OPENBLAS_NUM_THREADS","1")
os.environ.setdefault("OMP_NUM_THREADS","1")
import numpy as np
from scipy import ndimage, signal, optimize
sys.path.insert(0,str(Path(__file__).resolve().parent))
from project_export import save_project, export_project, audit_xml
import refinement
import relation_graph

VERSION = "opensync-autonomous-v1"
SR = 16000
HOP = 128
MEDIA = {".mp4", ".mov", ".mxf", ".mts", ".m2ts", ".wav", ".mp3", ".flac", ".m4a", ".aac"}


def save(path, obj):
    path = Path(path)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(obj, indent=2, ensure_ascii=False, allow_nan=False) + "\n", encoding="utf-8")
    os.replace(tmp, path)


def sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(2**20), b""):
            h.update(block)
    return h.hexdigest()


def probe(path):
    return json.loads(subprocess.check_output(["ffprobe", "-v", "error", "-show_format", "-show_streams", "-of", "json", str(path)], timeout=120))


def wav_info(path):
    with wave.open(str(path), "rb") as w:
        if (w.getframerate(), w.getnchannels(), w.getsampwidth()) != (SR, 1, 2):
            raise ValueError("analysis cache must be 16kHz mono PCM16")
        return w.getnframes()/SR


def read(path, start, seconds):
    with wave.open(str(path), "rb") as w:
        first, n = round(start*SR), round(seconds*SR)
        if first < 0 or first+n > w.getnframes():
            raise ValueError("PCM interval outside recording")
        w.setpos(first)
        return np.frombuffer(w.readframes(n), "<i2").astype(np.float32)/32768


def input_files(args):
    """Resolve the public file-list interface without a project-specific script."""
    explicit=list(args.files or [])
    if args.file_list:
        manifest=Path(args.file_list).resolve()
        content=manifest.read_text(encoding="utf-8-sig")
        listed=json.loads(content) if manifest.suffix.lower()==".json" else [line.strip() for line in content.splitlines() if line.strip()]
        if not isinstance(listed,list) or not all(isinstance(p,str) for p in listed):raise ValueError("file list must contain path strings")
        explicit.extend(str((manifest.parent/p).resolve()) if not Path(p).is_absolute() else p for p in listed)
    if explicit:
        if args.input or args.source:raise ValueError("choose explicit files or an input root with source folders")
        files=sorted((Path(p).resolve() for p in explicit),key=lambda p:str(p).casefold())
        if len(set(files))!=len(files):raise ValueError("duplicate input file")
        for path in files:
            if not path.is_file() or path.suffix.lower() not in MEDIA:raise ValueError("unsupported or missing media file: "+str(path))
        return files
    if not args.input or not args.source:raise ValueError("supply --files, --file-list, or --input with --source")
    return None


def prepare(args, out):
    old = {}
    if args.cache_index:
        old = {os.path.normcase(str(Path(v["source"]).resolve())): v for v in json.loads(Path(args.cache_index).read_text(encoding="utf-8")).values()}
    files, excluded = [], []
    explicit=input_files(args)
    root = Path(args.input).resolve() if args.input else None
    if explicit:files.extend(explicit)
    for folder in args.source or []:
        selected = (root/folder).resolve()
        if not selected.is_relative_to(root) or not selected.is_dir():
            raise ValueError("source directories must exist inside input root")
        for path in sorted(selected.rglob("*")):
            if not path.is_file() or path.suffix.lower() not in MEDIA:
                continue
            if any("drift_corrected" in p.lower() for p in path.parts):
                excluded.append({"path": str(path), "reason": "existing derived drift-corrected copy; original retained"})
            else:
                files.append(path)
    if len(set(files)) != len(files) or not files:
        raise ValueError("empty or overlapping source selection")
    records = []
    for i, path in enumerate(sorted(files)):
        st = path.stat()
        rid = hashlib.sha256(str(path).encode()).hexdigest()[:20]
        state_path = out/"cache"/(rid+".json")
        identity = {"source": str(path), "source_bytes": st.st_size, "source_mtime_ns": st.st_mtime_ns}
        if state_path.exists():
            rec = json.loads(state_path.read_text(encoding="utf-8"))
            if any(rec[k] != v for k, v in identity.items()) or not Path(rec["cache"]).exists():
                raise ValueError("stale cache; create a new run: "+str(path))
            records.append(rec)
            continue
        prior = old.get(os.path.normcase(str(path)))
        cache = out/"cache"/(rid+".wav")
        local_source = path
        reused = False
        info=None
        if prior and prior.get("source_bytes") == st.st_size and prior.get("source_mtime_ns") == st.st_mtime_ns and Path(prior["cache"]).is_file():
            cache = Path(prior["cache"])
            reused = True
        else:
            # Copy once from the slow source disk. All decoding, later
            # analyses and optional audio derivatives use this SSD copy.
            local_source = out/"cache"/(rid+path.suffix.lower())
            if local_source == cache:
                local_source = out/"cache"/(rid+"-original"+path.suffix.lower())
            print(f"CACHE {i+1}/{len(files)} sequential source read: {path.name}", flush=True)
            temporary = local_source.with_suffix(local_source.suffix+".partial")
            shutil.copyfile(path, temporary)
            if path.stat().st_size != st.st_size or path.stat().st_mtime_ns != st.st_mtime_ns:
                raise ValueError("source changed during caching: "+str(path))
            os.replace(temporary, local_source)
            info=probe(local_source)
            if any(x["codec_type"]=="audio" for x in info["streams"]):
                subprocess.run(["ffmpeg", "-nostdin", "-v", "error", "-n", "-i", str(local_source), "-map", "0:a:0", "-ac", "1", "-ar", str(SR), "-c:a", "pcm_s16le", str(cache)], check=True)
            else:
                # Preserve silent video as unresolved instead of aborting the
                # whole project. This cache is explicitly synthetic silence.
                frames=round(float(info["format"]["duration"])*SR)
                with wave.open(str(cache),"wb") as w:
                    w.setparams((1,2,SR,0,"NONE","not compressed"))
                    block=bytes(SR*2)
                    while frames:
                        n=min(frames,SR);w.writeframesraw(block[:n*2]);frames-=n
        if info is None:info = probe(local_source)
        video = next((x for x in info["streams"] if x["codec_type"] == "video" and not x.get("disposition",{}).get("attached_pic")), None)
        audio = next((x for x in info["streams"] if x["codec_type"] == "audio"), None)
        if audio is None and video is None:raise ValueError("no usable audio or video stream: "+str(path))
        group=str(path.parent.relative_to(root)) if root else str(path.parent)
        rec = dict(identity, id=rid, name=path.name, cache=str(cache.resolve()), local_source=str(local_source), duration=wav_info(cache), video=video, audio=audio, cache_sha256=sha(cache), cache_reused=reused, source_group=group,analysis_audio="decoded_source_audio" if audio else "synthetic_silence_no_audio")
        save(state_path, rec)
        records.append(rec)
        print(f"CACHE {i+1}/{len(files)} ready {path.name} ({rec['duration']:.2f}s, reused={reused})", flush=True)
    save(out/"inventory.json", {"version":VERSION,"root":str(root),"records":records,"excluded":excluded,"source_hash_policy":"size and mtime guarded; analysis PCM SHA256; no full rescan of original HDD media"})
    return records


def fingerprint(samples, absolute_start=0):
    if len(samples) < 1024:
        return np.empty((0,2),dtype=np.int64)
    _, _, z = signal.stft(samples, fs=SR, nperseg=1024, noverlap=1024-HOP, boundary=None, padded=False)
    logmag = np.log1p(200*np.abs(z))
    logmag[:8] = 0
    logmag[385:] = 0
    maximum = ndimage.maximum_filter(logmag, size=(9,11))
    threshold = np.percentile(logmag[8:385],97)
    f,t = np.nonzero((logmag==maximum)&(logmag>threshold))
    ordering = np.argsort(-logmag[f,t],kind="stable")
    per_second = Counter()
    peaks = []
    for j in ordering:
        second = int(t[j])*HOP//SR
        if per_second[second] < 12:
            peaks.append((int(t[j]),int(f[j])))
            per_second[second]+=1
    peaks.sort()
    hashes=[]
    base=round(absolute_start*SR/HOP)
    for i,(t1,f1) in enumerate(peaks):
        added=0
        for t2,f2 in peaks[i+1:]:
            dt=t2-t1
            if dt>125:break
            if dt<10:continue
            key=((f1//3)<<16)|((f2//3)<<8)|(dt//3)
            hashes.append((key,base+t1))
            added+=1
            if added==5:break
    return np.asarray(hashes,dtype=np.int64).reshape(-1,2)


def fingerprints(rec, out):
    path=out/"features"/(rec["id"]+".npz")
    if path.exists():
        data=np.load(path)
        if str(data["digest"])!=rec["cache_sha256"] or str(data["version"])!=VERSION:raise ValueError("stale fingerprints")
        return data["hashes"]
    pieces=[]
    if rec.get("audio") is None:
        hashes=np.empty((0,2),dtype=np.int64)
        np.savez_compressed(path,hashes=hashes,digest=rec["cache_sha256"],version=VERSION)
        return hashes
    start=0.0
    while start < rec["duration"]:
        length=min(60,rec["duration"]-start)
        if length<1:break
        pieces.append(fingerprint(read(rec["cache"],start,length),start))
        if len(pieces)%20==0:
            print(f"INDEX progress {rec['name']}: {start+length:.0f}/{rec['duration']:.0f}s",flush=True)
        if start+length>=rec["duration"]:break
        start+=50
    hashes=np.unique(np.concatenate(pieces),axis=0) if pieces else np.empty((0,2),dtype=np.int64)
    temporary=path.with_suffix(".partial.npz")
    np.savez_compressed(temporary,hashes=hashes,digest=rec["cache_sha256"],version=VERSION)
    os.replace(temporary,path)
    return hashes


def discover(records, out):
    path=out/"discovery.json"
    if path.exists():return json.loads(path.read_text(encoding="utf-8"))["pairs"]
    all_hashes=[]
    postings=defaultdict(list)
    for i,rec in enumerate(records):
        hashes=fingerprints(rec,out)
        all_hashes.append(hashes)
        for key,t in hashes:
            postings[int(key)].append((i,int(t)))
        print(f"INDEX {i+1}/{len(records)} {rec['name']}: {len(hashes)} landmarks",flush=True)
    # Common content is retained in the source cache but is not allowed to
    # manufacture thousands of votes from a ubiquitous fingerprint.
    postings={k:v for k,v in postings.items() if len(v)<=150}
    pairs=[]
    for qid,(rec,hashes) in enumerate(zip(records,all_hashes)):
        votes=Counter()
        for key,t in hashes:
            for rid,rt in postings.get(int(key),()):
                if (records[rid]["duration"],rid)<=(rec["duration"],qid):continue
                bucket=round((rt-int(t))/16)
                votes[(rid,bucket)]+=1
        by_ref=defaultdict(list)
        for (rid,bucket),n in votes.items():
            by_ref[rid].append((sum(votes.get((rid,bucket+d),0) for d in (-1,0,1)),bucket))
        for rid,choices in sorted(by_ref.items()):
            picked=[]
            for count,bucket in sorted(choices,key=lambda x:(-x[0],x[1])):
                offset=bucket*16*HOP/SR
                if count<6:break
                if all(abs(offset-x["offset"])>=.75 for x in picked):
                    picked.append({"offset":offset,"votes":count})
                if len(picked)==8:break
            if picked:pairs.append({"reference":rid,"query":qid,"candidates":picked})
        print(f"DISCOVERY {qid+1}/{len(records)} {rec['name']}: {len(by_ref)} candidate references",flush=True)
    save(path,{"version":VERSION,"status":"discovery_only","pairs":pairs})
    return pairs


def normcorr(reference, query):
    n=len(query)
    if n<8 or len(reference)<n:return None
    q=query.astype(np.float64)
    r=reference.astype(np.float64)
    q-=q.mean()
    qvar=np.dot(q,q)
    if qvar<1e-10:return None
    sums=np.r_[0,np.cumsum(r)]
    squares=np.r_[0,np.cumsum(r*r)]
    variance=squares[n:]-squares[:-n]-(sums[n:]-sums[:-n])**2/n
    dots=signal.correlate(r,q,mode="valid",method="fft")
    corr=np.divide(dots,np.sqrt(np.maximum(variance,1e-20)*qvar))
    corr[variance<1e-10]=0
    best=int(np.argmax(np.abs(corr)))
    # At the 2 kHz measurement rate, peaks farther than 4 ms represent
    # competing local lags, not the same subframe peak.
    competing=max(float(np.max(np.abs(corr[:max(0,best-8)]),initial=0)),float(np.max(np.abs(corr[best+9:]),initial=0)))
    return best,float(np.clip(corr[best],-1,1)),min(1.0,competing)


def waveform(ref,query,qtime,offset,length,radius=.35):
    start=max(0,qtime+offset-radius)
    stop=min(ref["duration"],qtime+offset+length+radius)
    if stop-start<length or qtime<0 or qtime+length>query["duration"]:return None
    q=read(query["cache"],qtime,length)
    r=read(ref["cache"],start,stop-start)
    def prep(x):
        n=len(x)//8
        return np.diff(x[:n*8].reshape(n,8).mean(axis=1))
    matched=normcorr(prep(r),prep(q))
    if matched is None:return None
    lag,corr,competing=matched
    return {"query_time":qtime+length/2,"offset":start-qtime+lag*8/SR,"wave_correlation":abs(corr),"window":length,"competing_local_correlation":competing,"local_peak_distinct":abs(corr)-competing>=.025 and abs(corr)>=1.1*competing}


def flux_score(ref,query,qtime,offset,length):
    if qtime+offset<0 or qtime+offset+length>ref["duration"]:return 0.0
    arrays=[]
    for record,start in ((ref,qtime+offset),(query,qtime)):
        _,_,z=signal.stft(read(record["cache"],start,length),fs=SR,nperseg=512,noverlap=384,boundary=None,padded=False)
        mag=np.log1p(100*np.abs(z))
        flux=np.maximum(0,np.diff(mag,axis=1))
        arrays.append([flux[lo:hi].mean(axis=0) for lo,hi in ((7,29),(29,77),(77,193))])
    corrs=[]
    for a,b in zip(*arrays):
        if np.std(a)<1e-8 or np.std(b)<1e-8:corrs.append(0.0)
        else:corrs.append(float(np.corrcoef(a,b)[0,1]))
    return float(np.median(corrs))


def assess_candidate(ref,query,candidate):
    offset=candidate["offset"]
    begin=max(0,-offset)+.6
    end=min(query["duration"],ref["duration"]-offset)-.6
    overlap=end-begin
    if overlap<.8:return dict(candidate,status="insufficient_overlap",observations=[])
    length=min(candidate.get("verification_window_seconds",4.0),overlap/(5 if candidate.get("verification_window_seconds") else 3))
    count=min(candidate.get("verification_max_windows",7),int(overlap/(length+.5)))
    if count<2:return dict(candidate,status="insufficient_distinct_regions",observations=[])
    times=np.linspace(begin,end-length,count)
    observations=[]
    for qtime in times:
        qtime=float(qtime)
        radius=.35+abs(qtime+length/2-candidate["anchor_time"])*.001 if "anchor_time" in candidate else .35
        m=waveform(ref,query,qtime,offset,length,radius)
        if m is None:continue
        m["flux_correlation"]=flux_score(ref,query,qtime,m["offset"],length)
        controls=[x for shift in (-31,-15,15,31) if (x:=waveform(ref,query,qtime,offset+shift,length,radius)) is not None]
        strongest=max((x["wave_correlation"] for x in controls),default=1.0)
        m["controls"]=len(controls)
        m["strongest_control"]=strongest
        # Fixed conservative investigation thresholds, identical for all files.
        # Not claimed to be a calibrated error probability.
        m["supported"]=len(controls)>=2 and m["wave_correlation"]>=.10 and m["wave_correlation"]>=strongest*1.5 and m["wave_correlation"]-strongest>=.035 and (m["flux_correlation"]>=.25 or m["wave_correlation"]>=.65)
        observations.append(m)
    good=[x for x in observations if x["supported"]]
    required=3 if overlap>=12 else 2
    result=dict(candidate,status="weak_or_inconsistent",observations=observations)
    if len(good)<required:return result
    t=np.array([x["query_time"] for x in good]);v=np.array([x["offset"] for x in good])
    slopes=[(v[j]-v[i])/(t[j]-t[i]) for i in range(len(t)) for j in range(i+1,len(t)) if t[j]-t[i]>1]
    slope=float(np.median(slopes)) if slopes and np.ptp(t)>=30 else 0.0
    intercept=float(np.median(v-slope*t))
    residual=np.abs(v-intercept-slope*t)
    inliers=residual<=.025
    if np.count_nonzero(inliers)<required or abs(slope)>.001:return result
    keep=[x for x,ok in zip(good,inliers) if ok]
    confidence=float(np.median([x["wave_correlation"]-x["strongest_control"] for x in keep]))*math.sqrt(len(keep))
    result.update(status="acoustically_supported_not_certified",offset=intercept,slope=slope,residual_seconds=float(np.max(residual[inliers])),confidence=confidence,fit_observations=keep,observed_span_seconds=float(np.ptp(t[inliers])))
    return result


def verify(records,pairs,out,namespace="evidence-v2",workers=4):
    (out/namespace).mkdir(exist_ok=True)
    def assess_pair(pair):
        ref,query=records[pair["reference"]],records[pair["query"]]
        filename=out/namespace/(ref["id"]+"_"+query["id"]+".json")
        identity=hashlib.sha256(json.dumps({"pair":pair,"reference":ref["cache_sha256"],"query":query["cache_sha256"],"verifier":"v2"},sort_keys=True).encode()).hexdigest()
        if filename.exists():
            result=json.loads(filename.read_text(encoding="utf-8"))
            if result.get("input_digest")!=identity:raise ValueError("stale verification evidence")
        else:
            candidates=[assess_candidate(ref,query,c) for c in pair["candidates"]]
            supported=sorted([c for c in candidates if c["status"]=="acoustically_supported_not_certified"],key=lambda x:-x["confidence"])
            chosen=supported[0] if supported else None
            ambiguous=chosen is not None and len(supported)>1 and supported[1]["confidence"]>=.8*chosen["confidence"]
            result={"reference":pair["reference"],"query":pair["query"],"candidates":candidates,"selected":None if ambiguous else chosen,"ambiguous":ambiguous,"input_digest":identity,"provenance":"discovery_source_reused; no independent certification"}
            save(filename,result)
        return result
    edges=[]
    with ThreadPoolExecutor(max_workers=workers) as pool:
        for i,result in enumerate(pool.map(assess_pair,pairs)):
            if result["selected"] is not None:edges.append(result)
            if (i+1)%10==0 or result["selected"] is not None:
                print(f"VERIFY {i+1}/{len(pairs)} supported_edges={len(edges)} {records[result['query']]['name']} / {records[result['reference']]['name']}",flush=True)
    save(out/(namespace+"-edges.json"),{"version":VERSION,"edges":edges})
    return edges


def envelope_features(rec, out):
    """Low-rate multiband log-energy changes for microphone-robust recovery.

    This is a candidate generator only. Every proposed offset still goes
    through the same waveform, wrong-offset and consistency checks.
    """
    path=out/"features"/(rec["id"]+"-envelope-v1.npz")
    if path.exists():
        with np.load(path) as data:
            if str(data["digest"])!=rec["cache_sha256"]:raise ValueError("stale envelope cache")
            return data["features"]
    pieces=[]
    # Chunk boundaries fall on the 16 ms grid; overlap supplies the FFT tail.
    for first in range(0,round(rec["duration"]*SR),256*3750):
        count=min(256*3750+256,round(rec["duration"]*SR)-first)
        if count<512:break
        samples=read(rec["cache"],first/SR,count/SR)
        _,_,z=signal.stft(samples,fs=SR,nperseg=512,noverlap=256,boundary=None,padded=False)
        power=np.abs(z)**2
        bands=np.asarray([np.log(1e-10+power[lo:hi].mean(axis=0)) for lo,hi in ((7,29),(29,77),(77,193))])
        pieces.append(bands)
    bands=np.concatenate(pieces,axis=1) if pieces else np.empty((3,0))
    features=np.diff(ndimage.gaussian_filter1d(bands,1,axis=1),axis=1).astype(np.float32) if bands.shape[1]>1 else np.empty((3,0),np.float32)
    temporary=path.with_suffix(".partial.npz")
    np.savez_compressed(temporary,features=features,digest=rec["cache_sha256"])
    os.replace(temporary,path)
    return features


def envelope_candidates(reference, query, max_candidates=8):
    """Search the full reference, retaining separated competing peaks."""
    width=min(query.shape[1],1250) # 20 seconds, 16 ms grid
    if width<50 or reference.shape[1]<width:return []
    starts=sorted(set(int(x) for x in np.linspace(0,query.shape[1]-width,3)))
    candidates=[]
    for first in starts:
        q=query[:,first:first+width].astype(np.float64)
        q-=q.mean(axis=1,keepdims=True)
        qenergy=(q*q).sum(axis=1)
        if np.count_nonzero(qenergy>1e-8)<2:continue
        scores=[]
        for band in range(3):
            r=reference[band].astype(np.float64)
            sums=np.r_[0,np.cumsum(r)];squares=np.r_[0,np.cumsum(r*r)]
            variance=np.maximum(0,squares[width:]-squares[:-width]-(sums[width:]-sums[:-width])**2/width)
            dots=signal.correlate(r,q[band],mode="valid",method="fft")
            scores.append(np.divide(dots,np.sqrt(np.maximum(variance*qenergy[band],1e-20))))
        score=np.median(scores,axis=0)
        for _ in range(4):
            best=int(np.argmax(score));strength=float(score[best])
            if strength<.12:break
            candidates.append({"offset":(best-first)*.016,"envelope_score":strength,"anchor_time":(first+width/2)*.016,"method":"multiband_envelope_full_range"})
            score[max(0,best-47):best+48]=-1
    picked=[]
    for c in sorted(candidates,key=lambda c:(-c["envelope_score"],c["offset"])):
        if all(abs(c["offset"]-p["offset"])>=.5 for p in picked):picked.append(c)
        if len(picked)>=max_candidates:break
    return picked


def recover(records, edges, out):
    """Search recordings outside the largest supported component, not a list
    selected by an operator or by baseline timeline positions.
    """
    path=out/"recovery-discovery.json"
    if path.exists():return json.loads(path.read_text(encoding="utf-8"))["pairs"]
    adjacency=defaultdict(set)
    for e in edges:
        a,b=e["reference"],e["query"]
        adjacency[a].add(b);adjacency[b].add(a)
    groups=[];seen=set()
    for i in range(len(records)):
        if i in seen:continue
        group={i};todo=[i];seen.add(i)
        for node in todo:
            for other in sorted(adjacency[node]):
                if other not in seen:seen.add(other);group.add(other);todo.append(other)
        groups.append(group)
    largest=max(groups,key=lambda g:(len(g),sum(records[i]["duration"] for i in g)))
    targets=set(range(len(records)))-largest
    if not targets:
        save(path,{"version":"envelope-recovery-v1","target_recordings":[],"pairs":[],"baseline_used":False})
        return []
    features=[envelope_features(rec,out) for rec in records]
    pairs=[]
    for q,query in enumerate(records):
        searched=0
        for r,ref in enumerate(records):
            if q not in targets and r not in targets:continue
            if (ref["duration"],r)<=(query["duration"],q):continue
            candidates=envelope_candidates(features[r],features[q]);searched+=1
            if candidates:pairs.append({"reference":r,"query":q,"candidates":candidates})
        print(f"RECOVERY {q+1}/{len(records)} {query['name']}: {searched} full-range searches",flush=True)
    save(path,{"version":"envelope-recovery-v1","target_recordings":sorted(targets),"pairs":pairs,"baseline_used":False})
    return pairs


def validate_edges(records, edges, out,namespace=""):
    """Check the frozen pair model on disjoint audio windows.

    These windows are held out of local fitting, not from full-recording
    discovery. They do not supply a calibrated probability of correctness.
    Contradictory strong observations reject the model instead of being
    silently removed as fit outliers.
    """
    reports=[];accepted=[]
    for e in edges:
        ref,query=records[e["reference"]],records[e["query"]]
        model=e["selected"];a=model["offset"];b=1+model["slope"]
        begin=max(0,-a/b)+.1;end=min(query["duration"],(ref["duration"]-a)/b)-.1
        occupied=sorted((max(begin,o["query_time"]-o["window"]/2-.1),min(end,o["query_time"]+o["window"]/2+.1)) for o in model["observations"])
        gaps=[];cursor=begin
        for lo,hi in occupied:
            if lo-cursor>=.55:gaps.append((cursor,lo))
            cursor=max(cursor,hi)
        if end-cursor>=.55:gaps.append((cursor,end))
        regions=[]
        for lo,hi in gaps:
            if hi-lo>=1.3:
                middle=(lo+hi)/2
                regions.extend(((lo,middle-.05),(middle+.05,hi)))
            else:regions.append((lo,hi))
        chosen=[regions[i] for i in sorted(set(int(i) for i in np.linspace(0,len(regions)-1,min(9,len(regions)))))] if regions else []
        observations=[]
        for lo,hi in chosen:
            length=min(2,hi-lo-.1);t=(lo+hi-length)/2
            expected=a+model["slope"]*(t+length/2)
            measured=waveform(ref,query,t,expected,length,.08)
            if measured is None:continue
            controls=[m for shift in (-31,-15,15,31) if (m:=waveform(ref,query,t,expected+shift,length,.08)) is not None]
            strongest=max((m["wave_correlation"] for m in controls),default=1)
            flux=flux_score(ref,query,t,measured["offset"],length)
            supported=measured["local_peak_distinct"] and len(controls)>=2 and measured["wave_correlation"]>=.10 and measured["wave_correlation"]>=strongest*1.5 and measured["wave_correlation"]-strongest>=.035 and (flux>=.25 or measured["wave_correlation"]>=.65)
            residual=measured["offset"]-expected
            observations.append(dict(measured,expected_offset=expected,residual_seconds=residual,strongest_control=strongest,controls=len(controls),flux_correlation=flux,acoustically_supported=supported,passes=supported and abs(residual)<=.025))
        good=[o for o in observations if o["passes"]]
        contradictory=[o for o in observations if o["acoustically_supported"] and not o["passes"]]
        all_good=good+model["fit_observations"]
        covered_span=max((o["query_time"]+o["window"]/2 for o in all_good),default=0)-min((o["query_time"]-o["window"]/2 for o in all_good),default=0)
        coverage=covered_span/max(end-begin,1e-9)
        passed=len(good)>=2 and not contradictory and (end-begin<30 or coverage>=.8)
        report={"reference":e["reference"],"query":e["query"],"status":"pass_disjoint_windows" if passed else "insufficient_or_contradictory_holdout","observations":observations,"passed_windows":len(good),"contradictory_windows":len(contradictory),"supported_span_fraction":coverage,"overlap_seconds":end-begin,"max_supported_residual_seconds":max((abs(o["residual_seconds"]) for o in observations if o["acoustically_supported"]),default=None),"fit_windows_excluded":True,"independent_of_discovery":False}
        reports.append(report)
        if passed:accepted.append(dict(e,validation=report))
    save(out/(namespace+"holdout-validation.json"),{"version":"disjoint-windows-v2-local-ambiguity","pairs":reports,"input_edges":len(edges),"accepted_edges":len(accepted)})
    save(out/(namespace+"validated-edges.json"),{"version":VERSION,"edges":accepted})
    print(f"VALIDATION {len(accepted)}/{len(edges)} pair models passed disjoint-window checks",flush=True)
    return accepted


def refine_failed_models(records, supported, validated, out):
    """Longer-window remeasurement of failed models, with all alternatives
    retained. This changes the measurements, never the acceptance limits.
    """
    accepted={(e["reference"],e["query"]) for e in validated}
    pairs=[]
    for e in supported:
        if (e["reference"],e["query"]) in accepted:continue
        candidates=[]
        for c in e["candidates"]:
            candidate={k:c[k] for k in ("offset","votes","anchor_time","method","envelope_score") if k in c}
            candidate.update(verification_window_seconds=12.0,verification_max_windows=11)
            candidates.append(candidate)
        pairs.append({"reference":e["reference"],"query":e["query"],"candidates":candidates})
    refined=verify(records,pairs,out,"evidence-refined-v1")
    combined=validated+refined
    save(out/"refined-supported-edges.json",{"version":VERSION,"edges":combined,"retry_pairs":len(pairs),"thresholds_relaxed":False})
    return validate_edges(records,combined,out)


def graph_layout(records, edges, excluded=None):
    """Deterministic maximum-support forest, with conflicting cycle edges
    removed by their weakest support. This is consistency, not certification.
    """
    excluded=set(excluded or ())
    active=[i for i in range(len(edges)) if i not in excluded]
    rejected=[]

    def forest(indices):
        parent=list(range(len(records)))
        def find(a):
            while parent[a]!=a:
                parent[a]=parent[parent[a]];a=parent[a]
            return a
        selected=[]
        for k in sorted(indices,key=lambda k:(-edges[k]["selected"]["confidence"],edges[k]["reference"],edges[k]["query"])):
            a,b=edges[k]["reference"],edges[k]["query"]
            aa,bb=find(a),find(b)
            if aa!=bb:parent[bb]=aa;selected.append(k)
        adjacency=defaultdict(list)
        for k in selected:
            e=edges[k]
            adjacency[e["reference"]].append((e["query"],k))
            adjacency[e["query"]].append((e["reference"],k))
        transforms={};groups=[]
        for root in sorted(range(len(records)),key=lambda i:(-records[i]["duration"],records[i]["id"])):
            if root in transforms:continue
            transforms[root]=(0.0,1.0)
            group=[];stack=[root]
            while stack:
                node=stack.pop();group.append(node)
                start,rate=transforms[node]
                for other,k in adjacency[node]:
                    if other in transforms:continue
                    e=edges[k];m=e["selected"]
                    a=m["offset"];b=1+m["slope"]
                    if node==e["reference"]:transforms[other]=(start+rate*a,rate*b)
                    else:transforms[other]=(start-rate*a/b,rate/b)
                    stack.append(other)
            groups.append(sorted(group))
        return selected,adjacency,transforms,groups

    for _ in range(len(edges)+1):
        selected,adj,transforms,groups=forest(active)
        selected_set=set(selected)
        conflicts=[]
        for k in active:
            if k in selected_set:continue
            e=edges[k];rs,rr=transforms[e["reference"]];qs,qr=transforms[e["query"]]
            residual=max(abs(rs+rr*(o["query_time"]+o["offset"])-qs-qr*o["query_time"]) for o in e["selected"]["fit_observations"])
            if residual>.04:conflicts.append((residual,k))
        if not conflicts:break
        residual,k=max(conflicts)
        e=edges[k];start,target=e["reference"],e["query"]
        queue=[(start,[])];seen={start};path=[]
        for node,route in queue:
            if node==target:path=route;break
            for other,j in adj[node]:
                if other not in seen:seen.add(other);queue.append((other,route+[j]))
        weakest=min(path+[k],key=lambda j:(edges[j]["selected"]["confidence"],-j))
        active.remove(weakest)
        rejected.append({"edge":weakest,"reason":"inconsistent_cycle","cycle_max_residual_seconds":residual})
    # Refine each connected component against every retained PCM observation.
    # Rates are free only when a recording has measurements >30 seconds apart.
    fit_reports=[]
    for group in groups:
        root=max(group,key=lambda i:(records[i]["duration"],records[i]["id"]))
        component_edges=[k for k in active if edges[k]["reference"] in group and edges[k]["query"] in group]
        moments=defaultdict(list)
        for k in component_edges:
            e=edges[k]
            for o in e["selected"]["fit_observations"]:
                moments[e["query"]].append(o["query_time"])
                moments[e["reference"]].append(o["query_time"]+o["offset"])
        variables={};counter=0
        for node in group:
            if node==root:continue
            variables[(node,"start")]=counter;counter+=1
            if len(moments[node])>=3 and max(moments[node])-min(moments[node])>=30:
                variables[(node,"drift")]=counter;counter+=1
        if not counter:continue
        rows=[];targets=[]
        for k in component_edges:
            e=edges[k];ref,q=e["reference"],e["query"]
            for o in e["selected"]["fit_observations"]:
                tq=o["query_time"];tr=tq+o["offset"]
                row=np.zeros(counter)
                for node,role,value in ((q,"start",1),(ref,"start",-1),(q,"drift",tq),(ref,"drift",-tr)):
                    if (node,role) in variables:row[variables[node,role]]+=value
                rows.append(row);targets.append(tr-tq)
        if not rows:continue
        a=np.array(rows);b=np.array(targets)
        x=np.zeros(counter)
        origin,origin_rate=transforms[root]
        for (node,kind),j in variables.items():
            x[j]=(transforms[node][0]-origin)/origin_rate if kind=="start" else transforms[node][1]/origin_rate-1
        # Robust IRLS on observation residuals. Regularization discourages
        # extreme rates without fabricating an independently measured clock.
        for _ in range(8):
            residual=a@x-b
            weights=1/np.maximum(1,np.abs(residual)/.015)
            aa=a*np.sqrt(weights[:,None]);bb=b*np.sqrt(weights)
            regular=[]
            for (node,kind),j in variables.items():
                if kind=="drift":
                    row=np.zeros(counter);row[j]=5;regular.append(row)
            if regular:aa=np.vstack([aa,regular]);bb=np.r_[bb,np.zeros(len(regular))]
            x=np.linalg.lstsq(aa,bb,rcond=None)[0]
        max_residual=float(np.max(np.abs(a@x-b)))
        fit_reports.append({"root":root,"nodes":group,"max_residual_seconds":max_residual,"observations":len(rows)})
        if max_residual<=.04 and all(abs(x[j])<=.002 for (node,kind),j in variables.items() if kind=="drift"):
            transforms[root]=(0,1)
            for node in group:
                if node==root:continue
                transforms[node]=(float(x[variables[(node,"start")]]),1+float(x[variables[(node,"drift")]]) if (node,"drift") in variables else 1.0)
    # Test the solved graph against the frozen evidence. An incompatible
    # relation is removed automatically; it cannot silently survive export.
    residual_reports=[];node_error=defaultdict(float);bad=[]
    for k in active:
        e=edges[k];rs,rr=transforms[e["reference"]];qs,qr=transforms[e["query"]]
        observations=e["selected"]["fit_observations"]+[o for o in e.get("validation",{}).get("observations",[]) if o.get("passes")]
        residual=max(abs(rs+rr*(o["query_time"]+o["offset"])-qs-qr*o["query_time"]) for o in observations)
        residual_reports.append({"edge":k,"maximum_residual_seconds":residual,"observations":len(observations)})
        for node in (e["reference"],e["query"]):node_error[node]=max(node_error[node],residual)
        if residual>.04 or any(abs(transforms[n][1]-1)>.002 for n in (e["reference"],e["query"])):bad.append((residual,k))
    if bad:
        residual,k=max(bad)
        layout,report=graph_layout(records,edges,excluded|{k}|{r["edge"] for r in rejected})
        report["rejected_edges"]+=rejected+[{"edge":k,"reason":"global_evidence_inconsistency","maximum_residual_seconds":residual}]
        return layout,report
    groups.sort(key=lambda g:(-len(g),-sum(records[i]["duration"] for i in g),g[0]))
    layout=[]
    for number,group in enumerate(groups,1):
        lo=min(transforms[i][0] for i in group)
        span=0.0
        for i in group:
            start,rate=transforms[i]
            start=start-lo
            span=max(span,start+rate*records[i]["duration"])
            relevant=[edges[k]["selected"] for k in active if i in (edges[k]["reference"],edges[k]["query"])]
            error=max([m["residual_seconds"] for m in relevant]+[node_error[i],.001])
            status="acoustically_supported_not_certified" if number==1 and len(group)>1 else "unresolved_disconnected_group" if len(group)>1 else "unresolved_no_supported_relation"
            layout.append({"recording":i,"component":number,"component_size":len(group),"start_seconds":start,"rate":rate,"fit_residual_seconds":error,"status":status})
    return layout,{"components":groups,"active_edges":active,"rejected_edges":rejected,"component_fits":fit_reports,"global_evidence_residuals":residual_reports,"maximum_global_residual_seconds":max((r['maximum_residual_seconds'] for r in residual_reports),default=0),"independent_certification":"not_performed"}


def finish(records,edges,out,tolerance,name="OpenSync",unsynced="append",profiles=None):
    if profiles is not None:
        # A namespace object also works when this module is loaded by a test
        # without registration in sys.modules.
        from types import SimpleNamespace
        edges,decisions=relation_graph.select_edges(SimpleNamespace(graph_layout=graph_layout),records,edges,profiles)
        save(out/'event-decisions.json',{'decisions':decisions,'baseline_positions_used':False})
        layout,graph=relation_graph.solve(records,edges)
        graph['event_decisions']=decisions
        from acoustic_context import relationship_map
        graph['acoustic_relationships']=relationship_map(records,profiles,graph)
        save(out/'acoustic-relationships.json',graph['acoustic_relationships'])
        discovery_path=out/'distributed-discovery.json'
        if discovery_path.exists():
            discovery=json.loads(discovery_path.read_text(encoding='utf-8'))
            graph['candidate_discovery']={k:v for k,v in discovery.items() if k!='pairs'}
            graph['candidate_discovery']['pairs']=[{'reference':p['reference'],'query':p['query'],'candidates':p['candidates'],'status':'DISCOVERY_ONLY'} for p in discovery['pairs']]
        graph['unresolved_timing_hypotheses']=[dict(reference=p['reference'],query=p['query'],hypotheses=[dict(prior=h['prior'],status=h['status'],audit_result=h.get('audit_result')) for h in p['profiles'] if h['status']!='validated_relation']) for p in profiles if any(h['status']!='validated_relation' for h in p['profiles'])]
    else:layout,graph=graph_layout(records,edges)
    save(out/"graph.json",graph)
    project_path=out/"OpenSync.opensync"
    save_project(project_path,records,edges,layout,graph,VERSION,name)
    return export_project(project_path,out,unsynced,tolerance*1000)


def main():
    if len(sys.argv)>1 and sys.argv[1]=='audit-import':
        from project_export import audit_roundtrip
        parser=argparse.ArgumentParser(prog='opensync audit-import',description='Compare an exported timeline with Premiere XML exported after import')
        parser.add_argument('original_xml');parser.add_argument('premiere_xml');parser.add_argument('--output',default='import-audit.json')
        args=parser.parse_args(sys.argv[2:]);report=audit_roundtrip(args.original_xml,args.premiere_xml,args.output)
        if report['errors']:raise SystemExit(1)
        return
    if len(sys.argv)>1 and sys.argv[1]=="export":
        parser=argparse.ArgumentParser(prog="opensync export",description="Export a saved OpenSync project without repeating synchronization")
        parser.add_argument("project")
        parser.add_argument("--output")
        parser.add_argument("--unsynced",choices=("append","separate-sequence"),default="append")
        parser.add_argument("--max-residual-ms",type=float,default=150)
        args=parser.parse_args(sys.argv[2:])
        export_project(args.project,args.output or Path(args.project).resolve().parent,args.unsynced,args.max_residual_ms)
        return
    parser=argparse.ArgumentParser(prog="opensync sync",description=__doc__)
    parser.add_argument("--input")
    parser.add_argument("--source",action="append")
    parser.add_argument("--files",nargs="+",help="media paths to synchronize automatically")
    parser.add_argument("--file-list",help="UTF-8 text (one path per line) or JSON array; relative paths resolve beside the list")
    parser.add_argument("--output",default="OpenSync-Output")
    parser.add_argument("--cache-index")
    parser.add_argument("--name",default="OpenSync",help="saved project name")
    parser.add_argument("--unsynced",choices=("append","separate-sequence"),default="append",help="Premiere export arrangement for unresolved recordings; does not change saved sync measurements")
    parser.add_argument("--max-residual-ms",type=float,default=150)
    parser.add_argument("--resume",action="store_true")
    parser.add_argument("--stage",choices=("cache","discover","verify","all"),default="all")
    args=parser.parse_args()
    if not 1<=args.max_residual_ms<=150:parser.error("residual limit must be in [1,150] ms")
    out=Path(args.output).resolve()
    explicit=input_files(args)
    if out.exists() and any(out.iterdir()) and not (out/"run-config.json").exists():parser.error("output directory contains unrelated files; choose an empty directory")
    out.mkdir(parents=True,exist_ok=True)
    config={"version":VERSION,"input":str(Path(args.input).resolve()),"sources":sorted(args.source),"max_residual_ms":args.max_residual_ms} if not explicit else {"version":VERSION,"files":[str(p) for p in explicit],"max_residual_ms":args.max_residual_ms}
    if (out/"run-config.json").exists():
        previous=json.loads((out/"run-config.json").read_text(encoding="utf-8"))
        if previous!=config:
            # Changing from a folder selector to its exact file list changes
            # the interface, not the immutable source selection or DSP data.
            inventory=out/"inventory.json"
            same_files=explicit and inventory.exists() and sorted(str(p).casefold() for p in explicit)==sorted(r["source"].casefold() for r in json.loads(inventory.read_text(encoding="utf-8"))["records"])
            same_settings=all(previous.get(k)==config.get(k) for k in ("version","max_residual_ms"))
            if not same_files or not same_settings:raise ValueError("cannot resume with changed input selection or DSP settings")
            save(out/"run-config.json",config)
    else:save(out/"run-config.json",config)
    for folder in ("cache","features","evidence"):(out/folder).mkdir(exist_ok=True)
    records=prepare(args,out)
    if args.stage=="cache":return
    pairs=discover(records,out)
    if args.stage=="discover":return
    edges=verify(records,pairs,out)
    recovery=recover(records,edges,out)
    initial={(p["reference"],p["query"]):p for p in pairs}
    for p in recovery:
        prior=initial.get((p["reference"],p["query"]),{}).get("candidates",[])
        # Evaluate old and new alternatives together so recovery cannot hide
        # a competing match that the first pass already discovered.
        p["candidates"]+= [c for c in prior if all(abs(c["offset"]-r["offset"])>=.2 for r in p["candidates"])]
    recovered=verify(records,recovery,out,"evidence-recovery-v2")
    replaced={(p["reference"],p["query"]) for p in recovery}
    edges=[e for e in edges if (e["reference"],e["query"]) not in replaced]+recovered
    save(out/"supported-edges.json",{"version":VERSION,"edges":edges,"recovery":"envelope-recovery-v1"})
    validated=validate_edges(records,edges,out,"initial-")
    edges=refine_failed_models(records,edges,validated,out)
    from types import SimpleNamespace
    api=SimpleNamespace(waveform=waveform,flux_score=flux_score,read=read,save=save)
    profiles=refinement.run_profiles(api,records,out)
    from distributed_recovery import run as distributed_recovery
    preliminary,_=relation_graph.select_edges(SimpleNamespace(graph_layout=graph_layout),records,edges,profiles)
    preliminary_layout,_=relation_graph.solve(records,preliminary)
    if distributed_recovery(api,records,preliminary_layout,recovery,out) is not None:
        profiles=refinement.run_profiles(api,records,out)
    if args.stage=="verify":return
    finish(records,edges,out,args.max_residual_ms/1000,args.name,args.unsynced,profiles)


if __name__=="__main__":
    main()
