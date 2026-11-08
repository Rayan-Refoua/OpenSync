import array
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
import wave

spec = importlib.util.spec_from_file_location("prepare_review", Path(__file__).resolve().parents[1] / "tools" / "prepare_review.py")
review = importlib.util.module_from_spec(spec)
spec.loader.exec_module(review)


class ReviewPacketTest(unittest.TestCase):
    def fixture(self, root):
        for name in ("ref.wav", "query.wav"):
            samples = array.array("h", [(i % 1000) - 500 for i in range(16000 * 10)])
            if sys.byteorder != "little":
                samples.byteswap()
            with wave.open(str(root / name), "wb") as f:
                f.setparams((1, 2, 16000, 0, "NONE", "not compressed"))
                f.writeframes(samples.tobytes())
        manifest = root / "manifest.json"
        manifest.write_text(json.dumps({"version": 1, "pairs": [{"name": "<unsafe>", "reference": "ref.wav", "query": "query.wav", "label": "unreviewed"}]}), encoding="utf-8")
        report = root / "report.json"
        report.write_text(json.dumps({"version": 1, "status": "completed", "inputs": [{"path": str(path.resolve()), "sha256": review.digest(path)} for path in (manifest, root / "ref.wav", root / "query.wav")], "runs": [{"dataset": "real", "scenario": "<unsafe>", "peaks": [{"offset_seconds": 0}]}]}), encoding="utf-8")
        return manifest, report

    def test_packet_has_correct_channels_and_no_truth(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            manifest, report = self.fixture(root)
            output = root / "packet"
            self.assertEqual(review.prepare(manifest, report, output), 1)
            notes = json.loads((output / "review-notes.json").read_text(encoding="utf-8"))
            self.assertEqual(notes["entries"][0]["review"], "unreviewed")
            self.assertEqual(notes["entries"][0]["measured_events"], [])
            self.assertNotIn("<unsafe>", (output / "index.html").read_text(encoding="utf-8"))
            for clip in notes["entries"][0]["clips"]:
                with wave.open(str(output / clip["file"]), "rb") as f:
                    self.assertEqual((f.getnchannels(), f.getframerate(), f.getnframes()), (2, 16000, 128000))
                    samples = array.array("h", f.readframes(f.getnframes()))
                    self.assertEqual(samples[0::2], samples[1::2])
            with self.assertRaises(FileExistsError):
                review.prepare(manifest, report, output)

    def test_changed_cache_is_rejected_before_creating_packet(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            manifest, report = self.fixture(root)
            with (root / "ref.wav").open("ab") as f:
                f.write(b"changed")
            with self.assertRaisesRegex(ValueError, "changed"):
                review.prepare(manifest, report, root / "packet")
            self.assertFalse((root / "packet").exists())


if __name__ == "__main__":
    unittest.main()
