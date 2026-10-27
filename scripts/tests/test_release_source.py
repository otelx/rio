"""Exercise release authorization against real local Git histories; never publish."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]
GUARD = ROOT / "scripts/check-release-source.sh"


class ReleaseSourceTests(unittest.TestCase):
    def setUp(self):
        (ROOT / ".artifacts").mkdir(exist_ok=True)
        self.temporary = tempfile.TemporaryDirectory(prefix="issue-5-release-source-", dir=ROOT / ".artifacts")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.remote = self.root / "remote.git"
        self.repo = self.root / "checkout"
        self.repo.mkdir()
        self.git("init", "--bare", "--initial-branch=main", str(self.remote))
        self.git("init", "--initial-branch=main")
        self.git("config", "user.name", "Release policy test")
        self.git("config", "user.email", "release-test@example.invalid")
        self.git("config", "commit.gpgsign", "false")
        self.git("config", "tag.gpgsign", "false")
        self.git("config", "core.hooksPath", "/dev/null")
        self.git("config", "url." + str(self.remote) + ".insteadOf",
                 "https://github.com/otelx/rio.git")
        self.git("commit", "--allow-empty", "-m", "Reviewed source")
        self.commit = self.git("rev-parse", "HEAD").strip()
        self.version = "v0.1.0-alpha.999"
        self.git("tag", "-a", self.version, "-m", "Candidate")
        self.git("push", str(self.remote), "main", "refs/tags/" + self.version)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        gh = self.bin / "gh"
        gh.write_text('''#!/usr/bin/env python3
import os, sys
args = sys.argv[1:]
assert args[:3] == ['api', '--method', 'GET'], args
if os.environ.get('TEST_API_ERROR'):
    sys.exit(1)
if args[3] == 'repos/otelx/rio/actions/workflows/ci.yml/runs':
    for parameter in ['branch=main', 'event=push', 'status=completed',
                      'head_sha=' + os.environ['GITHUB_SHA']]:
        assert parameter in args, args
    print(os.environ.get('TEST_SUCCESSFUL_RUNS', '1'))
else:
    assert args[3] == 'repos/otelx/rio/commits/' + os.environ['GITHUB_SHA'] + '/pulls', args
    print(os.environ.get('TEST_MERGED_PRS', '1'))
''')
        gh.chmod(0o755)
        self.env = dict(os.environ, PATH=str(self.bin) + os.pathsep + os.environ["PATH"],
                        GITHUB_REPOSITORY="otelx/rio", GITHUB_REF_TYPE="tag",
                        GITHUB_EVENT_NAME="push", GITHUB_REF_NAME=self.version,
                        GITHUB_SHA=self.commit)

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo,
                                       stderr=subprocess.STDOUT, text=True)

    def guard(self, success=True, **env):
        result = subprocess.run(["bash", str(GUARD)], cwd=self.repo,
                                env=dict(self.env, **env), text=True,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        if success:
            self.assertEqual(result.returncode, 0, result.stdout)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout)
        return result.stdout

    def test_annotated_tag_on_main(self):
        self.assertIn("Verified release source", self.guard())

    def test_normal_and_other_prerelease_semver_tags(self):
        for version in ("v1.0.0", "v1.2.3-rc.1+build.7"):
            with self.subTest(version=version):
                self.git("tag", version)
                self.git("push", str(self.remote), "refs/tags/" + version)
                self.guard(GITHUB_REF_NAME=version)

    def test_detached_ancestor_of_main(self):
        self.git("commit", "--allow-empty", "-m", "Later reviewed change")
        self.git("push", str(self.remote), "main")
        self.git("checkout", "--detach", self.commit)
        self.guard()

    def test_lightweight_tag(self):
        self.git("tag", "-d", self.version)
        self.git("tag", self.version)
        self.git("push", "--force", str(self.remote), "refs/tags/" + self.version)
        self.guard()

    def test_unreviewed_side_branch(self):
        self.git("checkout", "-b", "unreviewed")
        self.git("commit", "--allow-empty", "-m", "Unreviewed change")
        commit = self.git("rev-parse", "HEAD").strip()
        self.git("tag", "-f", self.version)
        self.git("push", "--force", str(self.remote), "refs/tags/" + self.version)
        self.assertIn("not in protected main history", self.guard(False, GITHUB_SHA=commit))

    def test_workflow_commit_differs_from_tag(self):
        self.assertIn("must match", self.guard(False, GITHUB_SHA="f" * 40))

    def test_checkout_differs_from_tag(self):
        self.git("commit", "--allow-empty", "-m", "Different checkout")
        self.assertIn("must match", self.guard(False))

    def test_missing_failed_or_pending_main_ci(self):
        self.assertIn("no successful CI", self.guard(False, TEST_SUCCESSFUL_RUNS="0"))

    def test_api_error_fails_closed(self):
        self.guard(False, TEST_API_ERROR="1")

    def test_historical_direct_push_fails_closed(self):
        self.assertIn("not the result of a merged PR", self.guard(False, TEST_MERGED_PRS="0"))

    def test_invalid_api_result_fails_closed(self):
        self.guard(False, TEST_SUCCESSFUL_RUNS="null")

    def test_fetch_error_fails_closed(self):
        self.git("config", "url." + str(self.remote) + ".insteadOf",
                 "https://example.invalid/unused")
        self.git("config", "url." + str(self.root / "missing.git") + ".insteadOf",
                 "https://github.com/otelx/rio.git")
        self.guard(False)

    def test_wrong_repository_event_or_ref(self):
        for key, value in [("GITHUB_REPOSITORY", "other/fork"),
                           ("GITHUB_EVENT_NAME", "workflow_dispatch"),
                           ("GITHUB_REF_TYPE", "branch"),
                           ("GITHUB_REF_NAME", "v01.0.0"),
                           ("GITHUB_REF_NAME", "../main")]:
            with self.subTest(key=key, value=value):
                self.guard(False, **{key: value})


if __name__ == "__main__":
    unittest.main()
