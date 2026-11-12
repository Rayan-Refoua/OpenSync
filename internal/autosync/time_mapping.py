"""Monotonic source-to-timeline mappings; video presentation is always 1x."""
import bisect
import math


def validate(points):
    if len(points)<2 or any(len(p)!=2 or not all(math.isfinite(v) for v in p) for p in points):
        raise ValueError('invalid timing knots')
    if any(b[0]<=a[0] or b[1]<=a[1] for a,b in zip(points,points[1:])):
        raise ValueError('timing map must be strictly monotonic')
    return points


def value(points,t):
    index=max(0,min(len(points)-2,bisect.bisect_right([p[0] for p in points],t)-1))
    a,b=points[index:index+2]
    return a[1]+(t-a[0])*(b[1]-a[1])/(b[0]-a[0])


def inverse(points):
    return [[b,a] for a,b in points]


def compose(parent,relation,duration):
    """relation maps child source to parent source; retain both sets of bends."""
    times={0.,float(duration)}
    times.update(t for t,_ in relation if 0<t<duration)
    inv=inverse(relation)
    times.update(value(inv,t) for t,_ in parent if relation[0][1]<t<relation[-1][1] and 0<value(inv,t)<duration)
    return validate([[t,value(parent,value(relation,t))] for t in sorted(times)])


def video_segments(points,total,fps,budget):
    """Partition all source frames; bound 1x playback error within each part.

    A varying offset changes cut positions, never the source playback rate.
    Every bend and boundary is checked because extrema of a linear interval
    occur at its endpoints. Frame rounding is budgeted by the exporter.
    """
    validate(points)
    if budget<=0:raise ValueError('no timing budget available for video')
    pending=[(0,total)];parts=[]
    while pending:
        first,last=pending.pop();a,b=first/fps,last/fps
        times=[a,b]+[t for t,_ in points if a<t<b]
        offsets=[value(points,t)-t for t in times]
        center=(min(offsets)+max(offsets))/2
        error=(max(offsets)-min(offsets))/2
        if error<=budget:
            parts.append((first,last,center,error))
        elif last-first<=1:
            raise ValueError('timing change cannot be represented without retiming a video frame')
        else:
            middle=(first+last)//2
            pending.extend([(middle,last),(first,middle)])
    return sorted(parts)
