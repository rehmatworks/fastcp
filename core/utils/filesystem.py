import os
import zipfile
from pathlib import Path
from datetime import datetime
from django.contrib.auth.models import User
from django.conf import settings
from django.template.loader import render_to_string


def extract_zip(root_path, archive_path):
    """Extract ZIP.

    This function attempts to extract the contents of a ZIP file to the specified
    path.

    Args:
        root_pat (str): The path where the extracted contents will be stored.
        archive_path (str): The path of the ZIP archive.
    """
    with zipfile.ZipFile(archive_path, 'r') as zip_ref:
        zip_ref.extractall(root_path)


def create_zip(root_path, file_name, selected=[], storage_path=None):
    """Create a ZIP

    This function creates a ZIP file of the provided root path.

    Args:
        root_path (str): Root path to start from when picking files and directories.
        file_name (str): File name to save the created ZIP file as.
        ignored (list): A list of files and/or directories that you want to ignore. This
                        selection is applied in root directory only.
        storage_path: If provided, ZIP file will be placed in this location. If None, the
                        ZIP will be created in root_path
    """

    # Ensure unique name for the ZIP file
    i = 1
    zip_root = None
    while True:
        if zip_root is None:
            if storage_path is not None:
                zip_root = os.path.join(storage_path, file_name)
            else:
                zip_root = os.path.join(root_path, file_name)

        if not os.path.exists(zip_root):
            break

        zip_root = zip_root.replace('.zip', f'-{i}.zip')
        i += 1

    zipf = zipfile.ZipFile(zip_root, 'w', zipfile.ZIP_DEFLATED)

    def iter_subtree(path, layer=0):
        # iter the directory
        path = Path(path)
        for p in path.iterdir():
            if layer == 0 and str(p) not in selected:
                continue

            zipf.write(p, str(p).replace(root_path, '').lstrip('/'))

            if p.is_dir():
                iter_subtree(p, layer=layer+1)

    iter_subtree(root_path)
    zipf.close()


def get_path_info(p):
    """Returns path info.

    This function tries to get details of a path including last modified time, creation time,
    permissions, size and so on.

    Args:
        [path] (str): The path of the file or the directory.

    Returns:
        dict: A dictionary containing the path details.
    """
    p = Path(p)
    try:
        username = str(p).split('fastcp/users')[1].split('/')[1]
        user = User.objects.filter(username=username).first()
    except IndexError as e:
        user = None
    return {
        'name': p.name,
        'user': user,
        'file_type': 'file' if p.is_file() else 'directory',
        'path': str(p).rstrip('/'),
        'size': os.path.getsize(p),
        'permissions': oct((os.stat(str(p)).st_mode))[-3:],
        'created': datetime.fromtimestamp(os.path.getctime(p)).strftime('%b %d, %Y %H:%M:%S'),
        'modified': datetime.fromtimestamp(os.path.getmtime(p)).strftime('%b %d, %Y %H:%M:%S')
    }


def get_user_path(user):
    """Get user path.

    This function returns the filesystem path for the provided user. Thie path is used by file manager.

    Args:
        user (object): User model object.
    """
    FM_ROOT = settings.FILE_MANAGER_ROOT
    if user.is_superuser:
        return FM_ROOT
    else:
        return os.path.join(FM_ROOT, user.username)


def get_fpm_path(website: object) -> str:
    """Get PHP-FPM conf path.

    Args:
        website (object): Website model object.
        
    Returns:
        str: Returns the FPM conf path as a string.
    """
    return os.path.join(settings.PHP_INSTALL_PATH, website.php, 'fpm', 'pool.d', f'{website.slug}.conf')


def generate_fpm_conf(website: object) -> bool:
    """Generate FPM pool conf.

    This function generates the PHP-FPM pool configuration file for the provided website.

    Args:
        website (object): Website model object.
        
    Returns:
        bool: True on success False otherwise.
    """
    context = {
        'ssh_user': website.user.username,
        'ssh_group': website.user.username,
        'app_name': website.slug
    }

    # Render template data
    data = render_to_string('system/php-fpm-pool.txt', context)

    # Write conf file
    fpm_path = get_fpm_path(website)
    try:
        with open(fpm_path, 'w') as f:
            f.write(data)
        return True
    except:
        return False

def delete_fpm_conf(website: object) -> bool:
    """Delete FPM pool conf.
    
    This function deletes the FPM pool conf for a website.
    
    Args:
        website (object): Website model object.
        
    Returns:
        bool: True on success Falase otherwise.
    """
    fpm_path = get_fpm_path(website)
    if os.path.exists(fpm_path):
        try:
            os.remove(fpm_path)
            return True
        except:
            pass
    
    return False