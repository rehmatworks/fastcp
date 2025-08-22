from django.test import TestCase

from .models import Website, User

# We'll patch the network download performed in `core.utils.system` so the
# test is fast and deterministic. The test creates a tiny in-memory ZIP that
# mimics the `wordpress` archive (only the files the code needs).
import io
import zipfile
from unittest.mock import patch

from .utils import system as system_mod


class TestWordPressDeploy(TestCase):

    def setUp(self) -> None:
        u = User.objects.create(username='fasdd3')
        w = u.websites.create(label='test-website')
        w.domains.create(domain='example.com')

    def _make_wp_zip_bytes(self) -> bytes:
        # Build an in-memory zip with the minimal wordpress layout used by
        # `setup_wordpress`: a top-level `wordpress/` folder containing
        # `wp-config-sample.php` and at least one other file.
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, 'w', zipfile.ZIP_DEFLATED) as z:
            z.writestr('wordpress/wp-config-sample.php', """
<?php
// sample config file
define('DB_NAME', 'database_name_here');
define('DB_USER', 'username_here');
define('DB_PASSWORD', 'password_here');
// put your unique phrase here
?>
""")
            z.writestr('wordpress/index.php', "<?php echo 'ok'; ?>")

        return buf.getvalue()

    def test_wp_deploy(self):
        w = Website.objects.first()

        # Dummy response object that can be used as a context manager by
        # `with requests.get(...) as res:` in `setup_wordpress`.
        class DummyResponse:
            def __init__(self, content: bytes):
                self.content = content

            def __enter__(self):
                return self

            def __exit__(self, exc_type, exc, tb):
                return False

        wp_bytes = self._make_wp_zip_bytes()

        # Patch the requests.get used inside core.utils.system
        patch_target = (
            'core.utils.system.open_url'
        )
        with patch(patch_target, return_value=DummyResponse(wp_bytes)):
            system_mod.setup_wordpress(
                w,
                dbname='testdb',
                dbuser='testuser',
                dbpassword='testpass',
            )
