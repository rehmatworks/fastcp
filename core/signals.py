import django.dispatch
from core.utils import php

# This signal will be sent when PHP version
# of a website is updated.
php_updated = django.dispatch.Signal()
domains_updated = django.dispatch.Signal()

def update_php_conf(sender, **kwargs):
    """Update PHP conf.
    
    Update the PHP-FPM pool configuration for the specified website.
    """
    php.update_php_conf(sender)
php_updated.connect(update_php_conf, dispatch_uid='update-php-conf')


def update_domains(sender, **kwargs):
    """Update domains.
    
    Update the vhost conf files once a website's domains are updated.
    """
    # To-do: Updae vhost files physically
    pass
domains_updated.connect(update_domains, dispatch_uid='domains-updated')