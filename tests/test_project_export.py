import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import shutil
import wave
import unittest
import xml.etree.ElementTree as ET

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/"internal"/"autosync"))
import project_export as export


class ProjectExportTest(unittest.TestCase):
    def test_comparison_export_is_opt_in_marked_and_preserves_native_truth(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);path,records=self.make_project(root)
            native=export.load_project(path)['payload'];layout=[dict(m,start_seconds=m['analysis_component_offset_seconds']) for m in native['timing_models']]
            native['graph']['provisional_placements']=[dict(recording=2,reference=0,start_seconds=5.,rate=1.,timing_knots=[[0,5],[12,17]],fit_residual_seconds=.07,status='provisional_acoustic_review_required')]
            export.save_project(path,records,[],layout,native['graph'],'test');before=path.read_bytes()
            strict=export.export_project(path,root/'strict')
            comparison=export.export_project(path,root/'comparison',include_provisional=True)
            self.assertEqual(len(strict['unresolved_recordings']),1)
            self.assertEqual(comparison['unresolved_recordings'],[])
            self.assertEqual(comparison['provisional_recordings'],[records[2]['name']])
            self.assertEqual(path.read_bytes(),before)
            self.assertFalse(export.load_project(path)['payload']['timing_models'][2]['globally_resolved'])
            tree=ET.parse(root/'comparison'/'OpenSync.xml')
            self.assertTrue(any('[REVIEW]' in c.findtext('name','') for c in tree.findall('.//sequence/media/audio/track/clipitem')))
            self.assertTrue(any(m.findtext('name','').startswith('REVIEW:') for m in tree.findall('.//sequence/marker')))
            self.assertEqual(comparison['derivatives'],[])

    def test_nonzero_source_timecode_survives_project_and_export(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);path,records=self.make_project(root)
            records[0]['source_timecode']={'frame':8319,'string':'00:05:32:19','rate':'25','displayformat':'NDF'}
            original=export.load_project(path)['payload']
            layout=[dict(m,start_seconds=m['analysis_component_offset_seconds']) for m in original['timing_models']]
            export.save_project(path,records,[],layout,original['graph'],'test')
            before=path.read_bytes();export.export_project(path,root/'export')
            self.assertEqual(path.read_bytes(),before)
            tree=ET.parse(root/'export'/'OpenSync.xml')
            file=tree.find(".//file[@id='file-0'][timecode]")
            self.assertEqual(file.findtext('./timecode/frame'),'8319')
            self.assertEqual(file.findtext('./timecode/string'),'00:05:32:19')
            clip=tree.find('./project/children/sequence/media/video/track/clipitem')
            self.assertEqual(clip.findtext('in'),'0')
            self.assertEqual(clip.findtext('out'),'1250')

    def test_import_audit_detects_negative_source_address_and_missing_items(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);path,_=self.make_project(root)
            export.export_project(path,root/'export')
            original=root/'export'/'OpenSync.xml';imported=root/'roundtrip.xml';report=root/'audit.json'
            result=export.audit_roundtrip(original,original,report)
            self.assertFalse(result['errors']);self.assertGreater(result['checked_clipitems'],0)
            tree=ET.parse(original);track=tree.find('./project/children/sequence/media/video/track')
            clip=track.find('clipitem');clip.find('in').text='-8319';clip.find('out').text=str(1250-8319)
            tree.write(imported)
            result=export.audit_roundtrip(original,imported,report)
            self.assertEqual(result['errors'][0]['reason'],'source_or_timeline_range_changed')
            track.remove(clip);tree.write(imported)
            result=export.audit_roundtrip(original,imported,report)
            self.assertTrue(any(e['reason']=='missing_timeline_item' for e in result['errors']))

    @unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'),'FFmpeg required for audio timing derivatives')
    def test_audio_derivative_keeps_original_master_and_valid_links(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);source=root/'original.wav'
            with wave.open(str(source),'wb') as w:
                w.setparams((1,2,16000,0,'NONE','not compressed'))
                w.writeframes(bytes(180*16000*2))
            rec={'id':'audio','name':source.name,'source':str(source),'local_source':str(source),'duration':180.,'video':None,'audio':{'channels':1,'sample_rate':'16000','bits_per_sample':16}}
            model={'recording':0,'component':1,'component_size':2,'start_seconds':0.,'rate':1.0018,'fit_residual_seconds':.001,'status':'acoustically_supported_not_certified'}
            reference=root/'reference.wav'
            with wave.open(str(reference),'wb') as w:
                w.setparams((1,2,16000,0,'NONE','not compressed'));w.writeframes(bytes(200*16000*2))
            ref=dict(rec,id='reference',name=reference.name,source=str(reference),local_source=str(reference),duration=200.)
            ref_model=dict(model,recording=1,rate=1.)
            path=root/'OpenSync.opensync';export.save_project(path,[rec,ref],[],[model,ref_model],{'components':[[0,1]]},'test')
            manifest=export.export_project(path,root/'export')
            self.assertEqual(len(manifest['derivatives']),1)
            tree=ET.parse(root/'export'/'OpenSync.xml')
            self.assertEqual(len(tree.findall('./project/children/bin/children/clip')),3)
            self.assertTrue(source.is_file())
            self.assertEqual(tree.findtext('./project/children/sequence/media/audio/track/clipitem/masterclipid'),'master-audio-corrected')

    def make_project(self,root):
        records=[];models=[]
        for i,(rate,duration) in enumerate(((25,50),(30,50),(None,12))):
            folder=root/f"input-{i}";folder.mkdir()
            source=folder/("ordinary name " + str(i) + (".mp4" if rate else ".wav"))
            source.write_bytes(b"metadata serializer test fixture")
            rec={"id":str(i),"name":source.name,"source":str(source),"duration":duration,"video":{"avg_frame_rate":str(rate),"nb_frames":str(rate*duration),"width":1280,"height":720} if rate else None,"audio":{"channels":2 if rate else 1,"sample_rate":"48000","bits_per_sample":16}}
            records.append(rec)
            models.append({"recording":i,"component":1 if i<2 else 2,"component_size":2 if i<2 else 1,"start_seconds":1.23456789012345 if i==1 else 0.0,"rate":1.0,"fit_residual_seconds":.001,"status":"acoustically_supported_not_certified" if i<2 else "unresolved_no_supported_relation"})
        path=root/"OpenSync.opensync"
        export.save_project(path,records,[],models,{"components":[[0,1],[2]]},"test")
        return path,records

    def test_precision_policies_mixed_frame_rates_and_source_bins(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);path,records=self.make_project(root);before=path.read_bytes()
            project=export.load_project(path)
            self.assertEqual(project['payload']['timing_models'][1]['component_offset_seconds'],1.23456789012345)
            self.assertFalse(project['payload']['timing_models'][2]['globally_resolved'])
            for policy in ('append','separate-sequence'):
                out=root/policy
                manifest=export.export_project(path,out,policy)
                self.assertEqual(before,path.read_bytes())
                tree=ET.parse(out/'OpenSync.xml')
                self.assertEqual(tree.getroot().get('version'),'4')
                self.assertEqual(len(tree.findall('./project/children/bin')),3)
                self.assertEqual(len(tree.findall('./project/children/bin/children/clip')),3)
                self.assertEqual(len(tree.findall('./project/children/sequence')),1 if policy=='append' else 2)
                self.assertEqual(manifest['placements'][0]['source_out'],1250)
                self.assertEqual(manifest['placements'][1]['source_out'],1500)
                self.assertEqual(manifest['placements'][2]['sequence'],'synced' if policy=='append' else 'unresolved')
                self.assertEqual(manifest['sequence_fps'],'30')
                if policy=='append':
                    self.assertGreater(manifest['placements'][2]['start'],max(p['end'] for p in manifest['placements'][:2]))
                clip=tree.find('./project/children/sequence/media/video/track/clipitem')
                clip.find('./link/trackindex').text='999'
                bad=out/'broken.xml';tree.write(bad)
                with self.assertRaisesRegex(ValueError,'index mismatch'):export.audit_xml(bad,records,manifest['placements'],policy)

    def test_project_integrity_rejects_altered_timing(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);path,_=self.make_project(root)
            data=json.loads(path.read_text());data['payload']['timing_models'][1]['component_offset_seconds']=999
            path.write_text(json.dumps(data))
            with self.assertRaisesRegex(ValueError,'integrity'):export.load_project(path)

    def test_audio_stream_origin_is_preserved_separately_from_video_origin(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);path,records=self.make_project(root)
            original=export.load_project(path)
            records[1]['audio']['start_time']='0.250000'
            records[1]['video']['start_time']='0.000000'
            layout=[]
            for m in original['payload']['timing_models']:
                item=dict(m,start_seconds=m['analysis_component_offset_seconds']);layout.append(item)
            export.save_project(path,records,[],layout,original['payload']['graph'],'test')
            model=export.load_project(path)['payload']['timing_models'][1]
            self.assertEqual(model['analysis_component_offset_seconds'],1.23456789012345)
            self.assertEqual(model['analysis_audio_origin_seconds'],.25)
            self.assertEqual(model['component_offset_seconds'],1.23456789012345-.25)

    def test_ntsc_frame_rate_round_trip(self):
        node=ET.Element('clipitem');export.xml_rate(node,'30000/1001')
        self.assertEqual(str(export.read_rate(node)),'30000/1001')
        self.assertEqual(node.findtext('./rate/ntsc'),'TRUE')


if __name__=='__main__':unittest.main()
