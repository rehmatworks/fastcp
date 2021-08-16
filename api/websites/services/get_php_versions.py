from pathlib import Path
from django.conf import settings


class PhpVersionListService(object):
    """List PHP versions.
    
    This class scans the PHP installation directory and gets the list of supported PHP versions on the system.
    """
    
    def get_php_versions(self) -> list:
        path = Path(settings.PHP_INSTALL_PATH)
        versions = []
        for version in path.iterdir():
            if version.is_dir():
                versions.append(version.name)
        versions.sort(reverse=True)
        return versions