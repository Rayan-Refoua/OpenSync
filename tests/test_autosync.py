import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
import wave

import numpy as np
from scipy import signal

spec = importlib.util.spec_from_file_location("autosync", Path(__file__).resolve().parents[1]/"internal"/"autosync"/"pipeline.py")
engine = importlib.util.module_from_spec(spec)
spec.loader.exec_module(engine)


def write(path, samples):
    with wave.open(str(path),"wb") as f:
        f.setparams((1,2,16000,0,"NONE","not compressed"))
        f.writeframes(np.clip(samples*32767,-32768,32767).astype("<i2").tobytes())


class AutonomousEngineTest(unittest.TestCase):
    def source(self, path, duration, video=True):
        return {"id":path.stem,"name":path.name,"source":str(path),"local_source":str(path),"cache":str(path),"duration":duration,"source_group":"Camera","video":{"width":1920,"height":1080,"avg_frame_rate":"50/1"} if video else None,"audio":{"channels":1,"sample_rate":"16000","bits_per_sample":16}}

    def test_positive_negative_and_offset_measurement(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);rng=np.random.default_rng(183)
            base=signal.lfilter([1],[1,-.85],rng.normal(size=120*16000)).astype(np.float32)*.08
            # Irregular events and independent mic noise; truth is not supplied
            # to waveform maximization other than a deliberately coarse prior.
            ref=root/"ref.wav";query=root/"q.wav";noise=root/"noise.wav"
            write(ref,base)
            write(query,base[round(7.375*16000):round(57.375*16000)]*.6+rng.normal(0,.004,50*16000))
            write(noise,rng.normal(0,.08,50*16000))
            r=self.source(ref,120);q=self.source(query,50)
            measured=engine.waveform(r,q,5,7.4,4)
            self.assertLess(abs(measured["offset"]-7.375),.001)
            positive=engine.assess_candidate(r,q,{"offset":7.4,"votes":12})
            self.assertEqual(positive["status"],"acoustically_supported_not_certified")
            negative=engine.assess_candidate(r,self.source(noise,50),{"offset":7.4,"votes":12})
            self.assertNotEqual(negative["status"],"acoustically_supported_not_certified")
            write(noise,np.zeros(50*16000))
            silent=engine.assess_candidate(r,self.source(noise,50),{"offset":7.4,"votes":12})
            self.assertNotEqual(silent["status"],"acoustically_supported_not_certified")

    def edge(self,a,b,offset,confidence=1):
        return {"reference":a,"query":b,"selected":{"offset":offset,"slope":0,"confidence":confidence,"residual_seconds":.001,"fit_observations":[{"query_time":t,"offset":offset} for t in (1,10,20)]}}

    def test_graph_conflict_and_disconnected_sources(self):
        records=[{"id":str(i),"duration":50+i} for i in range(4)]
        layout,report=engine.graph_layout(records,[self.edge(0,1,3),self.edge(1,2,5),self.edge(0,2,80,.1)])
        by_id={p["recording"]:p for p in layout}
        self.assertAlmostEqual(by_id[1]["start_seconds"]-by_id[0]["start_seconds"],3,places=5)
        self.assertAlmostEqual(by_id[2]["start_seconds"]-by_id[0]["start_seconds"],8,places=5)
        self.assertEqual(len(report["rejected_edges"]),1)
        self.assertEqual(by_id[3]["component_size"],1)
        self.assertTrue(by_id[3]["status"].startswith("unresolved"))

    def test_export_preserves_all_sources_and_links(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);records=[]
            for i in range(3):
                path=root/f"source{i}.wav";write(path,np.zeros(50*16000))
                records.append(self.source(path,50,video=i!=2))
            engine.finish(records,[self.edge(0,1,4)],root,.15)
            audit=json.loads((root/"structural-audit.json").read_text())
            manifest=json.loads((root/"timeline-manifest.json").read_text())
            self.assertEqual(audit["recordings"],3)
            self.assertEqual(audit["video_clipitems"],2)
            self.assertEqual(audit["audio_clipitems"],3)
            self.assertEqual(len(manifest["unresolved_recordings"]),1)
            self.assertTrue(audit["all_source_frames_accounted"])
            self.assertFalse(manifest["source_media_modified"])

    def test_fingerprint_offset_is_not_from_metadata(self):
        rng=np.random.default_rng(41)
        t=np.arange(30*16000)/16000
        sound=(np.sin(2*np.pi*(320*t+4*t*t))+.5*np.sin(2*np.pi*(1110*t+3*t*t)))*(.1+.05*np.sin(t*3.13))
        sound+=rng.normal(0,.01,len(t))
        a=engine.fingerprint(sound.astype(np.float32))
        b=engine.fingerprint(sound[5*16000:25*16000].astype(np.float32))
        index={}
        for key,time in a:index.setdefault(key,[]).append(time)
        offsets=[]
        for key,time in b:
            offsets.extend((at-time)*engine.HOP/engine.SR for at in index.get(key,[]))
        self.assertTrue(any(abs(offset-5)<.02 for offset in offsets))

    def test_discovery_resumes_without_changing_inputs(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);(root/"features").mkdir()
            rng=np.random.default_rng(54)
            t=np.arange(45*16000)/16000
            base=(np.sin(2*np.pi*(337*t+3.7*t*t))+.4*np.sin(2*np.pi*(1310*t+1.1*t*t)))*(.12+.06*np.sin(t*2.71))
            base+=rng.normal(0,.01,len(t))
            records=[]
            for name,samples in (("ref",base),("query",base[5*16000:25*16000])):
                path=root/(name+".wav");write(path,samples)
                rec=self.source(path,len(samples)/16000)
                rec["cache_sha256"]=engine.sha(path);records.append(rec)
            first=engine.discover(records,root)
            self.assertEqual(len(first),1)
            self.assertTrue(any(abs(c["offset"]-5)<.2 for c in first[0]["candidates"]))
            self.assertEqual(first,engine.discover(records,root))
            for rec in records:self.assertEqual(rec["cache_sha256"],engine.sha(rec["cache"]))
            bad=dict(records[0],cache_sha256="changed")
            with self.assertRaisesRegex(ValueError,"stale"):
                engine.fingerprints(bad,root)


if __name__=="__main__":unittest.main()
