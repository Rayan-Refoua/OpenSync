"""Source timecode is a media address, never acoustic synchronization truth."""
from datetime import datetime
from fractions import Fraction
from functools import lru_cache
import hashlib
from pathlib import Path
import re
import xml.etree.ElementTree as ET


def frame_count(hours,minutes,seconds,frames,nominal,drop=False):
    if not (0<=hours<24 and 0<=minutes<60 and 0<=seconds<60 and 0<=frames<nominal):raise ValueError('invalid source timecode')
    skipped=round(nominal*.0666666666667) if drop else 0
    if drop and (nominal not in (30,60) or (seconds==0 and minutes%10 and frames<skipped)):raise ValueError('invalid drop-frame timecode')
    total_minutes=hours*60+minutes
    return ((hours*60+minutes)*60+seconds)*nominal+frames-skipped*(total_minutes-total_minutes//10)


def parse_string(text,rate):
    match=re.fullmatch(r'(\d{2}):(\d{2}):(\d{2})([:;])(\d{2})',text.strip())
    if not match:raise ValueError('unsupported source timecode text')
    h,m,s,separator,f=match.groups();drop=separator==';';nominal=round(float(Fraction(rate)))
    return dict(frame=frame_count(int(h),int(m),int(s),int(f),nominal,drop),string=text.strip(),rate=str(rate),displayformat='DF' if drop else 'NDF')


def decode_sony_ltc(encoded,tc_fps,half_step,rate):
    """Sony packs BCD FF SS MM HH; HFR field parity occupies the hour flag."""
    if not re.fullmatch(r'[0-9A-Fa-f]{8}',encoded):raise ValueError('invalid packed LTC')
    raw=bytes.fromhex(encoded)
    def bcd(n):
        if n%16>9 or n//16>9:raise ValueError('invalid BCD timecode')
        return n//16*10+n%16
    f,s,m,h=[bcd(v&mask) for v,mask in zip(raw,(0x3f,0x7f,0x7f,0x3f))]
    nominal=round(float(Fraction(rate)));multiplier=2 if half_step else 1
    if tc_fps*multiplier!=nominal:raise ValueError('timecode/video rate disagreement')
    f=f*multiplier+(int(bool(raw[3]&0x80)) if half_step else 0)
    drop=bool(raw[0]&0x40)
    text=f'{h:02}:{m:02}:{s:02}{";" if drop else ":"}{f:02}'
    return dict(frame=frame_count(h,m,s,f,nominal,drop),string=text,rate=str(rate),displayformat='DF' if drop else 'NDF')


def local_name(tag):return tag.rsplit('}',1)[-1]


def read_sony(path,rate):
    raw=path.read_bytes();root=ET.fromstring(raw)
    if local_name(root.tag)!='NonRealTimeMeta':return None
    elements={local_name(e.tag):e for e in root.iter()}
    table=elements.get('LtcChangeTable');duration=elements.get('Duration')
    if table is None or duration is None:return None
    entries=[e for e in table if local_name(e.tag)=='LtcChange']
    first=next((e for e in entries if e.get('frameCount')=='0'),None)
    if first is None:return None
    tc_fps=int(table.get('tcFps'));half=table.get('halfStep','false').lower()=='true'
    result=decode_sony_ltc(first.get('value',''),tc_fps,half,rate)
    count=int(duration.get('value'));last=next((e for e in entries if e.get('frameCount')==str(count-1)),None)
    if last is None:return None
    end=decode_sony_ltc(last.get('value',''),tc_fps,half,rate)
    nominal=round(float(Fraction(rate)));day=nominal*86400
    if result['displayformat']=='DF':day-=round(nominal*.0666666666667)*(1440-144)
    if (end['frame']-result['frame'])%day!=count-1:raise ValueError('camera timecode does not cover source frame count')
    creation=elements.get('CreationDate');video=elements.get('VideoFrame')
    if video is not None:
        declared=video.get('formatFps','').rstrip('pi')
        if declared and abs(float(declared)-float(Fraction(rate)))>.02:raise ValueError('sidecar source frame rate mismatch')
    result.update(kind='sony_nrt_sidecar',source=str(path),source_sha256=hashlib.sha256(raw).hexdigest(),source_frames=count,
                  creation_time=creation.get('value') if creation is not None else None,end_frame_verified=True)
    return result


@lru_cache(maxsize=128)
def directory_sidecars(directory,stamp):
    return tuple(Path(directory).glob('*M01.XML'))


def source_timecode(rec):
    video=rec.get('video') or {};rate=video.get('avg_frame_rate') or video.get('r_frame_rate') or '50/1'
    for stream in (video,rec.get('audio') or {}):
        text=(stream.get('tags') or {}).get('timecode')
        if text:
            try:return dict(parse_string(text,rate),kind='container_timecode')
            except ValueError:pass
    if not video:return None
    path=Path(rec['source']);expected_frames=video.get('nb_frames')
    preferred=[path.with_name(path.stem+'M01.XML'),path.with_suffix('.XML')]
    for sidecar in preferred:
        if not sidecar.is_file():continue
        try:
            result=read_sony(sidecar,rate)
            if result and (not expected_frames or result['source_frames']==int(expected_frames)):return result
        except (ValueError,ET.ParseError,OSError,TypeError):continue
    # Renamed clips retain their camera sidecars. Match only a unique exact
    # creation instant AND frame count AND format, never a filename sequence.
    creation=(video.get('tags') or {}).get('creation_time')
    if not creation or not expected_frames or not path.parent.is_dir():return None
    matches=[]
    for sidecar in directory_sidecars(str(path.parent),path.parent.stat().st_mtime_ns):
        try:
            result=read_sony(sidecar,rate)
            if result and result['source_frames']==int(expected_frames) and result['creation_time'] and datetime.fromisoformat(result['creation_time'].replace('Z','+00:00'))==datetime.fromisoformat(creation.replace('Z','+00:00')):matches.append(result)
        except (ValueError,ET.ParseError,OSError,TypeError):continue
    return matches[0] if len(matches)==1 else None
