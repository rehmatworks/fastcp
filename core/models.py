from django.db import models
from django.template.defaultfilters import slugify
from django.conf import settings
from api.websites.services.get_php_versions import PhpVersionListService
from django.contrib.auth.models import AbstractUser, BaseUserManager
import os


class FastcpUserManager(BaseUserManager):
    """FastCP User Manager.
    
    This user manager allows us to make customizations to create_superuser method to make the auth flow
    compatible to FastCP's requirements.
    """
    
    def _create_user(self, **extra_fields):
        """
        Creates a a user for the provided username.
        """
        user = self.model(**extra_fields)
        user.save()
        return user
    
    def create_superuser(self, username, password=None, **kwargs):
        """Create superuser.
        
        We are overriding this method because the original method requires the email address. But we aren't
        going to have a field for user email.
        """
        user = self._create_user(
            username=username,
            is_staff=True,
            is_superuser=True,
            is_active=True
        )
        if password:
            user.set_password(password)
            user.save()
        return user

class User(AbstractUser):
    """User model.
    
    User model that supports both database and unix password authentication.
    """
    uid = models.IntegerField(null=True, blank=True)
    username = models.CharField(max_length=30, unique=True)
    
    # FastCP resource limits
    max_dbs = models.IntegerField(default=10)  # Max number of databases a user can create
    max_sites = models.IntegerField(default=10)  # Max number of websites a user can create
    storage_used = models.FloatField(default=0)  # Used storage in Bytes (1024 bytes == 1kb)
    max_storage = models.FloatField(default=1024)  # Max storage in Bytes a user can consume (1024 bytes == 1kb)
    max_ftp_users = models.IntegerField(default=5)  # Max number of FTP users
    
    # Security settings
    password_last_changed = models.DateTimeField(null=True, blank=True)
    password_expiry_days = models.IntegerField(default=90)  # Password expires after 90 days
    failed_login_attempts = models.IntegerField(default=0)
    is_locked = models.BooleanField(default=False)
    last_failed_login = models.DateTimeField(null=True, blank=True)
    
    # Audit fields
    last_activity = models.DateTimeField(null=True, blank=True)
    created_by = models.ForeignKey('self', null=True, blank=True, on_delete=models.SET_NULL, related_name='created_users')
    notes = models.TextField(null=True, blank=True)
    
    # More customizations
    REQUIRED_FIELDS = []
    objects = FastcpUserManager()
    
    @property
    def total_dbs(self):
        """Get the count of total databases owned by this user."""
        return self.databases.count()

    
    @property
    def total_sites(self):
        """Get the count of total sites owned by this user."""
        return self.websites.count()


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

# Define supported PHP versions
SUPPORTED_PHP_VERSIONS = ['8.3', '8.2', '8.1', '8.0', '7.4']

# Filter PHP versions to only include supported ones
php_versions = [v for v in php_versions if v in SUPPORTED_PHP_VERSIONS]

PHP_CHOICES = tuple((v, f'PHP {v}') for v in php_versions)
    
class FTPUser(models.Model):
    """FTP User model to store FTP user accounts."""
    user = models.ForeignKey(User, related_name='ftp_users', on_delete=models.CASCADE)
    username = models.CharField(max_length=32, unique=True)
    password = models.CharField(max_length=128)
    home_dir = models.CharField(max_length=255)
    website = models.ForeignKey('Website', related_name='website_ftp_users', on_delete=models.CASCADE)
    created_at = models.DateTimeField(auto_now_add=True)
    updated_at = models.DateTimeField(auto_now=True)
    
    # Access control
    is_active = models.BooleanField(default=True)
    permissions = models.CharField(max_length=3, default='rw-')  # r=read, w=write, d=delete
    upload_bandwidth_limit = models.IntegerField(default=0)  # 0 = unlimited, otherwise in KB/s
    download_bandwidth_limit = models.IntegerField(default=0)  # 0 = unlimited, otherwise in KB/s
    
    # Security
    last_login = models.DateTimeField(null=True, blank=True)
    last_ip = models.GenericIPAddressField(null=True, blank=True)
    failed_login_attempts = models.IntegerField(default=0)
    is_locked = models.BooleanField(default=False)
    
    # Quotas
    disk_quota = models.BigIntegerField(default=0)  # 0 = unlimited, otherwise in bytes
    disk_usage = models.BigIntegerField(default=0)  # In bytes

    def save(self, *args, **kwargs):
        """Create the FTP user in the Pure-FTPd password database."""
        # Ensure home directory exists
        if not os.path.exists(self.home_dir):
            os.makedirs(self.home_dir, mode=0o755, exist_ok=True)
        super().save(*args, **kwargs)

    def __str__(self):
        return self.username

class Website(models.Model):
    """Website model holds the websites owned by users."""
    user = models.ForeignKey(User, related_name='websites', on_delete=models.CASCADE)
    label = models.CharField(max_length=30, unique=True)
    has_ssl = models.BooleanField(default=False)
    slug = models.SlugField(max_length=50, unique=True, null=True, blank=True)
    php = models.CharField(choices=PHP_CHOICES, max_length=20)
    is_wp = models.BooleanField(default=False)
    
    # Timestamps
    created = models.DateTimeField(auto_now_add=True)
    updated = models.DateTimeField(auto_now=True)
    
    # Storage and monitoring
    disk_usage = models.BigIntegerField(default=0)  # In bytes
    is_suspended = models.BooleanField(default=False)
    suspension_reason = models.TextField(null=True, blank=True)
    
    # Backup settings
    backup_enabled = models.BooleanField(default=True)
    backup_retention_days = models.IntegerField(default=30)
    last_backup = models.DateTimeField(null=True, blank=True)
    
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
        base_path = os.path.join(settings.FILE_MANAGER_ROOT, self.user.username, 'apps', self.slug)
        return {
            'path': base_path,
            'pub_path': os.path.join(base_path, 'public'),
            'user': self.user.username,
            'ip_addr': settings.SERVER_IP_ADDR
        }
    
    def needs_ssl(self) -> bool:
        """Check either website needs SSL or not."""
        return self.domains.filter(ssl=False).count() > 0

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
    user = models.ForeignKey(User, related_name='databases', on_delete=models.CASCADE)
    name = models.SlugField(max_length=50, unique=True)
    username = models.SlugField(max_length=50, unique=True)
    created = models.DateTimeField(auto_now_add=True)
    
    def __str__(self):
        return self.name