"""Relative acoustic relationships, explicitly separate from export permission.

An offset trend can be clock skew, moving geometry, or changing PA paths.
This diagnostic describes measured arrival timing without pretending to
identify a camera's physical position or to calibrate propagation delay.
"""
import numpy as np


def delay_profile(hypothesis):
    observations=[]
    for role in ('training','selection','audit'):
        for o in hypothesis.get(role,[]):
            # Keep the same signal/control gate as the timing verifier.
            # Indistinct peaks remain visible but cannot establish a trend.
            if o.get('supported'):
                observations.append(dict(query_time=o['query_time'],arrival_offset=o['offset'],
                                         precise=o.get('precise',False),role=role))
    observations.sort(key=lambda o:(o['query_time'],o['role']))
    precise=[o for o in observations if o['precise']]
    result=dict(observations=observations,precise_observations=len(precise),
                interpretation='insufficient_precise_timing',physical_cause='not_identifiable',
                permits_automatic_placement=False,permits_video_retiming=False)
    if observations:
        offsets=[o['arrival_offset'] for o in observations]
        result['observed_offset_range_seconds']=[min(offsets),max(offsets)]
    # Repeated observations of the same region are not extra witnesses.
    by_time={o['query_time']:o['arrival_offset'] for o in precise}
    if len(by_time)<4:return result
    t=np.array(sorted(by_time));y=np.array([by_time[x] for x in t]);span=float(np.ptp(t))
    if span<2:return result
    slopes=[(y[j]-y[i])/(t[j]-t[i]) for i in range(len(t)) for j in range(i+1,len(t))]
    slope=float(np.median(slopes));intercept=float(np.median(y-slope*t))
    error=float(np.max(np.abs(y-(intercept+slope*t))));spread=float(np.ptp(y))
    result.update(observed_span_seconds=span,precise_offset_range_seconds=[float(min(y)),float(max(y))],
                  apparent_rate_ppm=slope*1e6,linear_residual_seconds=error)
    if spread<=.025:result['interpretation']='stable_acoustic_offset'
    elif error<=.025:result['interpretation']='smooth_arrival_trend_clock_or_motion'
    else:result['interpretation']='changing_paths_or_conflicting_measurements'
    return result


def relationship_map(records,profiles,graph):
    """Map recording relationships, without inventing physical camera IDs."""
    relations=[]
    for pair in profiles:
        hypotheses=[]
        for h in pair['profiles']:
            hypotheses.append(dict(candidate_offset=h['prior']['offset'],verification_status=h['status'],
                                   relationship_status='timing_supported' if h['status']=='validated_relation'
                                   else 'event_supported_timing_unresolved' if h['status'] in ('corroboration_required','event_supported_timing_unresolved')
                                   else 'candidate_same_event_not_confirmed',delay_profile=delay_profile(h)))
        relations.append(dict(reference=pair['reference'],query=pair['query'],hypotheses=hypotheses))
    return dict(version='acoustic-relationships-v1',coordinate_system='relative_acoustic_arrival_seconds',
                physical_camera_positions='not_established',camera_identity='recordings_not_assumed_to_be_distinct_devices',
                placement_authority='validated_timing_graph_only',
                nodes=[dict(recording=i,id=r['id'],name=r['name'],kind='camera_recording' if r.get('video') else 'audio_recording') for i,r in enumerate(records)],
                relations=relations,graph_conflicts=graph.get('rejected_edges',[]))
