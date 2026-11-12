import sys
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
import wave
import numpy as np

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'internal'/'autosync'))
import pipeline
import relation_graph
import project_export
from time_mapping import compose,value,video_segments,validate


def edge(ref,q,offset,confidence=1.):
    obs=[dict(query_time=t,offset=offset,window=1.,supported=True) for t in (1.,20.,40.)]
    return dict(reference=ref,query=q,selected=dict(offset=offset,slope=0.,confidence=confidence,residual_seconds=.001,fit_observations=obs,observed_span_seconds=39.))


def profile(offset,status='validated_relation'):
    obs=[dict(query_time=t,offset=offset,window=.5,supported=True,precise=True) for t in (1.,4.,7.)]
    return dict(status=status,prior=dict(offset=offset,slope=0.,prior_strength=.5),model=dict(kind='constant',offset=offset,slope=0.),training=obs,selection=obs,audit=obs,
                confidence=.5,validated_source_range=[0.,9.],timing_limit_seconds=.025)


class TimingGraphTest(unittest.TestCase):
    def test_bounded_comparison_requires_reserved_support_and_rejects_competitors(self):
        records=[dict(id='ref',duration=200),dict(id='query',duration=8)]
        layout=[dict(recording=0,status='supported',timing_knots=[[0,0],[200,200]],fit_residual_seconds=.001),dict(recording=1,status='unresolved')]
        def obs(t,offset):return dict(query_time=t,offset=offset,window=.4,wave_correlation=.3,strongest_control=.08,precise=False)
        h=dict(prior=dict(offset=40),training=[obs(t,40+d) for t,d in ((.5,-.03),(3.5,.01),(6.5,.04))],
               selection=[obs(2,40),obs(5,40.04)],audit=[obs(2.8,39.98),obs(7.2,40.03)])
        profiles=[dict(reference=0,query=1,profiles=[h])]
        discovery=dict(pairs=[dict(reference=0,query=1,candidates=[dict(offset=40,score=.3,background_z=6),dict(offset=100,score=.2)])])
        proposals=relation_graph.comparison_placements(records,layout,profiles,discovery)
        self.assertEqual(len(proposals),1);self.assertEqual(proposals[0]['rate'],1)
        self.assertEqual(proposals[0]['status'],'provisional_acoustic_review_required')
        self.assertLess(proposals[0]['fit_residual_seconds'],.15)
        discovery['pairs'][0]['candidates'][1]['score']=.29
        self.assertFalse(relation_graph.comparison_placements(records,layout,profiles,discovery))
        discovery['pairs'][0]['candidates'][1]['score']=.2
        h['audit'][1].update(offset=40.5,precise=True)
        self.assertFalse(relation_graph.comparison_placements(records,layout,profiles,discovery))

    def test_shared_excerpt_cannot_join_otherwise_unrelated_long_recordings(self):
        records=[dict(duration=180.),dict(duration=240.),dict(duration=8.)]
        excerpt=edge(0,1,100)
        excerpt['selected']['fit_observations']=[dict(query_time=t,offset=100,window=1) for t in (1,3,7)]
        short=edge(0,2,40)
        short['selected']['fit_observations']=[dict(query_time=t,offset=40,window=1) for t in (1,3,7)]
        selected,rejected=relation_graph.reject_excerpt_bridges(records,[excerpt,short])
        self.assertEqual(selected,[short])
        self.assertEqual(rejected[0]['status'],'unresolved_short_excerpt_bridge')

    def test_composition_retains_parent_and_child_bends(self):
        parent=[[0,10],[30,40.1],[80,90.0]];child=[[0,5],[20,25.02],[60,65]]
        mapping=compose(parent,child,60)
        self.assertGreater(len(mapping),3)
        for t in np.linspace(0,60,301):self.assertAlmostEqual(value(mapping,t),value(parent,value(child,t)),places=10)
        with self.assertRaises(ValueError):validate([[0,0],[1,-1]])

    def test_video_partition_bounds_error_without_missing_or_stretched_frames(self):
        points=[[0,1],[40,41.25],[80,80.8],[120,121.1]];fps=50.;total=6000
        segments=video_segments(points,total,fps,.015)
        self.assertGreater(len(segments),1);self.assertEqual(segments[0][0],0);self.assertEqual(segments[-1][1],total)
        for left,right in zip(segments,segments[1:]):self.assertEqual(left[1],right[0])
        for first,last,offset,error in segments:
            for t in np.linspace(first/fps,last/fps,33):self.assertLessEqual(abs((t+offset)-value(points,t)),.015000001)
            self.assertGreater(last,first)

    def test_repeated_programme_requires_cross_reference_agreement(self):
        records=[dict(id='camera',duration=300.,video={'avg_frame_rate':'50/1'}),dict(id='recorder',duration=500.),dict(id='short',duration=9.)]
        base=[edge(0,1,10),edge(0,2,250)]
        profiles=[dict(reference=0,query=2,profiles=[profile(250),profile(40,'event_supported_timing_unresolved')]),
                  dict(reference=1,query=2,profiles=[profile(30)])]
        selected,decisions=relation_graph.select_edges(pipeline,records,base,profiles)
        self.assertEqual(decisions[0]['status'],'corroborated_alternative')
        self.assertFalse(any(e['query']==2 and e['selected']['offset']==250 for e in selected))
        layout,graph=relation_graph.solve(records,selected)
        model=next(m for m in layout if m['recording']==2)
        self.assertAlmostEqual(model['start_seconds'],40)
        self.assertEqual(graph['reference_recordings'][0],0)
        # With only one equally plausible reference for each event, abstain.
        profiles[0]['profiles']=profiles[0]['profiles'][:1]
        selected,decisions=relation_graph.select_edges(pipeline,records,base,profiles)
        self.assertEqual(decisions[0]['status'],'unresolved_competing_events')
        self.assertFalse(any(2 in (e['reference'],e['query']) for e in selected))

    def test_piecewise_graph_preserves_camera_clock_and_rejects_bad_cycle(self):
        records=[dict(id=str(i),duration=120.,video={'avg_frame_rate':'50/1'} if i==0 else None) for i in range(3)]
        first=edge(0,1,7);first['selected']['timing_knots']=[[0,7],[60,67.1],[120,127]]
        first['selected']['fit_observations']=[dict(query_time=t,offset=value(first['selected']['timing_knots'],t)-t) for t in (1.,20.,40.)]
        second=edge(1,2,3);wrong=edge(0,2,99,.05)
        layout,graph=relation_graph.solve(records,[first,second,wrong])
        self.assertEqual(graph['rejected_edges'][0]['edge'],2)
        model=next(m for m in layout if m['recording']==2)
        self.assertAlmostEqual(value(model['timing_knots'],57),67.1)

    @unittest.skipUnless(shutil.which('ffmpeg'),'FFmpeg required for audio correction')
    def test_piecewise_audio_preserves_channels_duration_and_event_times(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);source=root/'source.wav';target=root/'corrected.wav';sr=16000
            t=np.arange(12*sr)/sr;data=np.zeros((len(t),2))
            events=[(2.,0),(5.,1),(8.,0),(11.,1)]
            for center,channel in events:data[:,channel]+=.4*np.exp(-((t-center)/.06)**2)*np.sin(2*np.pi*700*t)
            with wave.open(str(source),'wb') as output:
                output.setparams((2,2,sr,0,'NONE','not compressed'));output.writeframes((data*32767).astype('<i2').tobytes())
            before=source.read_bytes();mapping=[[0,0],[6,6.12],[12,12]]
            record=dict(source=str(source),duration=12.,audio=dict(sample_rate=str(sr),channels=2),video=None)
            project_export.render_mapped_audio(record,mapping,target)
            decoded=subprocess.check_output(['ffmpeg','-nostdin','-v','error','-i',str(target),'-f','f32le','-'])
            actual=np.frombuffer(decoded,dtype='<f4').reshape(-1,2)
            self.assertEqual(len(actual),12*sr);self.assertEqual(before,source.read_bytes())
            for center,channel in events:
                expected=value(mapping,center);a=round((expected-.25)*sr);b=round((expected+.25)*sr)
                energy=actual[a:b,channel]**2;measured=float(np.sum(energy*np.arange(a,b)/sr)/np.sum(energy))
                self.assertLess(abs(measured-expected),.03)
                self.assertLess(float(np.max(np.abs(actual[a:b,1-channel]))),.001)


if __name__=='__main__':unittest.main()
