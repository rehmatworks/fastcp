from django.db import models
from django.contrib.auth.models import User
from django.template.defaultfilters import slugify


class Notification(models.Model):
    """Notification model to store important alerts against users."""
    users = models.ManyToManyField(User, related_name='notifications')
    title = models.CharField(max_length=100)
    details = models.TextField(null=True, blank=True)
    url = models.URLField(null=True, blank=True)
    date = models.DateTimeField(auto_now_add=True)
    
    def __str__(self):
        return self.title


PHP_CHOICES = (('7.1', 'PHP 7.1'), ('7.2', 'PHP 7.2'), ('7.3', 'PHP 7.3'), ('7.4', 'PHP 7.4'), ('8.0', 'PHP 8.0'))
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