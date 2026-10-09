"""Run as host root in an isolated temporary directory; never uses live stores."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('volumes',Path(__file__).with_name('team_volumes.py'))
volumes=importlib.util.module_from_spec(spec);spec.loader.exec_module(volumes)

@unittest.skipUnless(os.geteuid()==0,'Ownership preservation requires root')
class MigrationTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory(prefix='mist-migration-test-')
        self.previous=volumes.ROOT
        volumes.ROOT=Path(self.tmp.name)
        self.team='team-aaaaaaaaaaaaaaaa'
        self.root=volumes.ROOT/'teams'/self.team
        (self.root/'members').mkdir(parents=True)
        self.source=self.root/'common'/'jobs'/'mist-test'
        self.source.mkdir(parents=True)
        (self.source/'weights.bin').write_bytes(b'EXISTING_WEIGHTS')
        (self.source/'weights.bin').chmod(0o600)
        os.symlink('/missing-outside-file',self.source/'escape')
        self.target=self.root/'.models'/'common'/'jobs'/'mist-test'
    def tearDown(self):
        volumes.ROOT=self.previous
        self.tmp.cleanup()
    def test_preserve_content_modes_ownership_symlinks_and_repeat(self):
        before=volumes.fingerprint(self.source)
        volumes.migrate_outputs(self.team)
        self.assertEqual(volumes.fingerprint(self.target),before)
        self.assertFalse(self.source.exists())
        volumes.migrate_outputs(self.team)
        self.assertEqual(volumes.fingerprint(self.target),before)
    def test_retry_after_atomic_publish_before_original_removal(self):
        shutil.copytree(self.source,self.target,symlinks=True)
        volumes.migrate_outputs(self.team)
        self.assertFalse(self.source.exists())
        self.assertEqual((self.target/'weights.bin').read_bytes(),b'EXISTING_WEIGHTS')
    def test_conflict_does_not_overwrite_either_copy(self):
        self.target.mkdir(parents=True)
        (self.target/'weights.bin').write_bytes(b'DIFFERENT_MODEL')
        with self.assertRaisesRegex(ValueError,'conflicting'): volumes.migrate_outputs(self.team)
        self.assertEqual((self.source/'weights.bin').read_bytes(),b'EXISTING_WEIGHTS')
        self.assertEqual((self.target/'weights.bin').read_bytes(),b'DIFFERENT_MODEL')
    def test_partial_stage_and_symlink_job_directory(self):
        stage=self.target.parent/('.mist-migration-'+self.source.name)
        stage.mkdir(parents=True)
        (stage/'partial').write_bytes(b'partial')
        volumes.migrate_outputs(self.team)
        self.assertFalse(stage.exists())
        self.assertEqual((self.target/'weights.bin').read_bytes(),b'EXISTING_WEIGHTS')
        os.symlink('/outside',self.source)
        with self.assertRaisesRegex(ValueError,'unsafe legacy job'): volumes.migrate_outputs(self.team)
    def test_member_scope_remains_writable_by_api_owner(self):
        member=self.root/'members'/'member-hash'
        old=member/'jobs'/'mist-member'
        old.mkdir(parents=True)
        (old/'weights.bin').write_bytes(b'MEMBER_WEIGHTS')
        before=volumes.fingerprint(old)
        volumes.migrate_outputs(self.team)
        scope=self.root/'.models'/'members'/'member-hash'
        self.assertEqual(scope.stat().st_uid,65532)
        self.assertEqual(scope.stat().st_mode & 0o777,0o755)
        self.assertEqual(volumes.fingerprint(scope/'jobs'/'mist-member'),before)
        # Restart repairs a formerly root-owned, inaccessible parent as well.
        os.chown(scope,0,0);os.chmod(scope,0o700)
        volumes.migrate_outputs(self.team)
        self.assertEqual(scope.stat().st_uid,65532)
        self.assertEqual(scope.stat().st_mode & 0o777,0o755)
    def test_manifest_schema_owner_and_numeric_bounds(self):
        request=volumes.ROOT/(self.team+'.json')
        for size in [False,0,-1,volumes.budget()+1]:
            request.write_text(json.dumps({'id':self.team,'storage_gib':1,'model_storage_gib':size}));os.chown(request,65532,65532)
            with self.assertRaises(ValueError):volumes.manifest(request)
        request.write_text(json.dumps({'id':self.team,'storage_gib':1,'model_storage_gib':2}));os.chown(request,65532,65532)
        self.assertEqual(volumes.manifest(request)['model_storage_gib'],2)

if __name__=='__main__':unittest.main()
