import django.dispatch
from django.db.models.signals import (
    post_save, pre_delete
)
from django.dispatch import receiver
from core.models import Website, User, Database
from core.utils import system as fcpsys
from core.utils import filesystem



# This signal will be sent when PHP version
# of a website is updated.
update_php = django.dispatch.Signal()
domains_updated = django.dispatch.Signal()
restart_services = django.dispatch.Signal()
reload_services = django.dispatch.Signal()
create_db = django.dispatch.Signal()
create_user = django.dispatch.Signal()

def update_php_handler(sender, **kwargs):
    """Update PHP conf.
    
    Update the PHP-FPM pool configuration for the specified website.
    """
    filesystem.delete_fpm_conf(sender)
    old_version = sender.php
    new_version = kwargs.get('new_version')
    sender.php = new_version
    sender.save()
    filesystem.generate_fpm_conf(sender)

update_php.connect(update_php_handler, dispatch_uid='update-php-conf')


def domains_updated_handler(sender, **kwargs):
    """Update domains.
    
    Update the vhost conf files once a website's domains are updated.
    """
    # Create NGINX vhost
    filesystem.create_nginx_vhost(sender)
    
    if not kwargs.get('only_nginx'):
        filesystem.create_apache_vhost(sender)
    
domains_updated.connect(domains_updated_handler, dispatch_uid='domains-updated')

@receiver(post_save, sender=Website)
def setup_website(sender, instance=None, created=False, **kwargs):
    """Executes when a website is created at first. We will create the data."""
    if created:
        fcpsys.setup_website(instance)


@receiver(pre_delete, sender=Website)
def delete_website(sender, instance=None, **kwargs):
    """Executes when a website is deleted. We will clean the data then."""
    fcpsys.delete_website(instance)
    

def create_user_handler(sender, **kwargs):
    """Executes when a user is created at first. We will set the user is_active to True here as well
    we will create the user data directories."""
    sender.is_active = True
    sender.save()
    if not sender.is_superuser:
        fcpsys.setup_user(sender, password=kwargs.get('password'))
create_user.connect(create_user_handler, dispatch_uid='create-user')
    

def restart_services_handler(sender=None, **kwargs):
    """Restarts services. Expects the service names as a comma-separated string."""
    services = kwargs.get('services').split(',')
    for service in services:
        fcpsys.run_cmd(f'/usr/bin/systemctl restart {service}')

restart_services.connect(restart_services_handler, dispatch_uid='restart-services')


def reload_services_handler(sender=None, **kwargs):
    """Reload services. Expects the service names as a comma-separated string."""
    services = kwargs.get('services').split(',')
    for service in services:
        fcpsys.run_cmd(f'/usr/bin/systemctl restart {service}')

reload_services.connect(reload_services_handler, dispatch_uid='reload-services')


@receiver(pre_delete, sender=User)
def delete_user_data(sender=None, instance=None, **kwargs):
    fcpsys.delete_user_data(instance)

@receiver(pre_delete, sender=Database)
def delete_database(sender=None, instance=None, **kwargs):
    fcpsys.drop_db(instance)

def create_database_handler(sender, **kwargs):
    """Create the database in the system"""
    fcpsys.create_database(sender, password=kwargs.get('password'))
create_db.connect(create_database_handler)