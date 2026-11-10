from pathlib import Path
import sys
import unittest
sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'internal/autosync'))
from acoustic_context import delay_profile


class AcousticContextTest(unittest.TestCase):
    def profile(self,offsets,precise=True):
        return delay_profile(dict(training=[dict(query_time=i*10.,offset=v,supported=True,precise=precise) for i,v in enumerate(offsets)]))

    def test_constant_propagation_is_not_drift(self):
        p=self.profile([12.175]*6)
        self.assertEqual(p['interpretation'],'stable_acoustic_offset')
        self.assertAlmostEqual(p['apparent_rate_ppm'],0)

    def test_motion_and_clock_skew_remain_indistinguishable(self):
        p=self.profile([12.+i*.02 for i in range(6)])
        self.assertEqual(p['interpretation'],'smooth_arrival_trend_clock_or_motion')
        self.assertAlmostEqual(p['apparent_rate_ppm'],2000)
        self.assertEqual(p['physical_cause'],'not_identifiable')
        self.assertFalse(p['permits_video_retiming'])
        self.assertFalse(p['permits_automatic_placement'])

    def test_switching_paths_are_not_declared_clock_drift(self):
        p=self.profile([12.,12.,12.,12.16,12.16,12.16])
        self.assertEqual(p['interpretation'],'changing_paths_or_conflicting_measurements')

    def test_ambiguous_peaks_do_not_establish_motion(self):
        p=self.profile([12.,12.02,12.04,12.06,12.08],False)
        self.assertEqual(p['interpretation'],'insufficient_precise_timing')
        self.assertEqual(p['precise_observations'],0)


if __name__=='__main__':unittest.main()
