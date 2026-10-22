import json
import os
from pathlib import Path
import tempfile
import unittest

from team_folders import FolderStore, LAYOUT


class FolderTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / '.mist-storage-layout.json').write_text(json.dumps({'layout': LAYOUT}))
        self.store = FolderStore(self.root, os.getuid(), os.getgid())
        self.team = 'team-aaaaaaaaaaaaaaaa'
        self.body = {'id': self.team, 'storage_gib': 200, 'model_storage_gib': 500}

    def request(self, body):
        path = self.store.requests / f'{self.team}.json'
        path.write_text(json.dumps(body))
        return path

    def test_plain_folders_preserve_files_and_allow_lower_allowances(self):
        ready = self.store.provision(self.body)
        data = self.root / 'teams' / self.team / 'datasets'
        models = data.parent / 'results'
        (data / 'common' / 'tiny.csv').write_bytes(b'1,2\n')
        (models / 'common' / 'model.bin').write_bytes(b'weights')
        lower = {**self.body, 'storage_gib': 1, 'model_storage_gib': 1}
        updated = self.store.provision(lower)
        self.assertEqual(ready['layout'], LAYOUT)
        self.assertEqual(updated['dataset_used_bytes'], 4)
        self.assertEqual(updated['model_used_bytes'], 7)
        self.assertEqual(updated['storage_gib'], 1)
        self.assertEqual(data.stat().st_dev, models.stat().st_dev)
        self.assertFalse(list(self.root.rglob('*.img')))

    def test_usage_does_not_follow_links_or_double_count_hardlinks(self):
        self.store.provision(self.body)
        data = self.root / 'teams' / self.team / 'datasets'
        (data / 'common' / 'data').write_bytes(b'123')
        os.link(data / 'common' / 'data', data / 'common' / 'alias')
        (data / 'common' / 'outside').symlink_to('/etc')
        (data / 'common' / 'file-link').symlink_to('/etc/passwd')
        self.assertEqual(FolderStore.usage(data), 3)

    def test_unsafe_paths_fail_and_do_not_change_target(self):
        outside = self.root / 'outside'
        outside.mkdir(mode=0o700)
        (self.root / 'teams' / self.team).symlink_to(outside)
        with self.assertRaises(ValueError):
            self.store.provision(self.body)
        self.assertEqual(list(outside.iterdir()), [])

    def test_requests_reject_symlinks_invalid_schema_ids_and_allowances(self):
        path = self.request(self.body)
        self.assertEqual(self.store.request(path), self.body)
        for body in [dict(self.body, storage_gib=True), dict(self.body, model_storage_gib=0),
                     dict(self.body, id='../outside'), dict(self.body, extra='unexpected')]:
            self.request(body)
            with self.assertRaises(ValueError):
                self.store.request(path)
        path.unlink()
        target = self.root / 'request.json'
        target.write_text(json.dumps(self.body))
        path.symlink_to(target)
        with self.assertRaises(ValueError):
            self.store.request(path)

    def test_reconciliation_publishes_errors_and_recovers(self):
        self.request(self.body)
        cache = {}
        self.store.reconcile(cache)
        path = self.store.status / f'{self.team}.json'
        self.assertEqual(json.loads(path.read_text())['state'], 'Ready')
        results = self.root / 'teams' / self.team / 'results'
        results.rename(results.with_name('retained'))
        results.symlink_to('/tmp')
        self.store.reconcile(cache)
        self.assertEqual(json.loads(path.read_text())['state'], 'Error')
        results.unlink()
        results.with_name('retained').rename(results)
        self.store.reconcile(cache)
        self.assertEqual(json.loads(path.read_text())['state'], 'Ready')

    def test_uninitialized_root_is_refused(self):
        (self.root / '.mist-storage-layout.json').write_text('{"layout":"old-image-layout"}')
        with self.assertRaises(ValueError):
            FolderStore(self.root, os.getuid(), os.getgid())


if __name__ == '__main__':
    unittest.main()
