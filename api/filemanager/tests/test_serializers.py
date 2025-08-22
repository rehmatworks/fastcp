from django.test import SimpleTestCase, override_settings
from api.filemanager.serializers import FileListSerializer
from rest_framework.exceptions import ValidationError
import tempfile
import os
from urllib.parse import quote


class FileListSerializerTests(SimpleTestCase):
    @override_settings(FILE_MANAGER_ROOT='/tmp')
    def test_validate_path_accepts_url_encoded_path(self):
        # Create a temporary directory under a temp root
        tmp_root = tempfile.mkdtemp(prefix='fm_root_')
        try:
            # Ensure settings.FILE_MANAGER_ROOT points to tmp_root
            with override_settings(FILE_MANAGER_ROOT=tmp_root):
                subdir = os.path.join(tmp_root, 'sub')
                os.makedirs(subdir, exist_ok=True)
                # URL-encode the path as a client might
                encoded = quote(subdir)
                serializer = FileListSerializer()
                # Should not raise and should return decoded path
                result = serializer.validate_path(encoded)
                self.assertEqual(result, subdir)
        finally:
            # Cleanup
            if os.path.exists(tmp_root):
                try:
                    # remove created subdir and tmp_root
                    os.rmdir(os.path.join(tmp_root, 'sub'))
                    os.rmdir(tmp_root)
                except Exception:
                    pass

    @override_settings(FILE_MANAGER_ROOT='/tmp')
    def test_validate_path_rejects_nonexistent(self):
        with override_settings(FILE_MANAGER_ROOT='/tmp'):
            serializer = FileListSerializer()
            nonexist = '/this/path/does/not/exist'
            with self.assertRaises(ValidationError):
                serializer.validate_path(nonexist)
