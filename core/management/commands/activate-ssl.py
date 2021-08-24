from django.core.management.base import BaseCommand
from core.models import Website
from api.websites.services.ssl import FastcpSsl


class Command(BaseCommand):
    help = 'Activate SSL.'

    def handle(self, *args, **options):
        websites = Website.objects.all()
        
        for website in websites:
            fcp = FastcpSsl()
            fcp.get_ssl(website)