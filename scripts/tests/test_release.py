"""Publication boundary regressions; all remote commands are isolated fakes."""

import importlib.util
import gzip
import hashlib
import io
import json
import os
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def module(name):
    spec = importlib.util.spec_from_file_location(name, ROOT / 'scripts' / f'{name}.py')
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


metadata = module('release_metadata')
notices = module('image_notices')
licenses = module('check_license_review')


class VersionTests(unittest.TestCase):
    def test_stable_and_prerelease(self):
        for version in ('v0.0.0', 'v1.2.3', 'v12.20.30+build.01'):
            self.assertFalse(metadata.version_info(version)['prerelease'])
        for version in ('v1.2.3-alpha.0', 'v1.2.3-rc.12', 'v1.2.3-beta.x+build.01'):
            self.assertTrue(metadata.version_info(version)['prerelease'])
        self.assertEqual(metadata.version_info('v1.2.3+build.01')['image_tag'], 'v1.2.3_build.01')

    def test_malformed_semver(self):
        for version in ('1.2.3', 'v01.2.3', 'v1.02.3', 'v1.2.03', 'v1.2', 'v1.2.3-01',
                        'v1.2.3-alpha.01', 'v1.2.3-', 'v1.2.3+', 'v1.2.3-a..b',
                        'v1.2.3-a_b', 'v1.2.3\n', 'v1.2.3-' + 'x' * 120):
            with self.subTest(version=version), self.assertRaises(ValueError):
                metadata.version_info(version)

    def test_version_specific_notes(self):
        text = '## Unreleased\n\nFuture\n\n## v1.2.3\n\nShip this.\n\n## v1.2.2\n\nOld.'
        self.assertEqual(metadata.release_notes(text, 'v1.2.3'), 'Ship this.')
        for text in ('## Unreleased\nMissing', '## v1.2.3\n\n', '## v1.2.3\nA\n## v1.2.3\nB'):
            with self.assertRaises(ValueError):
                metadata.release_notes(text, 'v1.2.3')


class NoticeTests(unittest.TestCase):
    def test_go_source_changes_invalidate_redistribution_approval(self):
        (ROOT / '.artifacts').mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='issue-5-review-', dir=ROOT / '.artifacts') as directory:
            root = Path(directory)
            source = root / 'internal/operator/example.go'
            source.parent.mkdir(parents=True)
            source.write_text('package operator\n')
            subprocess.run(['git','init','-q',directory],check=True)
            subprocess.run(['git','-C',directory,'add','internal'],check=True)
            before = licenses.input_digest(root)
            source.write_text('package operator\nimport _ "example.com/newly-active-dependency"\n')
            after = licenses.input_digest(root)
            self.assertNotEqual(before,after)
            review = {'status':'approved','inputs_sha256':before,'reviewer':'maintainer','evidence':'review-link'}
            with self.assertRaises(ValueError):
                licenses.check_review(review,after)

    def test_extract_inventory_and_reject_traversal(self):
        for unsafe in (False, True):
            with tempfile.TemporaryDirectory() as directory, io.BytesIO() as data:
                with tarfile.open(fileobj=data, mode='w') as archive:
                    for name in ('licenses/LICENSE', 'dist/npm/LICENSE.md', '../NOTICE' if unsafe else 'licenses/go/NOTICE'):
                        info = tarfile.TarInfo(name)
                        info.size = 7
                        archive.addfile(info, io.BytesIO(b'license'))
                data.seek(0)
                with tarfile.open(fileobj=data) as archive:
                    if unsafe:
                        with self.assertRaises(ValueError):
                            notices.collect(archive, Path(directory))
                    else:
                        notices.collect(archive, Path(directory))
                        inventory = json.loads((Path(directory) / 'inventory.json').read_text())
                        self.assertEqual(len(inventory), 3)
                        self.assertTrue(all(len(p['sha256']) == 64 for p in inventory))

    def test_pending_stale_and_incomplete_review_rejected(self):
        for review in ({'status': 'pending'}, {'status': 'approved', 'inputs_sha256': 'stale'},
                       {'status': 'approved', 'inputs_sha256': 'current'}):
            with self.assertRaises(ValueError):
                licenses.check_review(review, 'current')
        licenses.check_review({'status': 'approved', 'inputs_sha256': 'current',
                               'reviewer': 'maintainer', 'evidence': 'review-link'}, 'current')


FAKE_DOCKER = r'''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
args=sys.argv[1:]; state=Path(os.environ['FAKE_STATE'])
data=json.loads(state.read_text())
with Path(os.environ['FAKE_LOG']).open('a') as log: log.write(json.dumps(args)+'\n')
def finish(code=0):
 state.write_text(json.dumps(data)); sys.exit(code)
def digest(ref): return 'sha256:'+('a' if ref.endswith('-amd64') else 'b' if ref.endswith('-arm64') else 'c')*64
if args[:3]==['buildx','imagetools','inspect']:
 ref=next(a for a in args[3:] if a.startswith(('localhost:','ghcr.io/')))
 if ref not in data.get('tags',{}):
  print(data.get('lookup_error', f'ERROR: {ref}: not found'),file=sys.stderr); finish(1)
 if '--raw' in args: print(json.dumps(data['index']))
 else: print(data['tags'][ref])
 finish()
if args[:2]==['image','inspect']:
 ref=args[-1]
 if '.Id' in args[3]: print(digest(ref))
 else: print('linux/'+ ('arm64' if ref.endswith('-arm64') else 'amd64'))
 finish()
if args[0]=='push': data.setdefault('tags',{})[args[1]]=digest(args[1]); finish()
if args[:3]==['buildx','imagetools','create']:
 manifests=[{'digest':'sha256:'+c*64,'platform':{'os':'linux','architecture':a},'mediaType':'application/vnd.oci.image.manifest.v1+json','size':123} for a,c in [('amd64','a'),('arm64','b')]]
 data['index']={'schemaVersion':2,'manifests':manifests}
 if '--dry-run' in args: print(json.dumps(data['index']))
 else: data.setdefault('tags',{})[args[args.index('--tag')+1]]=digest('index')
 finish()
if args[0] in ('tag','load'): finish()
raise SystemExit('Unexpected docker command: '+str(args))
'''


class PublicationFixture(unittest.TestCase):
    def setUp(self):
        (ROOT / '.artifacts').mkdir(exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(prefix='issue-5-release-test-', dir=ROOT / '.artifacts')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        self.state = self.root / 'state.json'
        self.state.write_text('{}')
        self.log = self.root / 'calls.jsonl'
        docker = self.bin / 'docker'
        docker.write_text(FAKE_DOCKER)
        docker.chmod(0o755)
        self.env = dict(os.environ, PATH=f'{self.bin}:{os.environ["PATH"]}', FAKE_STATE=str(self.state), FAKE_LOG=str(self.log))

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()] if self.log.exists() else []

    def publish(self):
        return subprocess.run([str(ROOT / 'scripts/publish-images.sh'), 'localhost:5000/issue-5', 'v1.2.3'],
                              env=self.env, text=True, capture_output=True)


class PublicationTests(PublicationFixture):
    def test_every_existing_reference_blocks_all_pushes(self):
        for image in ('operator', 'instrumentation'):
            for suffix in ('', '-amd64', '-arm64'):
                with self.subTest(image=image, suffix=suffix):
                    self.state.write_text(json.dumps({'tags': {f'localhost:5000/issue-5/rio/{image}:v1.2.3{suffix}': 'sha256:'+'a'*64}}))
                    self.log.unlink(missing_ok=True)
                    result = self.publish()
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn('Refusing to replace', result.stderr)
                    self.assertFalse(any(c[0] in ('tag', 'push') for c in self.calls()))

    def test_ambiguous_registry_failures_block_all_pushes(self):
        for error in ('unauthorized', 'denied: not found', 'HTTP 429', 'HTTP 500', 'connection refused', 'TLS failure', 'unexpected EOF'):
            with self.subTest(error=error):
                self.state.write_text(json.dumps({'lookup_error': error}))
                self.log.unlink(missing_ok=True)
                result = self.publish()
                self.assertNotEqual(result.returncode, 0)
                self.assertIn('Cannot establish', result.stderr)
                self.assertFalse(any(c[0] in ('tag', 'push') for c in self.calls()))

    def test_publish_exact_sources_and_digest_report_then_refuse_retry(self):
        report = self.root / 'digests'
        self.env['IMAGE_DIGESTS_FILE'] = str(report)
        result = self.publish()
        self.assertEqual(result.returncode, 0, result.stderr)
        lines = report.read_text().splitlines()
        self.assertEqual(len(lines), 6)
        tags = [c for c in self.calls() if c[0] == 'tag']
        self.assertEqual(len(tags), 4)
        self.assertEqual({c[1] for c in tags}, {f'ghcr.io/otelx/rio/{i}:dev-{a}' for i in ('operator','instrumentation') for a in ('amd64','arm64')})
        self.assertFalse(any(c[0] in ('build', 'buildx') and (c[0]=='build' or c[1]=='build') for c in self.calls()))
        self.log.unlink()
        result = self.publish()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(c[0] in ('tag', 'push') for c in self.calls()))



FAKE_GH = r'''#!/usr/bin/env python3
import json,os,sys
from pathlib import Path
args=sys.argv[1:]; state=Path(os.environ['FAKE_STATE']); data=json.loads(state.read_text())
with Path(os.environ['FAKE_LOG']).open('a') as log: log.write(json.dumps(['gh']+args)+'\n')
if args[0]=='api':
 if 'repos/otelx/rio/immutable-releases' in args:
  print('false' if data.get('settings_disabled') else 'true'); sys.exit(0)
 if any('releases/tags/' in a for a in args): print('true'); sys.exit(0)
 if data.get('api_failure'): raise SystemExit('API failure')
 if any('/pulls' in a or '/actions/workflows/' in a for a in args): print('1'); sys.exit(0)
 print(json.dumps([data.get('releases',[])])); sys.exit(0)
if args[:2]==['release','create']:
 if data.get('reserve_failure'): raise SystemExit('Reservation failed')
 data.setdefault('releases',[]).append({'id':123,'tag_name':args[2],'draft':True})
 state.write_text(json.dumps(data)); sys.exit(0)
if args[:2]==['attestation','verify']:
 if data.get('attestation_failure'): raise SystemExit('Attestation verification failed')
 sys.exit(0)
if args[:2]==['release','upload']:
 if data.get('upload_failure'): raise SystemExit('Upload failed')
 sys.exit(0)
if args[:2]==['release','edit']:
 if '--draft=false' in args:
  data['releases'][0]['draft']=False; state.write_text(json.dumps(data))
 sys.exit(0)
raise SystemExit('Unexpected gh invocation: '+str(args))
'''


class DraftFixture(PublicationFixture):
    def prepare_fixture(self, version="v1.2.3"):
        shutil.copytree(ROOT / 'scripts', self.root / 'scripts', ignore=shutil.ignore_patterns('__pycache__'))
        gh = self.bin / 'gh'
        gh.write_text(FAKE_GH)
        gh.chmod(0o755)
        for name in ('scripts/install.sh', 'config/install.yaml', 'config/example.yaml',
                     'examples/quickstart/policy.yaml', 'docs/quickstart.md', 'docs/configuration.md'):
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            if name == 'docs/quickstart.md':
                path.write_text(f'git clone --branch {version} https://github.com/otelx/rio.git\n')
            else:
                image = 'operator' if name in ('scripts/install.sh','config/install.yaml') else 'instrumentation'
                path.write_text(f'ghcr.io/otelx/rio/{image}:'+metadata.version_info(version)['image_tag']+'\n')
        (self.root / 'CHANGELOG.md').write_text(f'## Unreleased\nFuture\n\n## {version}\nStable changes.\n\n## v1.2.2\nOld changes.\n')
        for arch in ('amd64','arm64'):
            directory = self.root / f'.artifacts/release/tested-release-images-{arch}'
            directory.mkdir(parents=True)
            content = gzip.compress(b'tested image archive')
            (directory / 'release-images.tar.gz').write_bytes(content)
            (directory / 'archive.sha256').write_text(hashlib.sha256(content).hexdigest()+'  release-images.tar.gz\n')
            for image in ('operator','instrumentation'):
                (directory / f'{image}.id').write_text('sha256:'+('a' if arch=='amd64' else 'b')*64+'\n')
                (directory / f'{image}.spdx.json').write_text('{"spdxVersion":"SPDX-2.3","packages":[{}]}')
                notices = directory / 'notices' / image
                notices.mkdir(parents=True)
                (notices / 'inventory.json').write_text('[]')
        subprocess.run(['git','init','-q','--initial-branch=main',str(self.root)],check=True)
        subprocess.run(['git','-C',str(self.root),'config',f'url.{self.root}.insteadOf','https://github.com/otelx/rio.git'],check=True)
        subprocess.run(['git','-C',str(self.root),'add','scripts','config','examples','docs','CHANGELOG.md'],check=True)
        subprocess.run(['git','-C',str(self.root),'-c','commit.gpgsign=false','-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-qm','Fixture'],check=True)
        subprocess.run(['git','-C',str(self.root),'-c','tag.gpgsign=false','tag',version],check=True)
        sha = subprocess.check_output(['git','-C',str(self.root),'rev-parse','HEAD'],text=True).strip()
        self.env.update(GITHUB_ACTIONS='true',GITHUB_EVENT_NAME='push',GITHUB_REPOSITORY='otelx/rio',GITHUB_REF_TYPE='tag',GITHUB_REF_NAME=version,GITHUB_SHA=sha,GITHUB_RUN_ID='12345')

    def prepare(self):
        return subprocess.run([str(self.root / 'scripts/release.sh')],env=self.env,text=True,capture_output=True)


class DraftReservationTests(DraftFixture):
    def test_prerelease_reference_cannot_satisfy_stable_release_preflight(self):
        self.prepare_fixture()
        for name in ('config/install.yaml','docs/quickstart.md'):
            path = self.root / name
            original = path.read_text()
            path.write_text(original.replace('v1.2.3','v1.2.3-alpha.1'))
            result = self.prepare()
            self.assertNotEqual(result.returncode,0,result.stdout)
            self.assertFalse(self.calls(), 'Reference mismatches must stop before remote operations')
            path.write_text(original)

    def test_prerelease_with_build_metadata_uses_prerelease_flag_and_mapped_image_tags(self):
        self.prepare_fixture('v1.2.3-rc.1+build.01')
        result = self.prepare()
        self.assertEqual(result.returncode,0,result.stderr)
        reservation = next(c for c in self.calls() if c[:3]==['gh','release','create'])
        self.assertIn('--prerelease',reservation)
        pushes = [c[1] for c in self.calls() if c[0]=='push']
        self.assertTrue(all('v1.2.3-rc.1_build.01-' in ref for ref in pushes))

    def test_existing_draft_published_release_and_api_failure_prevent_push(self):
        self.prepare_fixture()
        for state in ({'releases':[{'tag_name':'v1.2.3','draft':True}]},
                      {'releases':[{'tag_name':'v1.2.3','draft':False}]}, {'api_failure':True}):
            with self.subTest(state=state):
                self.state.write_text(json.dumps(state)); self.log.unlink(missing_ok=True)
                result = self.prepare()
                self.assertNotEqual(result.returncode,0)
                self.assertFalse(any(c[0] in ('tag','push') for c in self.calls()))
                self.assertFalse(any(c[:3]==['gh','release','create'] for c in self.calls()))

    def test_failed_reservation_prevents_push(self):
        self.prepare_fixture()
        self.state.write_text('{"reserve_failure":true}')
        result = self.prepare()
        self.assertNotEqual(result.returncode,0,result.stdout)
        self.assertFalse(any(c[0] in ('tag','push') for c in self.calls()))

    def test_stable_draft_reserved_before_push_and_source_includes_notices(self):
        self.prepare_fixture()
        result = self.prepare()
        self.assertEqual(result.returncode,0,result.stderr)
        calls = self.calls()
        reservation = next(i for i,c in enumerate(calls) if c[:3]==['gh','release','create'])
        first_push = next(i for i,c in enumerate(calls) if c[0]=='push')
        self.assertLess(reservation,first_push)
        self.assertIn('--draft',calls[reservation]); self.assertNotIn('--prerelease',calls[reservation])
        self.assertFalse(any(c[:3]==['gh','release','edit'] for c in calls))
        output = self.root / '.artifacts/release/prepared'
        notes = (output / 'notes.md').read_text()
        self.assertIn('Stable changes.',notes); self.assertNotIn('Future',notes); self.assertNotIn('Old changes.',notes)
        self.assertIn('ghcr.io/otelx/rio/operator@sha256:',notes)
        with tarfile.open(output / 'rio-v1.2.3.tar.gz') as archive:
            self.assertIn('rio-v1.2.3/third_party/arm64/operator/inventory.json',archive.getnames())
        self.log.unlink()
        self.assertNotEqual(self.prepare().returncode,0)
        self.assertFalse(any(c[0] in ('tag','push') for c in self.calls()))


class FinalizationTests(DraftFixture):
    def prepared(self):
        self.prepare_fixture()
        result = self.prepare()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.output = self.root / '.artifacts/release/prepared'
        self.info = json.loads((self.output / 'release.json').read_text())
        for arch in ('amd64','arm64'):
            (self.output / f'verified-{arch}.json').write_text(json.dumps(dict(self.info,verified_arch=arch)))
        (self.output / 'source.spdx.json').write_text('{"spdxVersion":"SPDX-2.3","packages":[{}]}')
        (self.output / 'source.sigstore.json').write_text('{}')
        self.env['RELEASE_SETTINGS_TOKEN'] = 'fixture-read-only'
        self.log.unlink()

    def finalize(self):
        return subprocess.run([str(self.root / 'scripts/finalize-release.sh')],env=self.env,text=True,capture_output=True)

    def assert_unpublished(self, result):
        self.assertNotEqual(result.returncode,0,result.stdout)
        self.assertFalse(any(c[:3]==['gh','release','edit'] and '--draft=false' in c for c in self.calls()))

    def test_missing_wrong_arch_or_changed_artifact_evidence_blocks_publication(self):
        self.prepared()
        path = self.output / 'verified-arm64.json'
        path.unlink()
        self.assert_unpublished(self.finalize())
        for changes in ({'verified_arch':'amd64'}, {'verified_arch':'arm64','operator':'sha256:other'},
                        {'verified_arch':'arm64','run_id':'previous'}):
            path.write_text(json.dumps(dict(self.info,**changes)))
            self.assert_unpublished(self.finalize())

    def test_settings_attestation_upload_and_draft_changes_block_publication(self):
        self.prepared()
        base = self.state.read_text()
        for key in ('settings_disabled','attestation_failure','upload_failure','draft_changed','tag_changed','partial_assets'):
            with self.subTest(failure=key):
                data = json.loads(base)
                if key=='draft_changed': data['releases'][0]['id']=999
                elif key=='tag_changed': data['tags'][next(iter(data['tags']))]='sha256:'+'f'*64
                elif key=='partial_assets': data['releases'][0]['assets']=[{'name':'already-uploaded.tar.gz'}]
                else: data[key]=True
                self.state.write_text(json.dumps(data)); self.log.unlink(missing_ok=True)
                self.assert_unpublished(self.finalize())

    def test_complete_evidence_attaches_assets_before_publication(self):
        self.prepared()
        result = self.finalize()
        self.assertEqual(result.returncode,0,result.stderr)
        calls = self.calls()
        upload = next(i for i,c in enumerate(calls) if c[:3]==['gh','release','upload'])
        publish = next(i for i,c in enumerate(calls) if c[:3]==['gh','release','edit'] and '--draft=false' in c)
        self.assertLess(upload,publish)
        self.assertNotIn('--clobber',calls[upload])
        checksums = (self.output / 'SHA256SUMS').read_text()
        for name in ('IMAGE_DIGESTS','source.spdx.json','rio-v1.2.3.tar.gz','verified-arm64.json'):
            self.assertIn(name,checksums)


if __name__ == '__main__':
    unittest.main()
