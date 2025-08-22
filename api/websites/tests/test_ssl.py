from django.test import SimpleTestCase
from api.websites.services.ssl import FastcpSsl
from unittest.mock import patch


class DummyRes:
    def __init__(self, text="fastcp", status_code=200):
        self.text = text
        self.status_code = status_code

    def strip(self):
        return self.text.strip()


class TestFastcpSsl(SimpleTestCase):
    def test_is_resolving_true(self):
        # Avoid running __init__ which tries to create system dirs
        ssl = object.__new__(FastcpSsl)
        ssl.acc_key = None
        ssl.regr = None

        # patch requests.get used inside is_resolving
        patch_target = "api.websites.services.ssl.open_url"
        with patch(patch_target, return_value=DummyRes("fastcp", 200)):
            ok = ssl.is_resolving("example.com")

        self.assertTrue(ok)
