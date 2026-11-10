import sys
from pathlib import Path
import tempfile
import unittest
import wave
import numpy as np
from scipy import signal

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'internal'/'autosync'))
import pipeline
import refinement


def observations(times,offset):
    return [dict(query_time=float(t),offset=float(offset(t)),supported=True) for t in times]


class RefinementTest(unittest.TestCase):
    def test_model_complexity_requires_unseen_window_agreement(self):
        train=np.arange(0,121,20)
        for kind,truth in [('constant',lambda t:3.125),('affine',lambda t:3.125+.001*t),
                           ('piecewise_linear',lambda t:3.125+.002*min(t,60)-.002*max(t-60,0))]:
            models=refinement.fit_models(observations(train,truth))
            accepted=[m for m in models if refinement.judge(m,observations(np.arange(10,120,20),truth))['passes']]
            self.assertEqual(accepted[0]['kind'],kind)
            self.assertTrue(refinement.judge(accepted[0],observations(np.arange(5,120,20),truth))['passes'])
            bad=observations(np.arange(5,120,20),truth);bad[2]['offset']+=.1
            self.assertFalse(refinement.judge(accepted[0],bad)['passes'])

    def test_phase_confirmation_localizes_filtered_audio_but_rejects_tone(self):
        rng=np.random.default_rng(514)
        ref=signal.lfilter([1],[1,-.7],rng.normal(size=8000))
        query=signal.lfilter([.2,.5,.2],[1],ref[1000:5000])
        result=refinement.phase_peak(ref,query)
        self.assertTrue(result['distinct']);self.assertLessEqual(abs(result['lag']-999),2)
        tone=np.sin(np.arange(8000)*2*np.pi/50)
        self.assertFalse(refinement.phase_peak(tone,tone[1000:5000])['distinct'])

    def test_nonlinear_pcm_profile_and_unrelated_rejection(self):
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);sr=16000;rng=np.random.default_rng(191)
            ref=signal.lfilter([1],[1,-.8],rng.normal(0,.04,150*sr))
            t=np.arange(120*sr)/sr
            delay=7.125+.0015*np.minimum(t,60)-.0015*np.maximum(t-60,0)
            query=np.interp((t+delay)*sr,np.arange(len(ref)),ref)
            records=[]
            for name,values in [('reference',ref),('query',query),('unrelated',rng.normal(0,.04,len(query)))]:
                path=root/(name+'.wav')
                with wave.open(str(path),'wb') as f:
                    f.setparams((1,2,sr,0,'NONE','not compressed'));f.writeframes((values*32767).astype('<i2').tobytes())
                records.append(dict(cache=str(path),duration=len(values)/sr))
            result=refinement.profile_candidate(pipeline,records[0],records[1],dict(offset=7.125,slope=0))
            self.assertEqual(result['status'],'validated_relation',result)
            self.assertEqual(result['model']['kind'],'piecewise_linear')
            for center in [5,35,65,95,115]:
                truth=7.125+.0015*min(center,60)-.0015*max(center-60,0)
                self.assertLess(abs(float(refinement.predict(result['model'],center))-truth),.01)
            unrelated=refinement.profile_candidate(pipeline,records[0],records[2],dict(offset=7.125,slope=0))
            self.assertEqual(unrelated['status'],'insufficient_evidence')


if __name__=='__main__':unittest.main()
