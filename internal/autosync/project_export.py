"""Versioned OpenSync projects and a general Premiere/FCP7 XML exporter.

The project stores full-precision component-relative measurements. Export
arrangement and frame quantization never overwrite those measurements.
"""
from collections import Counter, defaultdict
from fractions import Fraction
import hashlib
import json
import math
import os
from pathlib import Path
import subprocess
import tempfile
import numpy as np
from urllib.parse import unquote, urlparse
import uuid
import xml.etree.ElementTree as ET
from time_mapping import validate as validate_mapping, value as mapped_time, inverse as inverse_mapping, video_segments
from source_metadata import source_timecode


def canonical(value):
    return json.dumps(value,sort_keys=True,separators=(",",":"),ensure_ascii=False,allow_nan=False).encode("utf-8")


def digest(path):
    h=hashlib.sha256()
    with open(path,"rb") as f:
        for block in iter(lambda:f.read(2**20),b""):h.update(block)
    return h.hexdigest()


def save_json(path,value):
    path=Path(path);tmp=path.with_suffix(path.suffix+".tmp")
    tmp.write_text(json.dumps(value,indent=2,ensure_ascii=False,allow_nan=False)+"\n",encoding="utf-8")
    os.replace(tmp,path)


def save_project(path,records,edges,layout,graph,engine_version,name="OpenSync"):
    records=[dict(r,source_timecode=source_timecode(r)) if 'source_timecode' not in r else dict(r) for r in records]
    models=[]
    for item in layout:
        model=dict(item)
        rec=records[item["recording"]]
        origin=float((rec.get("audio") or {}).get("start_time") or 0)-float(rec["video"].get("start_time") or 0) if rec.get("video") and rec.get("audio") else 0.0
        model["analysis_component_offset_seconds"]=model.pop("start_seconds")
        model["analysis_audio_origin_seconds"]=origin
        model["component_offset_seconds"]=model["analysis_component_offset_seconds"]-model["rate"]*origin
        if model.get('timing_knots'):
            model['analysis_timing_knots']=model['timing_knots']
            model['timing_knots']=[[t+origin,v] for t,v in model['timing_knots']]
            model['component_offset_seconds']=mapped_time(model['timing_knots'],0)
        model["time_domain"]=f"component-{item['component']}"
        model["globally_resolved"]=not item["status"].startswith("unresolved")
        models.append(model)
    payload={"name":name,"engine_version":engine_version,"recordings":records,"timing_models":models,"validated_relations":edges,"graph":graph,
             "timing_precision":"IEEE-754 binary64 seconds, affine rates and monotonic timing knots; no timeline frame quantization",
             "verification_status":"acoustic_and_graph_evidence_supported; production_import_review_pending",
             "source_media_modified":False}
    project={"format":"opensync.project","schema_version":2 if any(m.get('timing_knots') for m in models) else 1,"payload":payload,"content_sha256":hashlib.sha256(canonical(payload)).hexdigest()}
    save_json(path,project)
    return project


def load_project(path):
    project=json.loads(Path(path).read_text(encoding="utf-8"))
    if project.get("format")!="opensync.project" or project.get("schema_version") not in (1,2):raise ValueError("unsupported OpenSync project format")
    payload=project["payload"]
    if hashlib.sha256(canonical(payload)).hexdigest()!=project["content_sha256"]:raise ValueError("OpenSync project integrity check failed")
    records=payload["recordings"];models=payload["timing_models"]
    if not records or len({r["id"] for r in records})!=len(records):raise ValueError("invalid project source identities")
    if sorted(m["recording"] for m in models)!=list(range(len(records))):raise ValueError("project timing models do not account for every recording")
    for m in models:
        if not math.isfinite(m["component_offset_seconds"]) or not math.isfinite(m["rate"]) or m["rate"]<=0:raise ValueError("invalid precise timing model")
        if m.get('timing_knots'):validate_mapping(m['timing_knots'])
        components=payload["graph"]["components"]
        if not 1<=m["component"]<=len(components) or m["recording"] not in components[m["component"]-1] or m["component_size"]!=len(components[m["component"]-1]):raise ValueError("project component identity mismatch")
    return project


def tag(parent,name,text=None,**attributes):
    element=ET.SubElement(parent,name,attributes)
    if text is not None:element.text=str(text)
    return element


def rate_value(rec,default=50):
    video=rec.get("video")
    raw=video.get("avg_frame_rate") or video.get("r_frame_rate") if video else default
    rate=Fraction(str(raw)).limit_denominator(1001)
    if rate<=0:raise ValueError("invalid source frame rate: "+rec["name"])
    if rate.denominator not in (1,1001):raise ValueError("source requires variable/unusual frame-rate handling: "+rec["name"])
    return rate


def xml_rate(parent,rate):
    rate=Fraction(str(rate)).limit_denominator(1001)
    node=tag(parent,"rate");tag(node,"ntsc","TRUE" if rate.denominator==1001 else "FALSE");tag(node,"timebase",round(rate))


def read_rate(node):
    base=int(node.findtext("./rate/timebase"))
    return Fraction(base*1000,1001) if node.findtext("./rate/ntsc")=="TRUE" else Fraction(base)


def source_frames(rec,fps):
    count=(rec.get("video") or {}).get("nb_frames")
    return int(count) if count and str(count).isdigit() else round(rec["duration"]*float(fps))


def channels(rec):return int((rec.get("audio") or {}).get("channels",0))


def channel_type(count):return "stereo" if count==2 else "mono" if count<=1 else "multichannel"


def source_groups(records):
    """Directories are provenance groups, not inferred physical devices."""
    keys=sorted({("Camera" if r.get("video") else "Audio",str(Path(r["source"]).parent)) for r in records})
    counts=Counter();names={}
    for kind,parent in keys:
        counts[kind]+=1;names[(kind,parent)]=f"{kind} {counts[kind]}"
    return {i:names[("Camera" if r.get("video") else "Audio",str(Path(r["source"]).parent))] for i,r in enumerate(records)}


def file_reference(parent,rec,fps,definitions,source=None,duration=None):
    fid="file-"+rec["id"]+("-corrected" if source else "")
    node=tag(parent,"file",id=fid)
    if fid in definitions:return node
    definitions.add(fid);path=Path(source or rec["source"])
    tag(node,"name",path.name);xml_rate(node,fps)
    total=round(duration*float(fps)) if duration is not None else source_frames(rec,fps)
    tag(node,"duration",total)
    tc=None if source else rec.get('source_timecode')
    timecode=tag(node,'timecode');xml_rate(timecode,tc['rate'] if tc else fps)
    tag(timecode,'string',tc['string'] if tc else '00:00:00:00')
    tag(timecode,'frame',tc['frame'] if tc else 0)
    tag(timecode,'displayformat',tc['displayformat'] if tc else 'NDF')
    media=tag(node,'media')
    if rec.get("video") and not source:
        video=tag(media,"video");sample=tag(video,"samplecharacteristics")
        for key,value in (("width",rec["video"].get("width",1920)),("height",rec["video"].get("height",1080)),("anamorphic","FALSE"),("pixelaspectratio","square"),("fielddominance","none")):tag(sample,key,value)
        xml_rate(sample,fps);tag(video,"duration",total)
    if channels(rec):
        audio=tag(media,"audio");tag(audio,"channelcount",channels(rec));sample=tag(audio,"samplecharacteristics")
        tag(sample,"depth",24 if source else rec["audio"].get("bits_per_sample",16) or 16);tag(sample,"samplerate",rec["audio"].get("sample_rate",48000))
        tag(audio,"layout",channel_type(channels(rec)))
        for c in range(channels(rec)):tag(tag(audio,"audiochannel"),"sourcechannel",c+1)
    tag(node,"pathurl",path.as_uri())
    return node


def link_items(clip,instances):
    for item in instances:
        link=tag(clip,"link")
        for key,value in (("mediatype",item["kind"]),("trackindex",item["track_index"]),("clipindex",item["clip_index"]),("groupindex",1),("linkclipref",item["id"])):tag(link,key,value)


def track_attributes(kind,count,channel):
    if kind=="video":return {}
    return {"TL.SQTrackExpanded":"0","currentExplodedTrackIndex":str(channel),"premiereTrackType":channel_type(count).capitalize(),"totalExplodedTrackCount":str(count)}


def add_source_bins(children,records,groups,fps,definitions):
    bins={}
    for i,rec in enumerate(records):
        group=groups[i]
        if group not in bins:
            folder=tag(children,"bin");tag(folder,"name",group);bins[group]=tag(folder,"children")
        source_rate=rate_value(rec,fps);total=source_frames(rec,source_rate)
        master="master-"+rec["id"]
        clip=tag(bins[group],"clip",id=master,explodedTracks="TRUE")
        tag(clip,"name",rec["name"]);tag(clip,"duration",total);xml_rate(clip,source_rate);tag(clip,"masterclipid",master);tag(clip,"ismasterclip","TRUE")
        media=tag(clip,"media");instances=[]
        kinds=([("video",0)] if rec.get("video") else [])+[("audio",c) for c in range(channels(rec))]
        for kind,c in kinds:instances.append({"id":master+f"-{kind}-{c}","kind":kind,"track_index":c+1,"clip_index":1})
        for kind in ("video","audio"):
            if not any(k==kind for k,c in kinds):continue
            section=tag(media,kind)
            for item in [x for x in instances if x["kind"]==kind]:
                track=tag(section,"track",**track_attributes(kind,channels(rec),item["track_index"]-1))
                child=tag(track,"clipitem",id=item["id"])
                tag(child,"masterclipid",master);file_reference(child,rec,source_rate,definitions)
                source=tag(child,"sourcetrack");tag(source,"mediatype",kind);tag(source,"trackindex",item["track_index"])
                link_items(child,instances);tag(track,"enabled","TRUE");tag(track,"locked","FALSE")


def arrange(placements,fps,policy):
    """Apply the chosen presentation policy without touching project models."""
    resolved=[p for p in placements if not p["status"].startswith("unresolved")]
    unresolved=[p for p in placements if p["status"].startswith("unresolved")]
    def move(parts,new_start,sequence):
        if not parts:return new_start
        shift=new_start-min(p["start"] for p in parts)
        for p in parts:p["start"]+=shift;p["end"]+=shift;p["sequence"]=sequence
        return max(p["end"] for p in parts)
    main_end=move(resolved,0,"synced")
    sequence="synced" if policy=="append" else "unresolved"
    cursor=main_end+round(10*float(fps)) if resolved and policy=="append" and unresolved else 0
    first_unresolved=cursor if unresolved else None
    components=sorted({p["component"] for p in unresolved},key=lambda c:(not any(p["has_video"] for p in unresolved if p["component"]==c),min(p["recording"] for p in unresolved if p["component"]==c)))
    for component in components:
        cursor=move([p for p in unresolved if p["component"]==component],cursor,sequence)+round(float(fps))
    return {"synchronized_end_frame":main_end,"unresolved_start_frame":first_unresolved,"unresolved_sequence":sequence if unresolved else None}


def render_mapped_audio(rec,points,target):
    """Render sequential audio-only pieces with short overlapping joins.

    Input remains untouched. Exact output sample counts prevent errors from
    accumulating at joins. Video never enters this rendering path.
    """
    if rec.get('video'):raise ValueError('audio correction cannot retime video')
    duration=rec['duration'];origin=mapped_time(points,0)
    sr=int((rec.get('audio') or {}).get('sample_rate') or 48000)
    channel_count=channels(rec);inv=inverse_mapping(points)
    boundaries=sorted({0,round((mapped_time(points,duration)-origin)*sr)}|{round((v-origin)*sr) for t,v in points if 0<t<duration})
    if any(b<=a for a,b in zip(boundaries,boundaries[1:])):raise ValueError('invalid audio render intervals')
    fade=min(round(.005*sr),min(b-a for a,b in zip(boundaries,boundaries[1:]))//4)
    local=rec.get('local_source',rec['source'])
    if not Path(local).is_file():local=rec['source']
    with tempfile.TemporaryDirectory(prefix='opensync-audio-',dir=target.parent) as temporary:
        root=Path(temporary);assembled=root/'assembled.s32';tail=b'';frame_bytes=channel_count*4
        with assembled.open('wb') as output:
            for i,(first,last) in enumerate(zip(boundaries,boundaries[1:])):
                left=first-(fade if i else 0);right=last+(fade if i<len(boundaries)-2 else 0)
                source_first=max(0,mapped_time(inv,origin+left/sr));source_last=min(duration,mapped_time(inv,origin+right/sr))
                count=right-left;tempo=(source_last-source_first)/(count/sr)
                if not .5<=tempo<=2:raise ValueError('unsupported audio timing rate')
                part=root/'part.s32'
                filters=f'atempo={tempo:.14f},apad,atrim=end_sample={count}'
                subprocess.run(['ffmpeg','-nostdin','-v','error','-y','-ss',f'{source_first:.12f}','-t',f'{source_last-source_first:.12f}','-i',local,'-map','0:a:0','-af',filters,'-ar',str(sr),'-ac',str(channel_count),'-c:a','pcm_s32le','-f','s32le',str(part)],check=True)
                with part.open('rb') as source:
                    if part.stat().st_size!=count*frame_bytes:raise ValueError('audio segment duration mismatch')
                    overlap=2*fade if i else 0
                    if overlap:
                        head=source.read(overlap*frame_bytes)
                        a=np.frombuffer(tail,dtype='<i4').reshape(-1,channel_count).astype(np.float64)
                        b=np.frombuffer(head,dtype='<i4').reshape(-1,channel_count).astype(np.float64)
                        weights=np.linspace(0,1,overlap)[:,None]
                        output.write(np.clip(np.rint(a*(1-weights)+b*weights),-2147483648,2147483647).astype('<i4').tobytes())
                    keep=2*fade if i<len(boundaries)-2 else 0
                    remaining=count-overlap-keep
                    while remaining:
                        n=min(65536,remaining);output.write(source.read(n*frame_bytes));remaining-=n
                    tail=source.read(keep*frame_bytes) if keep else b''
        if assembled.stat().st_size!=boundaries[-1]*frame_bytes:raise ValueError('audio join changed total duration')
        partial=target.with_suffix('.partial.wav')
        subprocess.run(['ffmpeg','-nostdin','-v','error','-y','-f','s32le','-ar',str(sr),'-ac',str(channel_count),'-i',str(assembled),'-c:a','pcm_s24le','-rf64','auto',str(partial)],check=True)
        os.replace(partial,target)


def export_project(project_path,out,unsynced="append",max_residual_ms=150):
    if unsynced not in ("append","separate-sequence"):raise ValueError("unknown unresolved export policy")
    if not 1<=max_residual_ms<=150:raise ValueError("maximum residual must be 1..150 ms")
    project=load_project(project_path);payload=project["payload"]
    # Older native projects did not retain source timecode. Read only the
    # missing source-address metadata; never alter their measured alignment.
    records=[dict(r,source_timecode=source_timecode(r)) if 'source_timecode' not in r else r for r in payload['recordings']]
    for rec in records:
        path=Path(rec["source"])
        if not path.is_file():raise ValueError("source unavailable: "+str(path))
        st=path.stat()
        if "source_bytes" in rec and (st.st_size,st.st_mtime_ns)!=(rec["source_bytes"],rec["source_mtime_ns"]):raise ValueError("source changed since synchronization: "+str(path))
    out=Path(out).resolve();out.mkdir(parents=True,exist_ok=True)
    rates=Counter(rate_value(r) for r in records if r.get("video"));fps=max(rates,key=lambda r:(rates[r],r)) if rates else Fraction(50)
    groups=source_groups(records);placements=[];derivatives=[];tolerance=max_residual_ms/1000
    for model in payload["timing_models"]:
        item=dict(model);i=item["recording"];rec=records[i];source_rate=rate_value(rec,fps);rate=item["rate"];duration=rec["duration"]
        source_duration=source_frames(rec,source_rate)/float(source_rate)
        points=item.get('timing_knots') or [[0,item['component_offset_seconds']],[source_duration,item['component_offset_seconds']+rate*source_duration]]
        check_times=[0,source_duration]+[t for t,_ in points if 0<t<source_duration]
        offsets=[mapped_time(points,t)-t for t in check_times]
        predicted=(max(offsets)-min(offsets))/2+item["fit_residual_seconds"]+.5/float(fps)
        item["predicted_constant_residual_seconds"]=predicted
        source=None;actual=duration;count=1
        if item["status"].startswith("unresolved"):
            # No global timing is asserted for this recording.
            rate=1 if item["component_size"]==1 else rate
        elif predicted>tolerance:
            budget=tolerance-item["fit_residual_seconds"]-.5/float(fps)
            if budget<=0 or (not rec.get('video') and budget<.03):item["status"]="unresolved_timing_uncertainty"
            elif rec.get("video"):pass  # Partition against every timing knot below.
            else:
                folder=out/"Audio Derivatives";folder.mkdir(exist_ok=True)
                key=hashlib.sha256(canonical({"id":rec["id"],"mapping":points,"source":rec.get("cache_sha256"),"renderer":"mapped-audio-v1"})).hexdigest()[:16]
                target=folder/(key+".wav")
                if not target.exists():
                    render_mapped_audio(rec,points,target)
                metadata=json.loads(subprocess.check_output(["ffprobe","-v","error","-show_format","-of","json",str(target)]))
                actual=float(metadata["format"]["duration"])
                if abs(actual-(mapped_time(points,duration)-mapped_time(points,0)))>.001:raise ValueError("timing derivative failed duration validation")
                source=str(target);derivatives.append({"recording_id":rec["id"],"original":rec["source"],"derivative":source,"rate":rate,"sha256":digest(target),"actual_duration_seconds":actual})
        total=source_frames(rec,source_rate)
        if not item['status'].startswith('unresolved') and rec.get('video'):
            parts=video_segments(points,total,float(source_rate),tolerance-item['fit_residual_seconds']-.5/float(fps))
        else:parts=[(0,total,(min(offsets)+max(offsets))/2,(max(offsets)-min(offsets))/2)]
        count=len(parts)
        for segment,(first,last,offset,approximation_error) in enumerate(parts):
            start_seconds=first/float(source_rate)+offset
            start=round((item["component_offset_seconds"] if source else start_seconds)*float(fps))
            end=start+round((actual if source else (last-first)/float(source_rate))*float(fps))
            placements.append(dict(item,id=f"recording-{rec['id']}-part-{segment+1}",source_in=0 if source else first,source_out=round(actual*float(source_rate)) if source else last,source_fps=str(source_rate),start=start,end=end,segment=segment+1,segments=count,derivative=source,has_video=bool(rec.get("video")),presentation_error_seconds=(.03 if source else approximation_error)+item['fit_residual_seconds']+.5/float(fps)))
    arrangement=arrange(placements,fps,unsynced)
    root=ET.Element("xmeml",version="4");project_node=tag(root,"project");tag(project_node,"name",payload["name"]);children=tag(project_node,"children");definitions=set()
    add_source_bins(children,records,groups,fps,definitions)
    if derivatives:
        corrected=[]
        for d in derivatives:
            rec=next(r for r in records if r["id"]==d["recording_id"])
            corrected.append(dict(rec,id=rec["id"]+"-corrected",name=Path(d["derivative"]).name,source=d["derivative"],duration=d["actual_duration_seconds"],video=None,audio=dict(rec["audio"],bits_per_sample=24)))
        add_source_bins(children,corrected,{i:"Timing-corrected Audio" for i in range(len(corrected))},fps,definitions)
    for sequence_name in ("synced","unresolved"):
        parts=[p for p in placements if p["sequence"]==sequence_name]
        if not parts and sequence_name=="unresolved":continue
        sequence=tag(children,"sequence",id="opensync-"+sequence_name)
        tag(sequence,"name","Synced Sequence" if sequence_name=="synced" else "Unresolved Clips")
        tag(sequence,"uuid",str(uuid.uuid5(uuid.NAMESPACE_URL,project["content_sha256"]+sequence_name)))
        tag(sequence,"duration",max((p["end"] for p in parts),default=0));xml_rate(sequence,fps)
        media=tag(sequence,"media");video=tag(media,"video");audio=tag(media,"audio");sample=tag(tag(video,"format"),"samplecharacteristics")
        camera=next((r["video"] for r in records if r.get("video")),{})
        for key,value in (("width",camera.get("width",1920)),("height",camera.get("height",1080)),("anamorphic","FALSE"),("pixelaspectratio","square")):tag(sample,key,value)
        xml_rate(sample,fps)
        audio_sample=tag(tag(audio,"format"),"samplecharacteristics");tag(audio_sample,"depth",24);tag(audio_sample,"samplerate",48000)
        lanes=defaultdict(list)
        for p in sorted(parts,key=lambda p:(p["start"],p["recording"],p["segment"])):
            rec=records[p["recording"]];p["instances"]=[]
            for kind,c in ([ ("video",0)] if rec.get("video") else [])+[("audio",c) for c in range(channels(rec))]:
                choices=lanes[(kind,groups[p["recording"]],c,channels(rec))]
                lane=next((l for l in choices if l[-1]["placement"]["end"]<=p["start"]),None)
                if lane is None:lane=[];choices.append(lane)
                instance={"id":p["id"]+f"-{kind}-{c}","kind":kind,"channel":c,"placement":p}
                lane.append(instance);p["instances"].append(instance)
        tracks=[];counts=Counter()
        for (kind,group,c,nchannels),choices in sorted(lanes.items()):
            for number,lane in enumerate(choices,1):
                counts[kind]+=1
                track=tag(video if kind=="video" else audio,"track",**track_attributes(kind,nchannels,c))
                # FCP7 track elements do not define a name child; bins and
                # stable ordering carry the source-group identity instead.
                for index,item in enumerate(lane,1):item.update(track_index=counts[kind],clip_index=index)
                tracks.append((track,lane))
        for track,lane in tracks:
            for instance in lane:
                p=instance["placement"];rec=records[p["recording"]];source_rate=Fraction(p["source_fps"])
                clip=tag(track,"clipitem",id=instance["id"],premiereChannelType=channel_type(channels(rec)))
                tag(clip,"name",rec["name"]+(f" [part {p['segment']}/{p['segments']}]" if p["segments"]>1 else ""))
                tag(clip,"duration",source_frames(rec,source_rate) if not p["derivative"] else p["source_out"]);xml_rate(clip,source_rate)
                for key,value in (("in",p["source_in"]),("out",p["source_out"]),("start",p["start"]),("end",p["end"])):tag(clip,key,value)
                tag(clip,"masterclipid","master-"+rec["id"] if not p["derivative"] else "master-"+rec["id"]+"-corrected")
                file_reference(clip,rec,source_rate,definitions,p["derivative"],p["source_out"]/float(source_rate) if p["derivative"] else None)
                source=tag(clip,"sourcetrack");tag(source,"mediatype",instance["kind"]);tag(source,"trackindex",instance["channel"]+1)
                link_items(clip,p["instances"])
                tag(tag(clip,"labels"),"label2","Rose" if p["status"].startswith("unresolved") else "Caribbean")
                tag(tag(clip,"logginginfo"),"description","Unresolved relative to the main sequence" if p["status"].startswith("unresolved") else "OpenSync acoustic and graph checks passed; production review pending")
            tag(track,"enabled","TRUE");tag(track,"locked","FALSE")
        unresolved=[p for p in parts if p["status"].startswith("unresolved")]
        if unresolved:
            marker=tag(sequence,"marker");tag(marker,"name","UNRESOLVED CLIPS — NOT SYNCHRONIZED");tag(marker,"comment","These recordings have no accepted position relative to the main synchronized group.");tag(marker,"in",min(p["start"] for p in unresolved));tag(marker,"out",-1)
    ET.indent(root,space="  ");xml_path=out/"OpenSync.xml"
    temporary=out/"OpenSync.partial.xml"
    temporary.write_bytes(b'<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE xmeml>\n'+ET.tostring(root,encoding="utf-8"))
    public=[{k:v for k,v in p.items() if k!="instances"} for p in placements]
    audit_xml(temporary,records,public,unsynced)
    os.replace(temporary,xml_path)
    manifest={"status":"EXPORTED_REQUIRES_PREMIERE_AND_ACCURACY_REVIEW","project":str(Path(project_path).resolve()),"project_content_sha256":project["content_sha256"],"xml":str(xml_path),"xml_sha256":digest(xml_path),"recordings":len(records),"sequence_fps":str(fps),"unsynced_policy":unsynced,"arrangement":arrangement,"placements":public,"derivatives":derivatives,"unresolved_recordings":[records[i]['name'] for i in sorted({p['recording'] for p in placements if p['status'].startswith('unresolved')})],"source_media_modified":False}
    save_json(out/"timeline-manifest.json",manifest)
    print(f"PROJECT {project_path}\nEXPORT {xml_path}\nRECORDINGS {len(records)}\nUNRESOLVED {len(manifest['unresolved_recordings'])}",flush=True)
    return manifest


def audit_xml(path,records,placements,policy="append"):
    tree=ET.parse(path);root=tree.getroot();seen={};locations={};tracks=Counter();counts=Counter();files={}
    for node in root.findall(".//file"):
        if node.find("pathurl") is not None:
            if node.get("id") in files:raise ValueError("duplicate file definition")
            files[node.get("id")]=node
    masters={c.get("id"):c for c in root.findall("./project/children/bin/children/clip")}
    expected_masters={"master-"+r["id"] for r in records}|{"master-"+records[p["recording"]]["id"]+"-corrected" for p in placements if p["derivative"]}
    if set(masters)!=expected_masters:raise ValueError("source-bin coverage mismatch")
    by_id={p["id"]:p for p in placements}
    for sequence in root.findall("./project/children/sequence"):
        sequence_rate=read_rate(sequence);sequence_id=sequence.get("id").removeprefix("opensync-")
        for kind in ("video","audio"):
            for ti,track in enumerate(sequence.findall(f"./media/{kind}/track"),1):
                tracks[kind]+=1;last=0
                for ci,clip in enumerate(track.findall("clipitem"),1):
                    ident=clip.get("id")
                    if ident in seen:raise ValueError("duplicate timeline identity")
                    start,end=int(clip.findtext("start")),int(clip.findtext("end"));first,stop=int(clip.findtext("in")),int(clip.findtext("out"));source_rate=read_rate(clip)
                    if start<last or end<=start or first<0 or abs((stop-first)/float(source_rate)-(end-start)/float(sequence_rate))>.51/float(sequence_rate):raise ValueError("timeline collision or invalid duration")
                    last=end;seen[ident]=clip;locations[ident]=(sequence_id,kind,ti,ci);counts[kind]+=1
                    base=ident.rsplit("-"+kind+"-",1)[0];p=by_id[base];rec=records[p["recording"]]
                    if (start,end,first,stop)!=(p["start"],p["end"],p["source_in"],p["source_out"]) or p["sequence"]!=sequence_id:raise ValueError("XML/placement timing mismatch")
                    definition=files[clip.find("file").get("id")]
                    expected=Path(p["derivative"] or rec["source"]).as_uri()
                    if definition.findtext("pathurl")!=expected:raise ValueError("XML source path identity mismatch")
                    tc=rec.get('source_timecode') if not p['derivative'] else None
                    if int(definition.findtext('timecode/frame','-1'))!=(tc['frame'] if tc else 0):raise ValueError('source timecode origin mismatch')
                    if clip.findtext("masterclipid") not in masters:raise ValueError("missing source master clip")
                    source=clip.find("sourcetrack");channel=int(source.findtext("trackindex"))
                    if source.findtext("mediatype")!=kind or not 1<=channel<=(1 if kind=="video" else channels(rec)):raise ValueError("source channel identity mismatch")
    for ident,clip in seen.items():
        links=clip.findall("link");base=ident.rsplit("-"+locations[ident][1]+"-",1)[0];p=by_id[base];rec=records[p["recording"]]
        expected=channels(rec)+int(bool(rec.get("video")))
        if len(links)!=expected or len({x.findtext('linkclipref') for x in links})!=expected:raise ValueError("missing/duplicate linked channel")
        for link in links:
            target=link.findtext("linkclipref")
            if target not in seen:raise ValueError("broken AV link")
            expected_location=(locations[ident][0],link.findtext("mediatype"),int(link.findtext("trackindex")),int(link.findtext("clipindex")))
            if locations[target]!=expected_location:raise ValueError("link track/clip index mismatch")
            if any(clip.findtext(k)!=seen[target].findtext(k) for k in ("start","end","in","out")):raise ValueError("linked timing mismatch")
    if {p["recording"] for p in placements}!=set(range(len(records))):raise ValueError("missing original recording")
    for i,rec in enumerate(records):
        parts=sorted((p for p in placements if p["recording"]==i),key=lambda p:p["segment"])
        if not any(p["derivative"] for p in parts):
            cursor=0
            for p in parts:
                if p["source_in"]!=cursor:raise ValueError("source frame gap or duplication")
                cursor=p["source_out"]
            if cursor!=source_frames(rec,Fraction(parts[0]["source_fps"])):raise ValueError("source frames omitted")
    synced=[p for p in placements if not p["status"].startswith("unresolved")];unsynced=[p for p in placements if p["status"].startswith("unresolved")]
    if policy=="append" and synced and unsynced and min(p["start"] for p in unsynced)<max(p["end"] for p in synced):raise ValueError("unresolved clips overlap synchronized section")
    if policy=="separate-sequence" and any(p["sequence"]!="unresolved" for p in unsynced):raise ValueError("unresolved sequence policy violated")
    result={"status":"PASS_STRUCTURAL_ONLY","recordings":len(records),"source_master_clips":len(masters),"video_clipitems":counts["video"],"audio_clipitems":counts["audio"],"tracks":dict(tracks),"all_source_frames_accounted":True,"source_path_identity_verified":True,"linked_timing_consistent":True,"link_track_and_channel_indices_verified":True,"no_within_track_collisions":True,"unresolved_policy_verified":policy,"premiere_import_test":"not_performed","acoustic_accuracy":"requires_final_comparison"}
    save_json(Path(path).parent/"structural-audit.json",result)
    return result


def audit_roundtrip(original_path,imported_path,output_path):
    """Check what the editor actually imported, including source addresses."""
    def snapshot(path):
        root=ET.parse(path).getroot()
        definitions={f.get('id'):f for f in root.findall('.//file') if f.find('pathurl') is not None}
        sequences={}
        for seq in root.findall('.//sequence'):
            items=[];seq_rate=float(read_rate(seq))
            for kind in ('video','audio'):
                for clip in seq.findall(f'./media/{kind}/track/clipitem'):
                    file=clip.find('file')
                    if file is None:continue
                    definition=definitions.get(file.get('id'),file)
                    identity=unquote(urlparse(definition.findtext('pathurl','')).path).replace('\\','/').casefold()
                    rate=float(read_rate(clip)) if clip.find('rate') is not None else float(read_rate(definition))
                    items.append(dict(name=clip.findtext('name'),kind=kind,source=identity,rate=rate,
                                      start=int(clip.findtext('start'))/seq_rate,end=int(clip.findtext('end'))/seq_rate,
                                      source_in=int(clip.findtext('in'))/rate,source_out=int(clip.findtext('out'))/rate))
            sequences[seq.findtext('name')]=items
        return sequences
    expected=snapshot(original_path);actual=snapshot(imported_path);errors=[];differences=[];checked=0
    for name,items in actual.items():
        if name not in expected:errors.append(dict(sequence=name,reason='unexpected_sequence'));continue
        remaining=list(expected[name])
        for item in items:
            candidates=[(index,other) for index,other in enumerate(remaining) if item['kind']==other['kind'] and item['source']==other['source']]
            if not candidates:
                errors.append(dict(clip=item['name'],reason='unexpected_or_relinked_source'));continue
            index,other=min(candidates,key=lambda p:abs(item['start']-p[1]['start'])+abs(item['end']-p[1]['end']))
            remaining.pop(index);checked+=1
            delta={key:item[key]-other[key] for key in ('start','end','source_in','source_out')}
            if any(abs(v)>1e-8 for v in delta.values()):differences.append(dict(clip=item['name'],kind=item['kind'],delta_seconds=delta))
            tolerance=1/min(item['rate'],other['rate'])+1e-6
            if item['source_in']<0 or any(abs(v)>tolerance for v in delta.values()):
                errors.append(dict(clip=item['name'],kind=item['kind'],reason='source_or_timeline_range_changed',expected=other,imported=item,delta_seconds=delta))
        errors.extend(dict(clip=item['name'],kind=item['kind'],reason='missing_timeline_item') for item in remaining)
    if not actual:errors.append(dict(reason='no_imported_sequences'))
    report=dict(status='FAIL_IMPORT_ROUNDTRIP' if errors else 'PASS_CHECKED_SEQUENCES',checked_clipitems=checked,
                checked_sequences=list(actual),unchecked_sequences=[s for s in expected if s not in actual],
                errors=errors,frame_rounding_differences=differences,acoustic_accuracy='not_assessed_by_this_import_check')
    save_json(output_path,report)
    print(f"IMPORT AUDIT {report['status']} — {checked} items, {len(errors)} errors",flush=True)
    return report
