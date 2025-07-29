from pathlib import Path
from django.conf import settings


class PhpVersionListService(object):
    """List PHP versions.
    
    This class scans the PHP installation directory and gets the list of supported PHP versions on the system.
    """
    
    def get_php_versions(self) -> list:
        try:
            path = Path(settings.PHP_INSTALL_PATH)
            versions = []
            if path.exists():
                for version in path.iterdir():
                    if version.is_dir():
                        versions.append(version.name)
                versions.sort(reverse=True)
            else:
                # Return default supported versions if PHP directory doesn't exist
                versions = ['8.2', '8.1', '8.0', '7.4']
            return versions
        except Exception:
            # Fallback to default supported versions
            return ['8.2', '8.1', '8.0', '7.4']