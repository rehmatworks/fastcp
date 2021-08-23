import os, shutil, zipfile
from pathlib import Path
from datetime import datetime
from core.models import User
from django.conf import settings
from django.template.loader import render_to_string
from core import signals


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
    return {
        'name': p.name,
        'file_type': 'file' if p.is_file() else 'directory',
        'path': str(p).rstrip('/'),
        'size': os.path.getsize(p),
        'permissions': oct((os.stat(str(p)).st_mode))[-3:],
        'created': datetime.fromtimestamp(os.path.getctime(p)).strftime('%b %d, %Y %H:%M:%S'),
        'modified': datetime.fromtimestamp(os.path.getmtime(p)).strftime('%b %d, %Y %H:%M:%S')
    }


def get_user_path(user, exact=False):
    """Get user path.

    This function returns the filesystem path for the provided user. Thie path is used by file manager.

    Args:
        user (object): User model object.
    """
    FM_ROOT = settings.FILE_MANAGER_ROOT
    if user.is_superuser and not exact:
        return FM_ROOT
    else:
        return os.path.join(FM_ROOT, user.username)


def get_user_paths(user: object) -> dict:
    """Get user paths.
    
    This should not be confused with get_user_path (singular) and this function returns a dict containing
    all paths associated to a user that FastCP needs.
    
    Args:
        user (object): User model object.
        
    Returns:
        dict: A dictionary containing user paths.
    """
    user_path = get_user_path(user, exact=True)
    return {
        'base_path': user_path,
        'apps_path': os.path.join(user_path, 'apps'),
        'run_path': os.path.join(user_path, 'run'),
        'logs_path': os.path.join(user_path, 'logs')
    }


def get_website_paths(website: object) -> dict:
    """Get website paths.
    
    This function returns the common paths for a website. Generating the paths in a single
    place makes things easier when it comes to modify a location of any installation.
    
    Args:
        website (object): Website model object.
    
    Returns:
        dict: Returns a dictionary that contains the path strings.
    """
    user_paths = get_user_paths(website.user)
    web_base = os.path.join(user_paths.get('apps_path'), website.slug)
    fpm_root = os.path.join(settings.PHP_INSTALL_PATH, website.php, 'fpm', 'pool.d')
    return {
        'fpm_root': fpm_root,
        'fpm_path': os.path.join(fpm_root, f'{website.slug}.conf'),
        'base_path': web_base,
        'web_root': os.path.join(web_base, 'public'),
        'socket_path': os.path.join(user_paths.get('run_path'), f'{website.slug}.sock'),
        'ngix_vhost_dir': os.path.join(settings.NGINX_VHOSTS_ROOT, f'{website.slug}.d'),
        'ngix_vhost_conf': os.path.join(settings.NGINX_VHOSTS_ROOT, f'{website.slug}.conf'),
        'ngix_vhost_ssl_conf': os.path.join(settings.NGINX_VHOSTS_ROOT, f'{website.slug}-ssl.conf'),
    }

def create_if_missing(path: str) -> bool:
    """Create a path if missing.
    
    This function crates a path if it's missing.
    
    Args:
        path (str): The path string.
        
    Returns:
        bool: True if created, False if not.
    """
    if not os.path.exists(path):
        try:
            os.makedirs(path)
            return True
        except:
            pass
    return False

def delete_nginx_vhost(website: object) -> bool:
    """Delete NGINX vhosts file.
    
    This function deletes the NGINX vhost files for the provided website.
    
    Args:
        website (object): Website model object.
    
    Returns:
        bool: True on success False otherwise.
    """
    website_paths = get_website_paths(website)
    website_conf_dir = website_paths.get('ngix_vhost_dir')
    try:
        shutil.rmtree(website_conf_dir)
        ssl_vhost = website_paths.get('ngix_vhost_ssl_conf')
        non_ssl_vhost = website_paths.get('ngix_vhost_conf')
        
        if os.path.exists(ssl_vhost):
            os.remove(ssl_vhost)
        
        if os.path.exists(non_ssl_vhost):
            os.remove(non_ssl_vhost)
        signals.restart_services.send(sender=None, services='nginx')
        return True
    except:
        return False

def create_nginx_vhost(website: object, protocol: str='http', **kwargs) -> bool:
    """Create NGINX vhost file.
    
    This function generates NGINX vhost file. The default protocol is HTTP. If the vhost needs to be created for
    the HTTPs protocol, ssl_cert and ssl_key should be passed in args.
    
    Args:
        website (object): Website model object.
        protocol (str): It should be either http or https.
    
    Returns:
        bool: True on success and False otherwise.
    """
    website_paths = get_website_paths(website)
    create_if_missing(website_paths.get('ngix_vhost_dir'))
    
    # Vhost conf path
    is_ssl = protocol == 'https' and kwargs.get('ssl_cert') and kwargs.get('ssl_key')
    if is_ssl:
        website_vhost_path = website_paths.get('ngix_vhost_ssl_conf')
    else:
        website_vhost_path = website_paths.get('ngix_vhost_conf')
    
    domains = ''
    i = 0
    for domain in website.domains.all():
        if i > 0:
            domains += ' '
        domains += domain.domain
        i += 1
    
    context = {
        'domains': domains,
        'webroot': website_paths.get('web_root'),
        'socket_path': website_paths.get('socket_path')
    }
    
    if is_ssl:
        context['ssl_cert'] = kwargs.get('ssl_cert')
        context['ssl_key'] = kwargs.get('ssl_key')
        tpl_data = render_to_string('system/nginx-vhost-https.txt', context=context)
    else:
        tpl_data = render_to_string('system/nginx-vhost-http.txt', context=context)
    
    try:
        with open(website_vhost_path, 'w') as f:
            f.write(tpl_data)
        signals.restart_services.send(sender=None, services='nginx')
        return True
    except:
        return False
 
 
def create_user_dirs(user: object) -> bool:
    """Create user directories.
    
    Creates the user data directories.
    
    Args:
        user (object): User model object.
        
    Returns:
        bool: Returns True on success and Falase otherwise
    """     
    user_paths = get_user_paths(user)  
    
    try:
        # Create user dir if not exists
        create_if_missing(user_paths.get('base_path'))
        
        # Sockets path
        create_if_missing(user_paths.get('run_path'))
        
        # Logs path
        create_if_missing(user_paths.get('logs_path'))
        
        # Apps root path
        create_if_missing(user_paths.get('apps_path'))
        return True
    except:
        return False

def create_website_dirs(website: object):
    """Create website directories.
    
    This function creates the website directories. If the SSH user doesn't have the directories yet,
    they are created as well.
    
    Args:
        website (object): The website model object.
        
    Returns:
        Website path string on success False otherwise.
    """
    try:
       
        # Create user dirs if missing
        create_user_dirs(website.user) 
        
        # Website path
        website_paths = get_website_paths(website)
        create_if_missing(website_paths.get('base_path'))
        
        # Website public path
        create_if_missing(website_paths.get('web_root'))
        
        return website_paths.get('base_path')
    except:
        return False

def delete_website_dirs(website: object) -> bool:
    """Deletes website directories."""
    website_paths = get_website_paths(website)
    try:
        shutil.rmtree(website_paths.get('base_path'))
        return True
    except:
        return False
    

def generate_fpm_conf(website: object) -> bool:
    """Generate FPM pool conf.

    This function generates the PHP-FPM pool configuration file for the provided website.

    Args:
        website (object): Website model object.
        
    Returns:
        bool: True on success False otherwise.
    """
    paths = get_website_paths(website)
    
    # Delete if default fpm pool exists
    default_conf = os.path.join(paths.get('fpm_root'), 'www.conf')
    if os.path.exists(default_conf):
        os.remove(default_conf)
    
    context = {
        'app_name': website.slug,
        'ssh_user': website.user.username,
        'ssh_group': website.user.username,
        'socket_path': paths.get('socket_path')
    }

    # Render template data
    data = render_to_string('system/php-fpm-pool.txt', context)

    # Write conf file
    try:
        with open(paths.get('fpm_path'), 'w') as f:
            f.write(data)
        signals.restart_services.send(sender=None, services=f'php{website.php}-fpm')
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
    fpm_path = get_website_paths(website).get('fpm_path')
    if os.path.exists(fpm_path):
        try:
            os.remove(fpm_path)
            signals.restart_services.send(sender=None, services=f'php{website.php}-fpm')
            return True
        except:
            pass
    
    return False