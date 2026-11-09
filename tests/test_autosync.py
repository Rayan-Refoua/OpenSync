import importlib.util
import argparse
import json
import os
from pathlib import Path
import tempfile
import shutil
import subprocess
import sys
import unittest
import wave

import numpy as np
from scipy import signal

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/"internal"/"autosync"))
spec = importlib.util.spec_from_file_location("autosync", Path(__file__).resolve().parents[1]/"internal"/"autosync"/"pipeline.py")
engine = importlib.util.module_from_spec(spec)
spec.loader.exec_module(engine)


def write(path, samples):
    with wave.open(str(path),"wb") as f:
        f.setparams((1,2,16000,0,"NONE","not compressed"))
        f.writeframes(np.clip(samples*32767,-32768,32767).astype("<i2").tobytes())


class AutonomousEngineTest(unittest.TestCase):
    @unittest.skipUnless(shutil.which("ffmpeg") and shutil.which("ffprobe"),"FFmpeg needed for real CLI cache/export integration")
    def test_cli_produces_timeline_from_only_file_list(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);rng=np.random.default_rng(206)
            modulation=np.repeat(rng.uniform(.1,1,900),1600)
            base=signal.lfilter([1],[1,-.85],rng.normal(size=90*16000))*modulation*.05
            files=[]
            for name,samples in (("reference",base),("camera audio",base[round(7.375*16000):round(27.375*16000)]),("unrelated",rng.normal(0,.04,12*16000))):
                path=root/(name+".wav");write(path,samples);files.append(str(path))
            silent_video=root/"video without audio.mp4"
            subprocess.run(["ffmpeg","-nostdin","-v","error","-f","lavfi","-i","color=c=blue:s=160x90:r=25","-t","2","-an","-c:v","mpeg4",str(silent_video)],check=True)
            files.append(str(silent_video))
            listing=root/"files.json";listing.write_text(json.dumps(files),encoding="utf-8")
            output=root/"result"
            packaged=os.environ.get("OPENSYNC_TEST_EXECUTABLE")
            prefix=[packaged,"sync"] if packaged else [sys.executable,str(Path(engine.__file__))]
            child_env=os.environ.copy()
            if packaged:
                child_env.update(PATH=str(Path(os.environ['SystemRoot'])/'System32'),PYTHONHOME=str(root/'no-python-installation'),PYTHONPATH=str(root/'no-python-packages'))
            command=prefix+["--file-list",str(listing),"--output",str(output)]
            run=subprocess.run(command,capture_output=True,text=True,env=child_env)
            self.assertEqual(run.returncode,0,run.stdout+run.stderr)
            manifest=json.loads((output/"timeline-manifest.json").read_text())
            inventory=json.loads((output/"inventory.json").read_text())["records"]
            by_name={inventory[p["recording"]]["name"]:p for p in manifest["placements"]}
            self.assertEqual(manifest['sequence_fps'],'25')
            self.assertLess(abs((by_name["camera audio.wav"]["start"]-by_name["reference.wav"]["start"])/25-7.375),.021)
            self.assertGreater(by_name["unrelated.wav"]["start"],max(by_name[n]["end"] for n in ("camera audio.wav","reference.wav")))
            self.assertEqual(set(manifest["unresolved_recordings"]),{"unrelated.wav","video without audio.mp4"})
            self.assertTrue((output/"OpenSync.opensync").is_file())
            # Identical command resumes without an extra user decision.
            resumed=subprocess.run(command,capture_output=True,text=True,env=child_env)
            self.assertEqual(resumed.returncode,0,resumed.stdout+resumed.stderr)
            self.assertEqual(manifest,json.loads((output/"timeline-manifest.json").read_text()))
            saved=(output/"OpenSync.opensync").read_bytes()
            cache=output/"cache";hidden_cache=root/"held-cache"
            self.assertTrue(cache.resolve().is_relative_to(root.resolve()))
            self.assertTrue(hidden_cache.resolve().is_relative_to(root.resolve()))
            cache.rename(hidden_cache)
            export_prefix=[packaged] if packaged else [sys.executable,str(Path(engine.__file__))]
            export_command=export_prefix+["export",str(output/"OpenSync.opensync"),"--output",str(root/"reexport"),"--unsynced","separate-sequence"]
            exported=subprocess.run(export_command,capture_output=True,text=True,env=child_env)
            self.assertEqual(exported.returncode,0,exported.stdout+exported.stderr)
            self.assertEqual(saved,(output/"OpenSync.opensync").read_bytes())

    def test_public_file_list_resolves_relative_paths_and_rejects_duplicates(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);media=root/"recording with spaces.wav";write(media,np.zeros(16000))
            listing=root/"media.txt";listing.write_text(media.name+"\n",encoding="utf-8-sig")
            args=argparse.Namespace(files=None,file_list=str(listing),input=None,source=None)
            self.assertEqual(engine.input_files(args),[media.resolve()])
            args.files=[str(media)]
            with self.assertRaisesRegex(ValueError,"duplicate"):engine.input_files(args)

    def test_recovery_retains_repeated_alternatives_and_rejects_silence(self):
        rng=np.random.default_rng(199)
        query=signal.lfilter([1],[1,-.7],rng.normal(size=(3,600)),axis=1).astype(np.float32)
        reference=rng.normal(0,.1,size=(3,8000)).astype(np.float32)
        reference[:,1000:1600]+=query
        reference[:,5000:5600]+=query
        candidates=engine.envelope_candidates(reference,query)
        self.assertTrue(any(abs(c["offset"]-16)<.02 for c in candidates))
        self.assertTrue(any(abs(c["offset"]-80)<.02 for c in candidates))
        self.assertEqual(engine.envelope_candidates(reference,np.zeros_like(query)),[])

    def test_recovery_from_audio_with_different_microphone_filter(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);(root/"features").mkdir()
            rng=np.random.default_rng(200)
            modulation=np.repeat(rng.uniform(.1,1,1200),1600)
            base=signal.lfilter([1],[1,-.8],rng.normal(size=120*16000))*modulation*.05
            start=23.376
            clip=base[round(start*16000):round((start+12)*16000)]
            clip=signal.lfilter([.8,.1,.1],[1],clip)+rng.normal(0,.002,len(clip))
            records=[]
            for name,samples in (("ref",base),("query",clip)):
                path=root/(name+".wav");write(path,samples)
                rec=self.source(path,len(samples)/16000);rec["cache_sha256"]=engine.sha(path);records.append(rec)
            features=[engine.envelope_features(rec,root) for rec in records]
            candidates=engine.envelope_candidates(*features)
            self.assertTrue(any(abs(c["offset"]-start)<.05 for c in candidates))
            best=engine.assess_candidate(*records,candidates[0])
            self.assertEqual(best["status"],"acoustically_supported_not_certified")
            self.assertLess(abs(best["offset"]-start),.002)

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
            n=self.source(noise,50)
            for rec in (r,q,n):rec["cache_sha256"]=engine.sha(rec["cache"])
            pairs=[{"reference":a,"query":b,"candidates":[{"offset":offset,"votes":12}]} for a,b,offset in ((0,1,7.4),(0,2,7.4),(1,2,0))]
            serial=engine.verify([r,q,n],pairs,root,"serial",workers=1)
            concurrent=engine.verify([r,q,n],pairs,root,"parallel",workers=4)
            self.assertEqual(serial,concurrent)
            for evidence in (root/"serial").glob("*.json"):
                self.assertEqual(evidence.read_bytes(),(root/"parallel"/evidence.name).read_bytes())
            verified=engine.validate_edges([r,q],serial,root)
            self.assertEqual(len(verified),1)
            for check in verified[0]["validation"]["observations"]:
                for fitted in positive["observations"]:
                    self.assertGreater(abs(check["query_time"]-fitted["query_time"]),(check["window"]+fitted["window"])/2)
            bad=dict(serial[0],selected=dict(positive,offset=positive["offset"]+.06))
            self.assertEqual(engine.validate_edges([r,q],[bad],root),[])
            refined=engine.refine_failed_models([r,q],[bad],[],root)
            self.assertEqual(len(refined),1)
            self.assertLess(abs(refined[0]["selected"]["offset"]-7.375),.002)
            negative=engine.assess_candidate(r,self.source(noise,50),{"offset":7.4,"votes":12})
            self.assertNotEqual(negative["status"],"acoustically_supported_not_certified")
            write(noise,np.zeros(50*16000))
            silent=engine.assess_candidate(r,self.source(noise,50),{"offset":7.4,"votes":12})
            self.assertNotEqual(silent["status"],"acoustically_supported_not_certified")
            # A periodic tone can correlate almost perfectly at multiple
            # incompatible lags. It must not provide unique local evidence.
            write(ref,.15*np.sin(2*np.pi*40*np.arange(120*16000)/16000))
            write(noise,.15*np.sin(2*np.pi*40*np.arange(50*16000)/16000))
            periodic=engine.waveform(r,self.source(noise,50),5,7.4,2)
            self.assertGreater(periodic["wave_correlation"],.99)
            self.assertFalse(periodic["local_peak_distinct"])

    def test_affine_clock_drift_on_known_synthetic_truth(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);rng=np.random.default_rng(205)
            modulation=np.repeat(rng.uniform(.1,1,1800),1600)
            base=signal.lfilter([1],[1,-.9],rng.normal(size=180*16000))*modulation*.035
            path=root/"ref.wav";write(path,base);ref=self.source(path,180)
            for ppm in (-250,250):
                t=np.arange(90*16000)/16000
                samples=np.interp((31.375+t*(1+ppm/1e6))*16000,np.arange(len(base)),base)
                qpath=root/f"q{ppm}.wav";write(qpath,samples)
                query=self.source(qpath,90)
                model=engine.assess_candidate(ref,query,{"offset":31.376,"anchor_time":45,"method":"synthetic"})
                self.assertEqual(model["status"],"acoustically_supported_not_certified")
                for t in (0,45,90):
                    self.assertLess(abs(model["offset"]+model["slope"]*t-(31.375+ppm/1e6*t)),.002)
                self.assertEqual(len(engine.validate_edges([ref,query],[{"reference":0,"query":1,"selected":model}],root)),1)

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
