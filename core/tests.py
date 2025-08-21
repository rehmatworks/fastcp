from django.test import TestCase
from .models import Website, User
from .utils.system import setup_wordpress

# Create your tests here.
class TestWordPressDeploy(TestCase):
    
    def setUp(self) -> None:
        u = User.objects.create(
            username='fasdd3'
        )
        w = u.websites.create(
            label='test-website'
        )
        w.domains.create(
            domain='example.com'
        )
    
    def test_wp_deploy(self):
        w = Website.objects.first()
        setup_wordpress(w)
