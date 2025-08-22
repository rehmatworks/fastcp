from django.test import SimpleTestCase
from unittest.mock import patch

from core.utils.http import open_url


class TestOpenUrl(SimpleTestCase):
    def test_open_url_raises_when_network_disabled(self):
        # Temporarily set the env var and ensure open_url refuses network access
        with patch.dict("os.environ", {"FASTCP_DISABLE_NETWORK": "1"}):
            with self.assertRaises(RuntimeError):
                open_url("http://example.com")
