"""
Test settings for running Django tests locally without external DB.

This imports the normal settings and overrides DATABASES to use sqlite3
so tests can run inside the developer environment where the `mariadb`
service may not be available.
"""
from .settings import *  # noqa: F401,F403
from .settings import BASE_DIR

# Use a lightweight sqlite database for tests to avoid requiring the
# docker-compose mariadb host when running `manage.py test` locally.
DATABASES = {
    'default': {
        'ENGINE': 'django.db.backends.sqlite3',
        'NAME': BASE_DIR / 'test_db.sqlite3',
    }
}

# Make tests deterministic for timezone-sensitive code
USE_TZ = False

# Use a local, writable file manager root for tests to avoid permission issues
FILE_MANAGER_ROOT = BASE_DIR / 'test_data'
