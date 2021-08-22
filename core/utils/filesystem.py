import os, shutil, zipfile
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


def get_fpm_path(website: object) -> str:
    """Get PHP-FPM conf path.

    Args:
        website (object): Website model object.
        
    Returns:
        str: Returns the FPM conf path as a string.
    """
    return os.path.join(settings.PHP_INSTALL_PATH, website.php, 'fpm', 'pool.d', f'{website.slug}.conf')

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
    website_conf_dir = os.path.join(settings.NGINX_VHOSTS_ROOT, f'{website.slug}.d')
    try:
        shutil.rmtree(website_conf_dir)
        ssl_vhost = os.path.join(settings.NGINX_VHOSTS_ROOT, f'{website.slug}-ssl.conf')
        non_ssl_vhost = os.path.join(settings.NGINX_VHOSTS_ROOT, f'{website.slug}.conf')
        
        if os.path.exists(ssl_vhost):
            os.remove(ssl_vhost)
        
        if os.path.exists(non_ssl_vhost):
            os.remove(non_ssl_vhost)
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
    website_conf_dir = os.path.join(settings.NGINX_VHOSTS_ROOT, f'{website.slug}.d')
    create_if_missing(website_conf_dir)
    
    # Vhost conf path
    is_ssl = protocol == 'https' and kwargs.get('ssl_cert') and kwargs.get('ssl_key')
    if is_ssl:
        website_vhost_path = os.path.join(settings.NGINX_VHOSTS_ROOT, f'{website.slug}-ssl.conf')
    else:
        website_vhost_path = os.path.join(settings.NGINX_VHOSTS_ROOT, f'{website.slug}.conf')
    
    domains = ''
    i = 0
    for domain in website.domains.all():
        if i > 0:
            domains += ' '
        domains += domain.domain
        i += 1
    
    user_path = get_user_path(website.user, exact=True)
    web_root = os.path.join(user_path, 'apps', website.slug, 'public')
    socket_path = os.path.join(user_path, 'run', f'{website.slug}.sock')
    
    context = {
        'domains': domains,
        'webroot': web_root,
        'socket_path': socket_path
    }
    
    if is_ssl:
        context['ssl_cert'] = kwargs.get('ssl_cert')
        context['ssl_key'] = kwargs.get('ssl_key')
        tpl_data = render_to_string('system/nginx-vhost-https.txt', context=context)
    else:
        tpl_data = render_to_string('system/nginx-vhost-http.txt', context=context)
    
    with open(website_vhost_path, 'w') as f:
        f.write(tpl_data)
        

def create_website_dirs(website: object) -> bool:
    """Create website directories.
    
    This function creates the website directories. If the SSH user doesn't have the directories yet,
    they are created as well.
    
    Args:
        website (object): The website model object.
        
    Returns:
        bool: True on success False otherwise.
    """
    user_path = get_user_path(website.user, exact=True)
    try:
        # Create user dir if not exists
        create_if_missing(user_path)
            
        # Sockets path
        run_path = os.path.join(user_path, 'run')
        create_if_missing(run_path)
        
        # Logs path
        logs_path = os.path.join(user_path, 'logs')
        create_if_missing(logs_path)
        
        # Apps root path
        apps_path = os.path.join(user_path, 'apps')
        create_if_missing(apps_path)
        
        # Website path
        website_path = os.path.join(apps_path, website.slug)
        create_if_missing(website_path)
        
        # Website public path
        public_path = os.path.join(website_path, 'public')
        create_if_missing(public_path)
        
        return True
    except:
        return False

def delete_website_dirs(website: object) -> bool:
    """Deletes website directories."""
    user_path = get_user_path(website.user, exact=True)
    website_path = os.path.join(user_path, 'apps', website.slug)
    try:
        shutil.rmtree(website_path)
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
    user_path = get_user_path(website.user, exact=True)
    
    context = {
        'ssh_user': website.user.username,
        'ssh_group': website.user.username,
        'app_name': website.slug,
        'run_path': os.path.join(user_path, 'run')
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