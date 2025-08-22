import django.dispatch
from django.db.models.signals import post_save, pre_delete
from django.dispatch import receiver

from core.models import Website, User, Database
from core.utils import system as fcpsys
from core.utils import filesystem


# Module-level signals
update_php = django.dispatch.Signal()
domains_updated = django.dispatch.Signal()
restart_services = django.dispatch.Signal()
reload_services = django.dispatch.Signal()
create_db = django.dispatch.Signal()
create_user = django.dispatch.Signal()
install_wp = django.dispatch.Signal()


def update_php_handler(sender, **kwargs):
    """Update PHP-FPM pool configuration for the website."""
    filesystem.delete_fpm_conf(sender)
    new_version = kwargs.get("new_version")
    if new_version:
        sender.php = new_version
        sender.save()
        filesystem.generate_fpm_conf(sender)


update_php.connect(update_php_handler, dispatch_uid="update-php-conf")


def install_wp_handler(sender, **kwargs):
    """Install WordPress on the given website."""
    fcpsys.setup_wordpress(website=sender, **kwargs)


install_wp.connect(install_wp_handler, dispatch_uid="install-wp")


def domains_updated_handler(sender, **kwargs):
    """Update vhost configuration when a website's domains change."""
    filesystem.create_nginx_vhost(sender)
    if not kwargs.get("only_nginx"):
        filesystem.create_apache_vhost(sender)


domains_updated.connect(domains_updated_handler, dispatch_uid="domains-updated")


@receiver(post_save, sender=Website)
def setup_website(sender, instance=None, created=False, **kwargs):
    """When a Website is created, provision its data."""
    if created:
        fcpsys.setup_website(instance)


@receiver(pre_delete, sender=Website)
def delete_website(sender, instance=None, **kwargs):
    """When a Website is deleted, remove its data from the system."""
    fcpsys.delete_website(instance)


def create_user_handler(sender, **kwargs):
    """When a User is created, enable and provision their data."""
    # sender here is expected to be a User instance
    sender.is_active = True
    sender.save()
    if not getattr(sender, "is_superuser", False):
        fcpsys.setup_user(sender, password=(kwargs.get("password") or ""))


create_user.connect(create_user_handler, dispatch_uid="create-user")


def restart_services_handler(sender=None, **kwargs):
    """Restart comma-separated services via systemctl."""
    services = (kwargs.get("services") or "").split(",")
    for service in services:
        service = service.strip()
        if not service:
            continue
        fcpsys.run_cmd(f"/usr/bin/systemctl restart {service}")


restart_services.connect(restart_services_handler, dispatch_uid="restart-services")


def reload_services_handler(sender=None, **kwargs):
    """Reload comma-separated services via systemctl."""
    services = (kwargs.get("services") or "").split(",")
    for service in services:
        service = service.strip()
        if not service:
            continue
        fcpsys.run_cmd(f"/usr/bin/systemctl reload {service}")


reload_services.connect(reload_services_handler, dispatch_uid="reload-services")


@receiver(pre_delete, sender=User)
def delete_user_data(sender=None, instance=None, **kwargs):
    fcpsys.delete_user_data(instance)


@receiver(pre_delete, sender=Database)
def delete_database(sender=None, instance=None, **kwargs):
    fcpsys.drop_db(instance)


def create_database_handler(sender, **kwargs):
    """Create a database in the system with an optional password."""
    fcpsys.create_database(sender, password=(kwargs.get("password") or ""))


create_db.connect(create_database_handler, dispatch_uid="create-db")
