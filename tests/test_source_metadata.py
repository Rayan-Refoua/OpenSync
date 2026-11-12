from pathlib import Path
import sys
import tempfile
import unittest
import xml.etree.ElementTree as ET
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'internal'/'autosync'))
import source_metadata as metadata
import project_export as export


class SourceMetadataTest(unittest.TestCase):
    def test_hfr_ltc_and_odd_frame_parity(self):
        first=metadata.decode_sony_ltc('19320500',25,True,'50/1')
        last=metadata.decode_sony_ltc('07431280',25,True,'50/1')
        self.assertEqual(first['string'],'00:05:32:38')
        self.assertEqual(first['frame'],16638)
        self.assertEqual(last['frame']-first['frame'],21527)
        self.assertEqual(last['string'],'00:12:43:15')

    def test_drop_frame_and_invalid_timecodes(self):
        self.assertEqual(metadata.parse_string('01:00:00;00','30000/1001')['frame'],107892)
        with self.assertRaises(ValueError):metadata.parse_string('00:01:00;00','30000/1001')
        with self.assertRaises(ValueError):metadata.decode_sony_ltc('FA000000',25,False,'25/1')

    def test_sidecar_is_validated_and_explicitly_exported(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);source=root/'camera.mp4';source.write_bytes(b'fixture')
            sidecar=root/'cameraM01.XML'
            sidecar.write_text('<NonRealTimeMeta xmlns="urn:example"><Duration value="21528"/><LtcChangeTable tcFps="25" halfStep="true"><LtcChange frameCount="0" value="19320500"/><LtcChange frameCount="21527" value="07431280"/></LtcChangeTable><VideoFormat><VideoFrame formatFps="50p"/></VideoFormat></NonRealTimeMeta>')
            rec=dict(id='fixture',name=source.name,source=str(source),duration=430.56,video=dict(avg_frame_rate='50/1',nb_frames='21528'),audio=None)
            tc=metadata.source_timecode(rec);self.assertEqual(tc['frame'],16638);self.assertTrue(tc['end_frame_verified'])
            rec['source_timecode']=tc
            node=export.file_reference(ET.Element('clipitem'),rec,50,set())
            self.assertEqual(node.findtext('timecode/frame'),'16638')
            self.assertEqual(node.findtext('timecode/rate/timebase'),'50')
            self.assertEqual(node.findtext('duration'),'21528')
            # A stale or unrelated sidecar cannot silently set media origin.
            sidecar.write_text(sidecar.read_text().replace('07431280','08431280'))
            self.assertIsNone(metadata.source_timecode(rec))


if __name__=='__main__':unittest.main()
