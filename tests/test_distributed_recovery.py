import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0,str(Path(__file__).resolve().parents[1]/'internal/autosync'))
import distributed_recovery
import refinement


class DistributedRecoveryTest(unittest.TestCase):
    def test_budget_defers_search_without_using_offset_or_filename(self):
        records=[dict(id=str(i),name='untrusted-name',duration=d) for i,d in enumerate((15.,500.,20000.,200.))]
        layout=[dict(recording=i,status='unresolved' if i==0 else 'supported') for i in range(4)]
        pairs=[dict(reference=r,query=0,candidates=[dict(envelope_score=score,offset=987654)]) for r,score in ((1,.3),(2,.5),(3,.2))]
        jobs,deferred=distributed_recovery.schedule(records,layout,pairs,seconds_budget=600,pair_budget=8)
        self.assertEqual(jobs,[dict(reference=1,query=0)])
        self.assertEqual({p['reference'] for p in deferred},{2,3})
        self.assertTrue(all('offset' not in p for p in jobs))

    def test_discovery_does_not_earn_corroboration_or_discard_competitors(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            report=dict(pairs=[dict(reference=0,query=1,candidates=[dict(offset=o,score=.8) for o in (12.,22.,32.)])])
            (root/'distributed-discovery.json').write_text(json.dumps(report))
            reservoir=refinement.candidate_reservoir([dict(duration=60),dict(duration=8)],root)
            self.assertEqual([p['offset'] for p in reservoir[0]['candidates']],[12.,22.,32.])
            self.assertTrue(all(p['prior_strength']==0 for p in reservoir[0]['candidates']))


if __name__=='__main__':unittest.main()
