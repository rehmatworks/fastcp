from django.core.management.base import BaseCommand
from core.models import Website
from api.websites.services.ssl import FastcpSsl
from core.signals import domains_updated


class Command(BaseCommand):
    help = 'Activate SSL.'

    def handle(self, *args, **options):
        websites = Website.objects.all()
        
        for website in websites:
            if website.domains.filter(ssl=False).count():
                fcp = FastcpSsl()
                activated = fcp.get_ssl(website)
                
                if activated:
                    self.stdout.write(self.style.SUCCESS(f'SSL certificate activated for website {website}'))
                    website.has_ssl = True
                    website.save()
                    domains_updated.send(sender=website)
                else:
                    self.stdout.write(self.style.ERROR(f'SSL certificate cannot be activated for {website}'))
            else:
                self.stdout.write(self.style.SUCCESS('Website {website} does not need an SSL.'))