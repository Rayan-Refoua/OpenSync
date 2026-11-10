"""Cross-reference event selection and validated piecewise timing propagation."""
from collections import defaultdict
import math
import heapq
import numpy as np
from time_mapping import compose,inverse,value


def observations(edge):
    selected=edge['selected']
    return selected['fit_observations']+[o for o in edge.get('validation',{}).get('observations',[]) if o.get('passes')]


def relation_points(edge,records):
    m=edge['selected'];duration=records[edge['query']]['duration']
    if m.get('timing_knots'):return m['timing_knots']
    return [[0,m['offset']],[duration,m['offset']+(1+m['slope'])*duration]]


def reject_excerpt_bridges(records,edges):
    """Do not join long recordings solely through one short shared excerpt.

    A song/sample can occur in otherwise unrelated sessions. A short take
    mostly covered by the evidence is different from a tiny fragment of two
    long takes. Weak excerpts can check an already established relationship,
    but cannot establish a new event connection on their own.
    """
    strong=[];weak=[];parents=list(range(len(records)))
    def find(n):
        while parents[n]!=n:parents[n]=parents[parents[n]];n=parents[n]
        return n
    for edge in edges:
        obs=observations(edge)
        span=max(o['query_time']+o.get('window',0)/2 for o in obs)-min(o['query_time']-o.get('window',0)/2 for o in obs)
        shorter=min(records[edge['reference']]['duration'],records[edge['query']]['duration'])
        if shorter>=120 and span<30 and span<.1*shorter:weak.append((edge,span))
        else:
            strong.append(edge);parents[find(edge['query'])]=find(edge['reference'])
    rejected=[]
    for edge,span in weak:
        if find(edge['reference'])==find(edge['query']):strong.append(edge)
        else:rejected.append(dict(reference=edge['reference'],query=edge['query'],status='unresolved_short_excerpt_bridge',supported_span_seconds=span))
    return strong,rejected


def edge_from_profile(pair,p,records):
    model=p['model'];duration=records[pair['query']]['duration']
    if model['kind']=='piecewise_linear':
        offsets=model['knots']
        points=[[0,offsets[0][1]]]+[[t,t+o] for t,o in offsets]+[[duration,duration+offsets[-1][1]]]
    else:points=[[0,model['offset']],[duration,model['offset']+(1+model.get('slope',0))*duration]]
    train=[o for o in p['training'] if o.get('precise',o['supported'])]
    checks=[dict(o,passes=True) for role in ('selection','audit') for o in p[role] if o['supported'] and (o.get('precise',True) or abs(value(points,o['query_time'])-o['query_time']-o['offset'])<=p.get('timing_limit_seconds',.025))]
    error=max(abs(value(points,o['query_time'])-o['query_time']-o['offset']) for o in train+checks)
    selected=dict(offset=points[0][1],slope=(points[-1][1]-points[0][1])/duration-1,
                  timing_knots=points,confidence=p['confidence']*math.sqrt(len(train)),
                  residual_seconds=error,fit_observations=train,observed_span_seconds=p['validated_source_range'][1]-p['validated_source_range'][0])
    return dict(reference=pair['reference'],query=pair['query'],selected=selected,
                validation=dict(observations=checks),multilevel_profile=p)


def select_edges(api,records,base_edges,profiles):
    """Use longer relations as context, never a short candidate to prove itself.

    All previously supported hypotheses remain counter-evidence even if their
    fine timing is unresolved. More reference recordings must agree before
    choosing between separate occurrences of repeated programme audio.
    """
    base_edges,excerpt_rejections=reject_excerpt_bridges(records,base_edges)
    anchor_pairs={(e['reference'],e['query']):e for e in base_edges if min(records[e['query']]['duration'],records[e['reference']]['duration'])>=30}
    for pair in profiles:
        valid=[p for p in pair['profiles'] if p['status']=='validated_relation']
        if len(valid)==1 and min(records[pair['query']]['duration'],records[pair['reference']]['duration'])>=30:
            anchor_pairs[(pair['reference'],pair['query'])]=edge_from_profile(pair,valid[0],records)
    anchors=list(anchor_pairs.values())
    # The old affine solver is used only for event neighbourhoods here.
    # Preserve the established affine anchor if a new relation is nonlinear.
    anchors=[e for e in anchors if len(e['selected'].get('timing_knots',[]))<=2]
    anchors,_=reject_excerpt_bridges(records,anchors)
    layout,_=api.graph_layout(records,anchors)
    by_id={x['recording']:x for x in layout}
    contexts={}
    for q in {p['query'] for p in profiles}:
        if records[q]['duration']>=30 and sum(len(p['profiles']) for p in profiles if p['query']==q)>1:
            independent=[e for e in anchors if q not in (e['reference'],e['query'])]
            context,_=api.graph_layout(records,independent)
            contexts[q]={x['recording']:x for x in context}
    candidates=defaultdict(list)
    for pair in profiles:
        q=pair['query'];ref=pair['reference'];anchor=contexts.get(q,by_id)[ref]
        if anchor['component_size']<2:continue
        for p in pair['profiles']:
            if p['prior']['prior_strength']==0 and p['status'] not in ('validated_relation','corroboration_required','event_supported_timing_unresolved'):continue
            # The reservoir contains repeated supported acoustic windows.
            prior=p['prior'];mid=records[q]['duration']/2
            offset=prior['offset']+prior.get('slope',0)*mid
            position=anchor['start_seconds']+anchor['rate']*(mid+offset)-mid
            candidates[q].append(dict(reference=ref,component=anchor['component'],position=position,profile=p))
    decisions={};report=[]
    for q,entries in sorted(candidates.items()):
        groups=[]
        for entry in sorted(entries,key=lambda x:(x['component'],x['position'],x['reference'])):
            if not groups or groups[-1][0]['component']!=entry['component'] or entry['position']-groups[-1][0]['position']>.15:groups.append([])
            groups[-1].append(entry)
        if len(groups)<2:continue
        groups.sort(key=lambda g:(-len({x['reference'] for x in g}),-max(x['profile']['prior']['prior_strength'] for x in g)))
        counts=[len({x['reference'] for x in g}) for g in groups]
        winner=groups[0] if counts[0]>=2 and counts[0]>counts[1] else None
        if winner is None:
            def extent(group):
                spans=[]
                for x in group:
                    ts=[o['query_time'] for role in ('training','selection','audit') for o in x['profile'].get(role,[]) if o['supported']]
                    spans.append(max(ts)-min(ts) if len(ts)>1 else 0.)
                return max(spans,default=0.)
            ranked=sorted(groups,key=extent,reverse=True)
            # A small repeated excerpt cannot overrule minutes of supported
            # matching elsewhere in the same recording.
            if extent(ranked[0])>=60 and extent(ranked[0])>=3*max(extent(ranked[1]),1):winner=ranked[0]
        decisions[q]=winner
        report.append(dict(recording=q,status='corroborated_alternative' if winner else 'unresolved_competing_events',
                           alternatives=[dict(component=g[0]['component'],position_seconds=float(np.median([x['position'] for x in g])),references=sorted({x['reference'] for x in g})) for g in groups]))

    def permitted(edge):
        q=edge['query']
        if q not in decisions:return True
        winner=decisions[q]
        if winner is None:return False
        anchor=contexts.get(q,by_id)[edge['reference']];mid=records[q]['duration']/2
        position=anchor['start_seconds']+anchor['rate']*value(relation_points(edge,records),mid)-mid
        return anchor['component']==winner[0]['component'] and abs(position-float(np.median([x['position'] for x in winner])))<=.15

    edges={(e['reference'],e['query']):e for e in base_edges if permitted(e)}
    for pair in profiles:
        def corroborated(p):
            entries=candidates.get(pair['query'],[])
            own=next((x for x in entries if x['reference']==pair['reference'] and x['profile'] is p),None)
            return own is not None and len({x['reference'] for x in entries if x['component']==own['component'] and abs(x['position']-own['position'])<=.15})>=2
        valid=[p for p in pair['profiles'] if p['status']=='validated_relation' or (p['status']=='corroboration_required' and corroborated(p))]
        allowed=[e for p in valid if permitted(e:=edge_from_profile(pair,p,records))]
        # Never discard an alternative merely because another scored higher.
        if len(allowed)==1:edges[(pair['reference'],pair['query'])]=allowed[0]
        elif len(allowed)>1:edges.pop((pair['reference'],pair['query']),None)
    # An ambiguous clip cannot remain connected by a reverse-oriented edge.
    blocked={q for q,winner in decisions.items() if winner is None}
    selected=[e for _,e in sorted(edges.items()) if not blocked.intersection((e['reference'],e['query']))]
    selected,more_rejections=reject_excerpt_bridges(records,selected)
    unique={(e['reference'],e['query']):e for e in excerpt_rejections+more_rejections}
    return selected,report+list(unique.values())


def solve(records,edges):
    active=set(range(len(edges)));rejected=[]
    def tree():
        available=defaultdict(list);adjacency=defaultdict(list);chosen=set()
        for k in sorted(active):
            e=edges[k];available[e['reference']].append((e['query'],k));available[e['query']].append((e['reference'],k))
        maps={};groups=[];roots=[];errors={}
        # The common clock is a video clock whenever one is available.
        for root in sorted(range(len(records)),key=lambda i:(not bool(records[i].get('video')),-records[i]['duration'],records[i]['id'])):
            if root in maps:continue
            group=[];queue=[(0.,root,-1,-1)];settled=set()
            while queue:
                error,node,parent,k=heapq.heappop(queue)
                if node in settled:continue
                settled.add(node);group.append(node);errors[node]=error
                if parent<0:maps[node]=[[0.,0.],[records[node]['duration'],records[node]['duration']]]
                else:
                    relation=relation_points(edges[k],records)
                    if parent==edges[k]['query']:relation=inverse(relation)
                    maps[node]=compose(maps[parent],relation,records[node]['duration'])
                    chosen.add(k);adjacency[parent].append((node,k));adjacency[node].append((parent,k))
                for other,j in available[node]:
                    if other not in settled:
                        heapq.heappush(queue,(error+max(.001,edges[j]['selected']['residual_seconds']),other,node,j))
            groups.append(sorted(group));roots.append(root)
        return adjacency,chosen,maps,groups,roots,errors
    while True:
        adjacency,chosen,maps,groups,roots,errors=tree();conflicts=[];checks=[]
        for k in sorted(active):
            e=edges[k];residual=max(abs(value(maps[e['reference']],o['query_time']+o['offset'])-value(maps[e['query']],o['query_time'])) for o in observations(e))
            checks.append(dict(edge=k,maximum_residual_seconds=residual,observations=len(observations(e))))
            if residual>.05:conflicts.append((residual,k))
        if not conflicts:break
        residual,k=max(conflicts);e=edges[k];queue=[(e['reference'],[])];seen={e['reference']};route=[]
        for node,path in queue:
            if node==e['query']:route=path;break
            for other,j in adjacency[node]:
                if other not in seen:seen.add(other);queue.append((other,path+[j]))
        weakest=min(set(route+[k]),key=lambda j:(edges[j]['selected']['confidence'],-j))
        active.remove(weakest);rejected.append(dict(edge=weakest,reason='inconsistent_timing_path',maximum_residual_seconds=residual))
    groups.sort(key=lambda g:(-len(g),-sum(records[i]['duration'] for i in g),g[0]));layout=[]
    for component,group in enumerate(groups,1):
        origin=min(value(maps[i],0) for i in group)
        for i in group:
            points=[[t,v-origin] for t,v in maps[i]];rate=(points[-1][1]-points[0][1])/records[i]['duration']
            status='acoustically_supported_not_certified' if component==1 and len(group)>1 else 'unresolved_disconnected_group' if len(group)>1 else 'unresolved_no_supported_relation'
            layout.append(dict(recording=i,component=component,component_size=len(group),start_seconds=points[0][1],rate=rate,timing_knots=points,
                               fit_residual_seconds=max(.001,errors[i]),status=status))
    return layout,dict(components=groups,active_edges=sorted(active),rejected_edges=rejected,reference_recordings=roots,
                       global_evidence_residuals=checks,maximum_global_residual_seconds=max((c['maximum_residual_seconds'] for c in checks),default=0),
                       independent_certification='not_performed',clock_policy='prefer_video_reference',timing_model='monotonic_piecewise')


def comparison_placements(records,layout,profiles,discovery):
    """Bounded review proposals, never anchors or certified graph edges.

    Require full-reference spectral competition plus repeated waveform/control
    support in all three temporal roles. The relaxed part is local peak
    precision, not permission to accept a single coincidental excerpt.
    """
    models={m['recording']:m for m in layout}
    searches={(p['reference'],p['query']):p for p in discovery.get('pairs',[])}
    choices=defaultdict(list)
    for pair in profiles:
        r,q=pair['reference'],pair['query'];ref=models[r];query=models[q]
        if not query['status'].startswith('unresolved') or ref['status'].startswith('unresolved'):continue
        peaks=searches.get((r,q),{}).get('candidates',[])
        if not peaks:continue
        top=peaks[0]
        if top['background_z']<5 or (len(peaks)>1 and top['score']<1.15*peaks[1]['score']):continue
        for h in pair['profiles']:
            if abs(h['prior']['offset']-top['offset'])>.1:continue
            def usable(o):
                return (not o.get('at_search_boundary') and o['wave_correlation']>=.1
                        and o['wave_correlation']>=1.5*o['strongest_control']
                        and o['wave_correlation']-o['strongest_control']>=.035)
            train=[o for o in h.get('training',[]) if usable(o)]
            if len(train)<3:continue
            offset=float(np.median([o['offset'] for o in train]))
            if abs(offset-top['offset'])>.12:continue
            matched=[];counts={};failed=False
            for role in ('training','selection','audit'):
                observations=h.get(role,[]);good=[o for o in observations if usable(o)]
                inside=[o for o in good if abs(o['offset']-offset)<=.12]
                if len(inside)<(3 if role=='training' else 2) or len(inside)<math.ceil(.6*len(good)):failed=True
                if any(o.get('precise') and abs(o['offset']-offset)>.12 for o in observations):failed=True
                matched+=inside;counts[role]=len(inside)
            if failed:continue
            span=max(o['query_time']+o['window']/2 for o in matched)-min(o['query_time']-o['window']/2 for o in matched)
            if span<.6*records[q]['duration']:continue
            duration=records[q]['duration'];mid=duration/2;points=ref['timing_knots']
            start=value(points,offset+mid)-mid
            residual=max(abs(o['offset']-offset) for o in matched)
            reference_variation=max(abs(value(points,offset+t)-(start+t)) for t in (0,mid,duration))
            uncertainty=residual+ref['fit_residual_seconds']+reference_variation+.01
            # Leave frame rounding room under the existing 150ms policy.
            if uncertainty>.14:continue
            choices[q].append(dict(recording=q,reference=r,start_seconds=start,rate=1.,
                                   timing_knots=[[0.,start],[duration,start+duration]],
                                   fit_residual_seconds=uncertainty,status='provisional_acoustic_review_required',
                                   matched_windows=counts,measured_window_residual_seconds=residual,
                                   proposed_pair_offset_seconds=offset,discovery_offset_seconds=top['offset'],
                                   excluded_waveform_windows=sum(len(h.get(role,[]))-counts[role] for role in counts),
                                   physical_capture_alignment='not_independently_established'))
    result=[]
    for q,options in sorted(choices.items()):
        # Competing event locations remain unresolved. Proposals cannot prove
        # one another, or become references for a second propagation pass.
        if max(o['start_seconds'] for o in options)-min(o['start_seconds'] for o in options)>.15:continue
        result.append(min(options,key=lambda o:(o['fit_residual_seconds'],o['reference'])))
    return result
