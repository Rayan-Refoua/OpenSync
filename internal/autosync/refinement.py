"""General multilevel relation refinement and cross-reference ambiguity checks.

Discovery hypotheses are not timeline placements. Models are learned on one
set of audio windows, selected on disjoint windows, and checked again on a
third set. Uncertainty survives into the saved project.
"""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import math
from pathlib import Path
import numpy as np
from scipy import fft

PROFILE_VERSION="multilevel-profile-v7"


def phase_peak(reference,query):
    """Whitened cross-spectrum vote, independent of amplitude correlation."""
    if len(query)<64 or len(reference)<len(query):return None
    r=np.asarray(reference,dtype=float);q=np.asarray(query,dtype=float)
    r=r-r.mean();q=q-q.mean()
    if np.std(r)<1e-7 or np.std(q)<1e-7:return None
    size=fft.next_fast_len(len(r)+len(q)-1)
    cross=fft.rfft(r,size)*np.conj(fft.rfft(q,size))
    magnitude=np.abs(cross)
    weighted=cross/np.maximum(magnitude,np.max(magnitude)*1e-6)**.8
    values=np.abs(fft.irfft(weighted,size)[:len(r)-len(q)+1])
    best=int(np.argmax(values));peak=float(values[best])
    others=np.r_[values[:max(0,best-8)],values[best+9:]]
    competitor=float(np.max(others,initial=0))
    prominence=peak/max(competitor,1e-20)
    contrast=peak/max(float(np.sqrt(np.mean(values**2))),1e-20)
    return {'lag':best,'prominence':prominence,'contrast':contrast,
            'distinct':prominence>=1.2 and contrast>=5}


def phase_check(api,ref,query,start,offset,length,radius):
    left=max(0,start+offset-radius);right=min(ref['duration'],start+offset+length+radius)
    if right-left<length:return None
    def prep(x):
        n=len(x)//8
        return np.diff(x[:n*8].reshape(n,8).mean(axis=1))
    found=phase_peak(prep(api.read(ref['cache'],left,right-left)),prep(api.read(query['cache'],start,length)))
    if found is None:return None
    found['offset']=left-start+found.pop('lag')*.0005
    return found


def predict(model,t):
    if model['kind']=='piecewise_linear':
        knots=model['knots'];x=np.array([p[0] for p in knots]);y=np.array([p[1] for p in knots])
        return np.interp(t,x,y)
    return model['offset']+model.get('slope',0)*np.asarray(t)


def fit_models(observations):
    good=sorted(observations,key=lambda o:o['query_time'])
    t=np.array([o['query_time'] for o in good]);y=np.array([o['offset'] for o in good])
    if len(t)<2:return []
    # Minimax centres the measured range; the objective is a bounded maximum
    # synchronization error, not merely a low median error.
    models=[{'kind':'constant','offset':float((min(y)+max(y))/2),'slope':0.0}]
    if np.ptp(t)>=20 and len(t)>=3:
        slopes=[(y[j]-y[i])/(t[j]-t[i]) for i in range(len(t)) for j in range(i+1,len(t))]
        slope=float(np.median(slopes));offset=float(np.median(y-slope*t))
        if abs(slope)<=.002:models.append({'kind':'affine','offset':offset,'slope':slope})
    if len(t)>=5 and np.ptp(t)>=30:
        # No smoothing through clock steps: a piecewise model is retained
        # only if both reserved window sets independently support it.
        if np.all(np.diff(t+y)>0) and np.max(np.abs(np.diff(y)/np.diff(t)))<=.02:
            models.append({'kind':'piecewise_linear','knots':[[float(a),float(b)] for a,b in zip(t,y)]})
    return models


def judge(model,observations,limit=.025,minimum_windows=2):
    measured=[(o,abs(o['offset']-float(predict(model,o['query_time'])))) for o in observations if o['supported']]
    # An indistinct peak can corroborate a predicted location, but a different
    # indistinct peak cannot refute it. Distinct contrary evidence always can.
    residuals=[r for o,r in measured if o.get('precise',True) or r<=limit]
    return {'supported_windows':len(residuals),'ambiguous_windows':len(measured)-len(residuals),'maximum_residual_seconds':max(residuals,default=None),'median_residual_seconds':float(np.median(residuals)) if residuals else None,'passes':len(residuals)>=minimum_windows and max(residuals)<=limit}


def measure(api,ref,query,center,length,prior):
    offset=prior['offset']+prior.get('slope',0)*center
    start=center-length/2
    results=[]
    for radius in (.35,1.5):
        m=api.waveform(ref,query,start,offset,length,radius)
        if m is None:continue
        controls=[c for shift in (-31,-15,15,31) if (c:=api.waveform(ref,query,start,offset+shift,length,radius)) is not None]
        if len(controls)<2:
            # Short recordings cannot supply controls 15 seconds away. Use
            # distinct, non-overlapping local regions under the same test.
            controls += [c for shift in (-8,-4,-2,2,4,8) if abs(shift)>length+2*radius and (c:=api.waveform(ref,query,start,offset+shift,length,radius)) is not None]
        strongest=max((c['wave_correlation'] for c in controls),default=1.)
        flux=api.flux_score(ref,query,start,m['offset'],length)
        central=api.waveform(ref,query,center-length/4,m['offset'],length/2,.08)
        phase=phase_check(api,ref,query,start,offset,length,radius)
        phase_agrees=phase is not None and phase['distinct'] and abs(phase['offset']-m['offset'])<=.008
        stable=central is not None and abs(central['offset']-m['offset'])<=.008
        # A peak touching a computational boundary is re-searched more widely.
        # A real media boundary is allowed: there is no omitted audio beyond it.
        at_boundary=abs(m['offset']-offset)>=radius-.002 and start+offset-radius>0 and start+offset+length+radius<ref['duration']
        supported=not at_boundary and len(controls)>=2 and m['wave_correlation']>=.1 and m['wave_correlation']>=strongest*1.5 and m['wave_correlation']-strongest>=.035 and (flux>=.25 or m['wave_correlation']>=.65)
        precise=supported and (m['local_peak_distinct'] or phase_agrees) and (stable or phase_agrees)
        m.update(supported=bool(supported),precise=bool(precise),strongest_control=strongest,flux_correlation=flux,multiscale_consistent=bool(stable),central_offset=central['offset'] if central else None,search_radius=radius,phase=phase,phase_agrees=bool(phase_agrees),at_search_boundary=bool(at_boundary))
        results.append(m)
        if supported:break
    best=max(results,key=lambda o:(o['supported'],o['wave_correlation']-o['strongest_control'])) if results else None
    # Changing delay inside a long window smears its correlation. A shorter
    # centred measurement must pass the same controls, not a lower threshold.
    if best is not None and not best['supported'] and length>1 and best['phase_agrees'] and best['flux_correlation']>.5:
        shorter=measure(api,ref,query,center,length/2,prior)
        if shorter is not None and shorter['supported']:return shorter
    return best


def profile_candidate(api,ref,query,prior):
    a=prior['offset'];b=1+prior.get('slope',0)
    begin=max(0,-a/b)+.15;end=min(query['duration'],(ref['duration']-a)/b)-.15
    overlap=end-begin
    result={'prior':prior,'status':'insufficient_evidence','overlap_seconds':overlap}
    if overlap<1.8:return result
    count=min(129,max(3,int(overlap/10)+1))
    length=min(6,overlap/(5*(count-1)))
    if length<.2:return result
    centers=np.linspace(begin+length/2,end-length/2,count)
    schedules={'training':[(float(t),length) for t in centers],
               'selection':[(float((a+b)/2),length/2) for a,b in zip(centers[:-1],centers[1:])],
               'audit':[(float(a+(b-a)*fraction),length/2) for a,b in zip(centers[:-1],centers[1:]) for fraction in (1/3,2/3)]}
    if overlap<30:
        # Short takes often contain just a few usable bursts. Cover the full
        # overlap with interleaved disjoint roles instead of sampling only
        # the beginning, middle and end and missing those bursts entirely.
        block=overlap/12
        schedules={role:[(begin+(j+.5)*block,.9*block) for j in range(index,12,3)] for index,role in enumerate(('training','selection','audit'))}
    for role,schedule in schedules.items():
        result[role]=[m for t,n in schedule if (m:=measure(api,ref,query,t,n,prior)) is not None]
    return evaluate_profile(result)


def evaluate_profile(result):
    result={k:v for k,v in result.items() if k in ('prior','overlap_seconds','training','selection','audit')}
    result['status']='insufficient_evidence';overlap=result['overlap_seconds']
    if 'training' not in result:return result
    training=[o for o in result['training'] if o['precise']]
    required=3 if overlap>=30 else 2
    if len(training)<required:return result
    models=fit_models(training)
    selected=None
    limit=.025
    for limit in (.025,.05):
        for model in models:
            checked=judge(model,result['selection'],limit,1 if overlap<30 else 2)
            model['selection']=checked
            if checked['passes'] and judge(model,training,limit)['passes']:
                selected=model;break
        if selected is not None:break
    result['models']=models
    # A valid event relation may need a timing model the engine cannot yet
    # deliver. Preserve that distinction instead of erasing the alternative.
    result['status']='event_supported_timing_unresolved'
    if selected is None:return result
    checked=judge(selected,result['audit'],limit);result['audit_result']=checked
    if not checked['passes']:
        # A frozen model with a wider measured bound can only be admitted
        # with other reference recordings; its uncertainty is not hidden.
        checked=judge(selected,result['audit'],.05)
        if not checked['passes']:return result
        limit=.05;result['audit_result']=checked
    all_good=[o for role in ('training','selection','audit') for o in result[role] if o['supported'] and abs(o['offset']-float(predict(selected,o['query_time'])))<=limit]
    extent=max(o['query_time']+o['window']/2 for o in all_good)-min(o['query_time']-o['window']/2 for o in all_good)
    if overlap>=30 and extent<.8*overlap:return result
    strength=float(np.median([o['wave_correlation']-o['strongest_control'] for o in all_good]))
    needs_corroboration=limit>.025 or selected['selection']['supported_windows']<2 or (overlap>=12 and len(training)<3)
    result.update(status='corroboration_required' if needs_corroboration else 'validated_relation',model=selected,confidence=strength,timing_limit_seconds=limit,validated_source_range=[min(o['query_time']-o['window']/2 for o in all_good),max(o['query_time']+o['window']/2 for o in all_good)],supported_windows=len(all_good))
    return result


def candidate_reservoir(records,out):
    pairs={}
    for namespace in ('evidence-v2','evidence-recovery-v2','evidence-refined-v1'):
        for path in sorted((out/namespace).glob('*.json')):
            evidence=json.loads(path.read_text(encoding='utf-8'))
            key=(evidence['reference'],evidence['query'])
            for c in evidence['candidates']:
                good=[o for o in c.get('observations',[]) if o.get('supported')]
                required=3 if records[key[1]]['duration']>=30 else 2
                strength=float(np.median([o['wave_correlation']-o.get('strongest_control',0) for o in good])) if good else 0.
                if len(good)<required or strength<.05:
                    raw=[o for o in c.get('observations',[]) if o['wave_correlation']>=.3 and o.get('flux_correlation',0)>=.5]
                    strong_raw=len(raw)>=required and np.ptp([o['offset'] for o in raw])<.1
                    short_fingerprint=records[key[1]]['duration']<12 and c.get('votes',0)>=6
                    if not (strong_raw or short_fingerprint):continue
                    # A discovery candidate earns no corroboration vote until
                    # its new acoustic measurements establish an event.
                    strength=0.
                candidate={'offset':c['offset'],'slope':c.get('slope',0),'prior_strength':strength}
                existing=pairs.setdefault(key,[])
                close=next((i for i,p in enumerate(existing) if abs(p['offset']-candidate['offset'])<.5),None)
                if close is None:existing.append(candidate)
                elif existing[close]['prior_strength']<strength:existing[close]=candidate
    return [{'reference':r,'query':q,'candidates':sorted(c,key=lambda p:(-p['prior_strength'],p['offset']))} for (r,q),c in sorted(pairs.items())]


def run_profiles(api,records,out,workers=4):
    folder=out/PROFILE_VERSION;folder.mkdir(exist_ok=True)
    pairs=candidate_reservoir(records,out)
    def work(pair):
        ref,query=records[pair['reference']],records[pair['query']]
        identity=hashlib.sha256(json.dumps({'version':PROFILE_VERSION,'pair':pair,'sources':[ref['cache_sha256'],query['cache_sha256']]},sort_keys=True).encode()).hexdigest()
        path=folder/(ref['id']+'_'+query['id']+'.json')
        if path.exists():
            saved=json.loads(path.read_text(encoding='utf-8'))
            if saved.get('input_digest')!=identity:raise ValueError('stale multilevel profile')
            return saved
        # The long-recording sampling and controls are unchanged from v3.
        # Reuse those exact PCM measurements only when inputs also match.
        prior_path=out/'multilevel-profile-v3'/path.name
        previous=json.loads(prior_path.read_text(encoding='utf-8')) if prior_path.exists() else {}
        prior_identity=hashlib.sha256(json.dumps({'version':'multilevel-profile-v3','pair':pair,'sources':[ref['cache_sha256'],query['cache_sha256']]},sort_keys=True).encode()).hexdigest()
        profiles=[evaluate_profile(p) for p in previous['profiles']] if ref['duration']>=70 and query['duration']>=30 and previous.get('input_digest')==prior_identity else [profile_candidate(api,ref,query,p) for p in pair['candidates']]
        saved={'reference':pair['reference'],'query':pair['query'],'profiles':profiles,'input_digest':identity}
        api.save(path,saved);return saved
    results=[]
    with ThreadPoolExecutor(max_workers=workers) as pool:
        for i,result in enumerate(pool.map(work,pairs)):
            results.append(result)
            if (i+1)%10==0:print(f"MULTILEVEL {i+1}/{len(pairs)}",flush=True)
    api.save(out/'multilevel-profiles.json',{'version':PROFILE_VERSION,'pairs':results,'baseline_positions_used':False})
    return results
