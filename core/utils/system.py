import secrets, string, os, crypt, pwd
from datetime import datetime
from django.template.loader import render_to_string
from api.databases.services.mysql import FastcpSqlService
from core.utils import filesystem
from subprocess import (
    STDOUT, check_call, CalledProcessError, Popen, PIPE, DEVNULL
)
from cryptography import x509
from cryptography.hazmat.backends import default_backend


# Constants
FASTCP_SYS_GROUP = 'fcp-users'


def set_uid(uid=0) -> None:
    """Set UID.

    This function sets the system uid for the user. It is used by file manager and other components where
    permissions need to be persisted on created or updated items.

    Args:
        uid (int): UID of the user and group. Defaults to root.
    """
    os.setuid(uid)
    os.setgid(uid)


def run_cmd(cmd: str, shell=False) -> bool:
    """Runs a shell command.
    Runs a shell command using subprocess.

    Args:
        cmd (str): The shell command to run.
        shell (bool): Defines either shell should be set to True or False.

    Returns:
        bool: Returns True on success and False otherwise
    """
    try:
        if not shell:
            check_call(cmd.split(' '),
                       stdout=DEVNULL, stderr=STDOUT, timeout=300)
        else:
            Popen(cmd, stdin=PIPE, stdout=DEVNULL,
                  stderr=STDOUT).wait()
        return True
    except CalledProcessError:
        return False


def fix_ownership(website: object):
    """Fix ownership.

    Fixes the ownership of a website base directory and sub-directoris and files recursively.
    """
    # SSH user
    ssh_user = website.user.username

    # Website paths
    web_paths = filesystem.get_website_paths(website)
    base_path = web_paths.get('base_path')

    # Fix permissions
    run_cmd(f'/usr/bin/chown -R {ssh_user}:{ssh_user} {base_path}')


def setup_website(website: object):
    """Setup website.

    This function is responsible to setup the website when it's created and it
    restarts the services. Ideally, this function should be called soon after
    the website model is created.
    """

    # Create initial directories
    filesystem.create_website_dirs(website)

    # Fix permissions
    fix_ownership(website)

    # Create FPM pool conf
    filesystem.generate_fpm_conf(website)


def delete_website(website: object):
    """Delete website.

    This function cleans the website data and it should be called right before
    the website model is about to be deleted.
    """

    # Delete website directories
    filesystem.delete_website_dirs(website)

    # Delete PHP FPM pool conf
    filesystem.delete_fpm_conf(website)

    # Delete NGINX vhost files
    filesystem.delete_nginx_vhost(website)

    # Delete Apache vhost files
    filesystem.delete_apache_vhost(website)
    
    # Delete SSL certs
    filesystem.delete_ssl_certs(website)

def wpcli_cmd(website: object, cmd: str):
    """Run a WP-CLI command.

    Args:
        website (object): The website model.
        cmd (str): The WP-CLI command
    """
    # Website paths
    web_paths = filesystem.get_website_paths(website)
    web_root = web_paths.get('web_root')
    user = website.user.username
    
    cmd = f'/usr/local/bin/wp {cmd} --allow-root --path={web_root}'
    run_cmd(cmd)

    
def setup_wordpress(website: object, **kwargs) -> None:
    """Setup WordPress.
    
    By default, a blank PHP website is created, but if needed, this function
    installs WordPress in the root directory of the newly created wbsite.

    Args:
        website (object): Website model object.
    """
    # Download wp
    wpcli_cmd(website, f'core download')
    
    # Config wp
    dbname = kwargs.get('dbname')
    dbpassword = kwargs.get('dbpassword')
    dbuser = kwargs.get('dbuser')
    wpcli_cmd(website, f'core config --dbname="{dbname}" --dbuser="{dbuser}" --dbpass="{dbpassword}"')
    
    # Install WP
    domain = website.domains.first()
    siteurl = f'http://{domain.domain}'
    username = kwargs.get('username')
    email = kwargs.get('email')
    password = kwargs.get('password')
    wpcli_cmd(website, f'core install --url="{siteurl}" --title="My WordPress Blog" --admin_user="{username}" --admin_password="{password}" --admin_email="{email}"')
    fix_ownership(website)


def rand_passwd(length: int = 20) -> str:
    """Generate a random password.

    Generate a random and strong password using secrets module.
    """
    alphabet = string.ascii_letters + string.digits
    return ''.join(secrets.choice(alphabet) for _ in range(length))

def change_password(username: str) -> str:
    """Change a Unix user's password.
    
    This function generates a random password using rand_passwd function, sets the password for the user
    and returns the password as a string.
    
    Args:
        user (str): The Unix user's username.
    
    Returns:
        str: The new password.
    """
    passwd = rand_passwd()
    passwd_hash = crypt.crypt(passwd, '22')
    run_cmd(f'/usr/sbin/usermod --password {passwd_hash} {username}')
    
    return passwd

def change_db_password(username: str) -> str:
    """Change a MySQL user's password.
    
    This function generates a random password using rand_passwd function, sets the password for the user
    and returns the password as a string.
    
    Args:
        user (str): The MySQL username.
    
    Returns:
        str: The new password or None if update fails.
    """
    passwd = rand_passwd()
    result = FastcpSqlService().update_password(username, passwd)
    if result:
        return passwd
    return None

def setup_user(user: object, password: str = None) -> bool:
    """Setup the user.

    Setup the user data directories as well as create the unix user.

    Args:
        user (object): User model object.

    Returns:
        bool: True on success and False otherwise.
    """

    if password is None:
        password = rand_passwd(20)

    # Create SSH user
    user_paths = filesystem.get_user_paths(user)
    user_home = user_paths.get('base_path')
    run_path = user_paths.get('run_path')
    logs_path = user_paths.get('logs_path')
    user_pass = crypt.crypt(password, '22')

    # Create filesystem dirs
    filesystem.create_user_dirs(user)

    # Create unix user & group
    run_cmd(f'/usr/sbin/groupadd {user.username}')
    run_cmd(
        f'/usr/sbin/useradd -s /bin/bash -g {user.username} -p {user_pass} -d {user_home} {user.username}')
    run_cmd(f'/usr/sbin/usermod -G {FASTCP_SYS_GROUP} {user.username}')

    # Fix permissions
    run_cmd(f'/usr/bin/chown -R {user.username}:{user.username} {user_home}')
    run_cmd(f'/usr/bin/setfacl -m g:{FASTCP_SYS_GROUP}:--- {user_home}')
    run_cmd(f'/usr/bin/chown -R root:{user.username} {logs_path}')
    run_cmd(f'/usr/bin/setfacl -m u:{user.username}:r-x {logs_path}')
    run_cmd(f'/usr/bin/setfacl -m g::r-x {logs_path}')
    run_cmd(f'/usr/bin/chown root:www-data {run_path}')
    run_cmd(f'/usr/bin/setfacl -m o::x {run_path}')

    # Copy bash profile templates
    with open(os.path.join(user_home, '.profile'), 'w') as f:
        f.write(render_to_string('system/bash_profile.txt'))

    with open(os.path.join(user_home, '.bash_logout'), 'w') as f:
        f.write(render_to_string('system/bash_logout.txt'))

    with open(os.path.join(user_home, '.bashrc'), 'w') as f:
        f.write(render_to_string('system/bash_rc.txt'))

    # Get user uid
    uid = pwd.getpwnam(user.username).pw_uid
    user.uid = int(uid)
    user.save()


def create_database(database: object, password: str) -> bool:
    """Create database.

    Creates the MySQL database in the system.

    Args:
        database (object): The database model object.
        password (str): Database password.

    Returns:
        bool: True on success False otherwise.
    """

    return FastcpSqlService().setup_db(
        user=database.username,
        dbname=database.name,
        password=password
    )

def drop_db(database: object) -> None:
    """Deletes the database.
    
    Drops the database as well as the associated user.
    
    Args:
        database (object): Database model object.
    """
    try:
        FastcpSqlService().drop_db(database.name)
        FastcpSqlService().drop_user(database.username)
    except:
        pass

def delete_user_data(user: object) -> None:
    """Delete user data.

    Delete user directories, websites, and databases before a user model is deleted.

    Args:
        user (object): User model object.
    """

    # Delete webites
    for website in user.websites.all():
        website.delete()

    # Delete databases
    for db in user.databases.all():
        db.delete()

    # Delete user paths
    filesystem.delete_user_dirs(user)

    # Delete system user
    run_cmd(f'/usr/sbin/userdel {user.username}')


def ssl_expiring(website: object) -> bool:
    """Check if SSL is expiring.
    
    If an SSL has expired or if it is going to expire <= 30 days, this function will return True. FastCP uses this
    function to determine either an SSL certificate should be requested for a website or not.
    
    Args:
        website (object): Website model object.
        
    Returns:
        bool: Returns True if it's expiring, and returns False if expiry is not near or if SSL cert file was not found.
    """
    
    paths = filesystem.get_website_paths(website)
    
    if os.path.exists(paths.get('cert_chain_path')):
        with open(paths.get('cert_chain_path')) as f:
            certdata = f.read().encode()
        
        cert = x509.load_pem_x509_certificate(certdata, default_backend())
        expiry = cert.not_valid_after
        curr_time = datetime.now()
        
        # If expired
        if expiry <= curr_time:
            return True
        
        # Time delta
        delta = expiry - curr_time
        
        # If <= 7 days left
        if delta.days <= 30:
            return True
    
    return False