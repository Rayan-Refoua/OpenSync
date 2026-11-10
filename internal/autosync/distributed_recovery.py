"""Bounded native discovery escalation; never a second acceptance pipeline."""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import subprocess

VERSION='distributed-spectral-v1'


def schedule(records,layout,envelope_pairs,seconds_budget=3600,pair_budget=8):
    """Spend a deterministic per-query budget on unresolved short recordings.

    Coarse evidence orders partners, never supplies an offset to native search.
    Partners exceeding the remaining budget are deferred, not rejected.
    """
    targets={m['recording'] for m in layout if m['status'].startswith('unresolved') and 1.2<=records[m['recording']]['duration']<=60}
    strength={(p['reference'],p['query']):max((c.get('envelope_score',0) for c in p['candidates']),default=0) for p in envelope_pairs}
    jobs=[];deferred=[]
    for q in sorted(targets):
        refs=[r for r in range(len(records)) if r!=q and records[r]['duration']>=records[q]['duration']]
        refs.sort(key=lambda r:(-strength.get((r,q),0),records[r]['id']))
        used=0.;selected=0
        for r in refs:
            if selected>=pair_budget or used+records[r]['duration']>seconds_budget:
                deferred.append(dict(reference=r,query=q,reason='discovery_compute_budget'));continue
            jobs.append(dict(reference=r,query=q));used+=records[r]['duration'];selected+=1
    return jobs,deferred


def run(api,records,layout,envelope_pairs,out):
    executable=os.environ.get('OPENSYNC_NATIVE_EXECUTABLE')
    if not executable:return None
    identity=hashlib.sha256(json.dumps([VERSION,[(r['id'],r['cache_sha256']) for r in records]],sort_keys=True).encode()).hexdigest()
    saved_path=out/'distributed-discovery.json'
    if saved_path.exists():
        saved=json.loads(saved_path.read_text(encoding='utf-8'))
        if saved.get('input_digest')!=identity:raise ValueError('stale distributed discovery')
        return saved
    folder=out/VERSION;folder.mkdir(exist_ok=True)
    jobs,deferred=schedule(records,layout,envelope_pairs)
    paths={}
    for i in sorted({p[k] for p in jobs for k in ('reference','query')}):
        rec=records[i];path=folder/(rec['cache_sha256']+'.features')
        if not path.exists():
            subprocess.run([executable,'distributed','features','--input',rec['cache'],'--channels','1','--output',str(path)],check=True)
        paths[i]=path
    def search(pair):
        r,q=pair['reference'],pair['query']
        identity=hashlib.sha256((VERSION+records[r]['cache_sha256']+records[q]['cache_sha256']).encode()).hexdigest()
        path=folder/(identity+'.json')
        if not path.exists():subprocess.run([executable,'distributed','search','--reference',str(paths[r]),'--query',str(paths[q]),'--output',str(path)],check=True)
        result=json.loads(path.read_text(encoding='utf-8'))
        return dict(pair,**result,input_digest=identity)
    results=[]
    with ThreadPoolExecutor(max_workers=2) as pool:
        for i,result in enumerate(pool.map(search,jobs)):
            results.append(result)
            if (i+1)%10==0:print(f'DISTRIBUTED {i+1}/{len(jobs)}',flush=True)
    report=dict(version=VERSION,input_digest=identity,pairs=results,deferred=deferred,reference_seconds_budget_per_query=3600,
                pair_budget_per_query=8,fine_verification_candidates_per_pair=3,
                original_media_read=False,channel_policy='cached_mono_first_pass',
                status='DISCOVERY_ONLY',baseline_positions_used=False)
    api.save(out/'distributed-discovery.json',report)
    return report
