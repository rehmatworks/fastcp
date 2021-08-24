from django.core.management.base import BaseCommand
from core.models import Website
from api.websites.services.ssl import FastcpSsl


class Command(BaseCommand):
    help = 'Activate SSL.'

    def handle(self, *args, **options):
        websites = Website.objects.all()
        
        for website in websites:
            fcp = FastcpSsl()
            activated = fcp.get_ssl(website)
            
            if activated:
                self.stdout.write(self.style.SUCCESS(f'SSL certificate activated for website {website}'))
            else:
                self.stdout.write(self.style.ERROR(f'SSL certificate cannot be activated for {website}'))