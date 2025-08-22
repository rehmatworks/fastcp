from django.test import SimpleTestCase
from api.filemanager.services.file_upload import FileUploadService
from types import SimpleNamespace
from unittest.mock import patch


class DummyResponse:
    def __init__(self, content=b"ok", status_code=200):
        self._content = content
        self.status_code = status_code

    def iter_content(self, chunk_size=1024):
        # yield the whole content in one chunk
        yield self._content

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        return False


class TestFileUploadService(SimpleTestCase):
    def test_remote_upload_writes_file(self):
        import tempfile
        from pathlib import Path

        with tempfile.TemporaryDirectory() as tmpdir:
            dest_dir = Path(tmpdir) / "uploads"
            dest_dir.mkdir()

            request = SimpleNamespace(user=SimpleNamespace(username="tester"))
            svc = FileUploadService(request)

            # Patch instance methods temporarily
            orig_is_allowed = FileUploadService.is_allowed
            orig_fix_ownership = FileUploadService.fix_ownership
            try:
                def always_allowed(self, path, user):
                    return True

                def noop_fix_ownership(self, path):
                    return None

                FileUploadService.is_allowed = always_allowed
                FileUploadService.fix_ownership = noop_fix_ownership

                remote_url = "http://example.com/file.txt"
                validated = {"path": str(dest_dir), "remote_url": remote_url}

                patch_target = (
                    "api.filemanager.services.file_upload.open_url"
                )
                dummy = DummyResponse(b"content", 200)
                with patch(patch_target, return_value=dummy):
                    ok = svc.remote_upload(validated)

                self.assertTrue(ok)
                created = dest_dir / "file.txt"
                self.assertTrue(created.exists())
                self.assertEqual(created.read_bytes(), b"content")
            finally:
                FileUploadService.is_allowed = orig_is_allowed
                FileUploadService.fix_ownership = orig_fix_ownership
