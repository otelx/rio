"""Source inventory checks bind preserved inputs to the final base packages."""
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location("alpine_sources", ROOT / "payload/scripts/alpine_sources.py")
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class AlpineSourcesTests(unittest.TestCase):
    def setUp(self):
        (ROOT / ".artifacts").mkdir(exist_ok=True)
        self.temporary=tempfile.TemporaryDirectory(prefix="issue-5-source-test-",dir=ROOT / ".artifacts")
        self.addCleanup(self.temporary.cleanup)
        self.root=Path(self.temporary.name)
        self.output=self.root / "sources.tar.gz"
        commit="a"*40
        data=b"upstream source bytes"
        checksum=hashlib.sha512(data).hexdigest()
        # A recipe is data: even executable shell text must never run.
        recipe=f'pkgver=1.0\npkgrel=0\nsource="source.tar.gz"\ntouch {self.root / "must-not-run"}\nsha512sums="\n{checksum}  source.tar.gz\n"\n'.encode()
        url=f"https://raw.githubusercontent.com/alpinelinux/aports/{commit}/main/example/APKBUILD"
        archive_url="https://distfiles.alpinelinux.org/distfiles/v3.24/source.tar.gz"
        self.inputs={url:recipe,archive_url:data}
        package={"P":"example","V":"1.0-r0","o":"example","c":commit,"L":"GPL-2.0-only"}
        self.database="\n".join(f"{k}:{v}" for k,v in package.items())+"\n\n"
        self.lock={"schema":1,"base_image":"alpine@sha256:"+"b"*64,"packages":[package],"origins":[{"origin":"example","commit":commit,"files":[
            {"name":"APKBUILD","url":url,"sha256":hashlib.sha256(recipe).hexdigest()},
            {"name":"source.tar.gz","url":archive_url,"sha256":hashlib.sha256(data).hexdigest(),"aports_sha512":checksum}
        ]}]}
        self.calls=[]

    def fetch(self,url):
        self.calls.append(url)
        return self.inputs[url]

    def bundle(self,lock=None,database=None,output=None):
        module.bundle(lock or self.lock,database or self.database,output or self.output,self.fetch,base_image=self.lock["base_image"])

    def test_complete_bundle_is_deterministic_and_preserves_source(self):
        self.bundle()
        second=self.root / "second.tar.gz"
        self.bundle(output=second)
        self.assertEqual(self.output.read_bytes(),second.read_bytes())
        self.assertFalse((self.root / "must-not-run").exists())
        with tarfile.open(self.output) as archive:
            name="example-"+"a"*40+"/source.tar.gz"
            self.assertEqual(archive.extractfile(name).read(),b"upstream source bytes")
            index=json.load(archive.extractfile("SOURCE_INPUTS.json"))
            self.assertEqual(index,self.lock)

    def test_changed_packages_or_base_reference_stop_before_download(self):
        with self.assertRaises(ValueError):self.bundle(database=self.database.replace("1.0-r0","1.1-r0"))
        with self.assertRaises(ValueError):module.bundle(self.lock,self.database,self.output,self.fetch,base_image="changed")
        self.assertEqual(self.calls,[])
        self.assertFalse(self.output.exists())

    def test_tampered_source_never_creates_a_bundle(self):
        url=next(u for u in self.inputs if u.endswith("source.tar.gz"))
        self.inputs[url]=b"replaced source"
        with self.assertRaisesRegex(ValueError,"SHA-256 mismatch"):self.bundle()
        self.assertFalse(self.output.exists())

    def test_recipe_version_and_aports_checksum_must_also_match(self):
        database=self.database.replace("1.0-r0","2.0-r0")
        lock=copy.deepcopy(self.lock)
        lock["packages"][0]["V"]="2.0-r0"
        with self.assertRaisesRegex(ValueError,"recipe version"):
            self.bundle(lock=lock,database=database)
        lock=copy.deepcopy(self.lock)
        lock["origins"][0]["files"][1]["aports_sha512"]="0"*128
        with self.assertRaisesRegex(ValueError,"APorts SHA-512"):
            self.bundle(lock=lock)
        self.assertFalse(self.output.exists())

    def test_missing_recipe_input_cannot_be_silently_omitted(self):
        lock=copy.deepcopy(self.lock)
        lock["origins"][0]["files"].pop()
        with self.assertRaisesRegex(ValueError,"every recipe checksum"):self.bundle(lock=lock)
        self.assertFalse(self.output.exists())

    def test_untrusted_urls_and_traversal_stop_before_download(self):
        for key,value in [("url","http://localhost/private"),("name","../escape"),("name",r"..\escape")]:
            lock=copy.deepcopy(self.lock)
            lock["origins"][0]["files"][0][key]=value
            with self.assertRaises(ValueError):self.bundle(lock=lock)
        self.assertEqual(self.calls,[])

    def test_existing_artifact_is_never_replaced(self):
        self.output.write_bytes(b"retain")
        with self.assertRaises(FileExistsError):self.bundle()
        self.assertEqual(self.output.read_bytes(),b"retain")
        self.assertEqual(self.calls,[])


if __name__=="__main__":unittest.main()
