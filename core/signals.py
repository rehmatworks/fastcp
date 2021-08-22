import django.dispatch
from core.utils import php

# This signal will be sent when PHP version
# of a website is updated.
php_updated = django.dispatch.Signal()

def update_php_conf(sender, **kwargs):
    """Update PHP conf.
    
    Update the PHP-FPM pool configuration for the specified website.
    """
    php.update_php_conf(sender)
php_updated.connect(update_php_conf, dispatch_uid='update-php-conf')