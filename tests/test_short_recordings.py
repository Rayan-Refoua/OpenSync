"""Actual file-list regression: recover short positives without false pairs."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
import wave
import numpy as np
from scipy import signal


class ShortRecordingTest(unittest.TestCase):
    @unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'),'FFmpeg required')
    def test_distant_microphone_delay_and_echo_do_not_become_clock_drift(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);rng=np.random.default_rng(907);sr=16000
            source=signal.lfilter([1],[1,-.55],rng.normal(0,.04,40*sr))
            capture_start=10.;path_delay=.175;duration=20.
            # Query microphone hears the stage 175ms later. Its first sample
            # therefore corresponds to earlier stage sound, at 9.825s.
            arrival_offset=capture_start-path_delay
            first=round(arrival_offset*sr);count=round(duration*sr)
            query=source[first:first+count].copy()
            for delay,gain in ((.047,.25),(.119,.15),(.233,.08)):
                shift=round(delay*sr);query+=gain*source[first-shift:first-shift+count]
            query+=rng.normal(0,.002,len(query))
            files=[]
            for name,samples in [('stage',source),('distant',query)]:
                path=root/(name+'.wav');files.append(str(path))
                with wave.open(str(path),'wb') as output:
                    output.setparams((1,2,sr,0,'NONE','not compressed'));output.writeframes((samples*32767).astype('<i2').tobytes())
            packaged=os.environ.get('OPENSYNC_TEST_EXECUTABLE')
            command=[packaged,'sync'] if packaged else [sys.executable,str(Path(__file__).resolve().parents[1]/'internal/autosync/pipeline.py')]
            result=subprocess.run(command+['--files']+files+['--output',str(root/'result')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)
            payload=json.loads((root/'result/OpenSync.opensync').read_text())['payload']
            models={payload['recordings'][m['recording']]['name']:m for m in payload['timing_models']}
            distant=models['distant.wav'];stage=models['stage.wav']
            self.assertTrue(distant['globally_resolved'])
            self.assertLess(abs(distant['component_offset_seconds']-stage['component_offset_seconds']-arrival_offset),.003)
            self.assertLess(abs(distant['rate']-1),.0001)
            self.assertEqual(distant['timing_basis'],'acoustic_arrival')
            self.assertIsNone(distant['propagation_delay_seconds'])
            self.assertEqual(distant['physical_capture_alignment'],'not_independently_established')
            manifest=json.loads((root/'result/timeline-manifest.json').read_text())
            self.assertEqual(manifest['derivatives'],[])

    @unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'),'FFmpeg required')
    def test_three_second_match_with_noise_and_periodic_negatives(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);rng=np.random.default_rng(719);sr=16000
            ref=signal.lfilter([1],[1,-.7],rng.normal(0,.03,12*sr))
            query=ref[round(2.375*sr):round(5.375*sr)]
            files=[]
            for name,samples in [('reference',ref),('short',query),('noise',rng.normal(0,.03,3*sr)),('tone',.1*np.sin(2*np.pi*200*np.arange(3*sr)/sr))]:
                path=root/(name+'.wav');files.append(str(path))
                with wave.open(str(path),'wb') as output:
                    output.setparams((1,2,sr,0,'NONE','not compressed'));output.writeframes((samples*32767).astype('<i2').tobytes())
            packaged=os.environ.get('OPENSYNC_TEST_EXECUTABLE')
            command=[packaged,'sync'] if packaged else [sys.executable,str(Path(__file__).resolve().parents[1]/'internal/autosync/pipeline.py')]
            result=subprocess.run(command+['--files']+files+['--output',str(root/'result')],capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stdout+result.stderr)
            project=json.loads((root/'result/OpenSync.opensync').read_text())['payload'];records=project['recordings']
            models={records[m['recording']]['name']:m for m in project['timing_models']}
            self.assertFalse(models['short.wav']['status'].startswith('unresolved'))
            self.assertLess(abs(models['short.wav']['component_offset_seconds']-models['reference.wav']['component_offset_seconds']-2.375),.002)
            self.assertTrue(models['noise.wav']['status'].startswith('unresolved'))
            self.assertTrue(models['tone.wav']['status'].startswith('unresolved'))


if __name__=='__main__':unittest.main()
