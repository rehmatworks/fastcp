from django.core.management.base import BaseCommand
from core.models import Website
from api.websites.services.ssl import FastcpSsl
from core.signals import domains_updated
from core.utils.system import ssl_expiring


class Command(BaseCommand):
    help = 'Activate SSL.'

    def handle(self, *args, **options):
        websites = Website.objects.all()

        self.stdout.write(
            self.style.WARNING(
                f'Attempting to get/renew SSL certificates for {websites.count()} websites.'
            )
        )
        for website in websites:
            if website.needs_ssl() or ssl_expiring(website):
                try:
                    fcp = FastcpSsl()
                    activated = fcp.get_ssl(website)

                    if activated:
                        self.stdout.write(self.style.SUCCESS(
                            f'[{website}] SSL certificate activated for website.'))
                        website.has_ssl = True
                        website.save()
                        domains_updated.send(sender=website)
                    else:
                        self.stdout.write(self.style.ERROR(
                            f'[{website}] SSL certificate cannot be activated for some or all domains.'))
                except Exception as e:
                    self.stdout.write(self.style.ERROR(f'{str(e)}'))
            else:
                self.stdout.write(self.style.SUCCESS(
                    f'Website {website} does not need an SSL.'))
