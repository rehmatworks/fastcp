from django.db import models
from django.contrib.auth.models import User
from django.template.defaultfilters import slugify
from django.conf import settings
from api.websites.services.get_php_versions import PhpVersionListService


class Notification(models.Model):
    """Notification model to store important alerts against users."""
    users = models.ManyToManyField(User, related_name='notifications')
    title = models.CharField(max_length=100)
    details = models.TextField(null=True, blank=True)
    url = models.URLField(null=True, blank=True)
    date = models.DateTimeField(auto_now_add=True)
    
    def __str__(self):
        return self.title


php_versions = PhpVersionListService().get_php_versions()

PHP_CHOICES = ()
for v in php_versions:
    PHP_CHOICES += ((v, f'PHP {v}'),)
    
class Website(models.Model):
    """Website model holds the websites owned by users."""
    user = models.ForeignKey(User, related_name='websites', on_delete=models.CASCADE)
    label = models.CharField(max_length=100, unique=True)
    has_ssl = models.BooleanField(default=False)
    slug = models.SlugField(max_length=100, unique=True, null=True, blank=True)
    php = models.CharField(choices=PHP_CHOICES, max_length=20)
    created = models.DateTimeField(auto_now_add=True)
    
    def save(self, *args, **kwargs):
        """Always generate a slug on save."""
        if not self.slug:
            i = 0
            while True:
                slug = slugify(self.label)
                if i > 0:
                    slug = f'{slug}-{i}'
                if Website.objects.filter(slug=slug).count() == 0:
                    break
                i += 1
            self.slug = slug
        super(Website, self).save(*args, **kwargs)
    
    def __str__(self):
        return self.label
    
    @property
    def metadata(self) -> dict:
        """Returns the meta data for the website"""
        return {
            'path': f'{settings.FILE_MANAGER_ROOT}/{self.user.username}/apps/{self.slug}/public/',
            'user': self.user.username,
            'ip_addr': settings.SERVER_IP_ADDR
        }

class Domain(models.Model):
    """Domain model holds the domains associated to a website."""
    website = models.ForeignKey(Website, related_name='domains', on_delete=models.CASCADE)
    domain = models.CharField(max_length=100, unique=True)
    ssl = models.BooleanField(default=False)
    resolving_ip = models.GenericIPAddressField(null=True, blank=True)
    ssl_error = models.TextField(null=True, blank=True)
    ssl_retries = models.IntegerField(default=0)
    ssl_attempted = models.DateTimeField(null=True, blank=True)
    
    def __str__(self):
        return self.domain

class Database(models.Model):
    """Database model holds the MySQL databases."""
    name = models.SlugField(max_length=50, unique=True)
    username = models.SlugField(max_length=50, unique=True)
    created = models.DateTimeField(auto_now_add=True)
    
    def __str__(self):
        return self.name