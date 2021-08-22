import django.dispatch
from django.db.models.signals import (
    post_save, pre_delete
)
from django.dispatch import receiver
from core.models import Website
from core.utils import system as fcpsys
from core.utils import filesystem



# This signal will be sent when PHP version
# of a website is updated.
update_php = django.dispatch.Signal()
domains_updated = django.dispatch.Signal()

def update_php_conf(sender, **kwargs):
    """Update PHP conf.
    
    Update the PHP-FPM pool configuration for the specified website.
    """
    filesystem.delete_fpm_conf(sender)
    sender.php = kwargs.get('new_version')
    sender.save()
    filesystem.generate_fpm_conf(sender)

update_php.connect(update_php_conf, dispatch_uid='update-php-conf')


def update_domains(sender, **kwargs):
    """Update domains.
    
    Update the vhost conf files once a website's domains are updated.
    """
    # Create NGINX vhost
    filesystem.create_nginx_vhost(sender)
    
domains_updated.connect(update_domains, dispatch_uid='domains-updated')

@receiver(post_save, sender=Website)
def setup_website(sender, instance=None, created=False, **kwargs):
    if created:
        fcpsys.setup_website(instance)


@receiver(pre_delete, sender=Website)
def delete_website(sender, instance=None, **kwargs):
    fcpsys.delete_website(instance)