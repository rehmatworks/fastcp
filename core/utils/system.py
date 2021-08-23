import secrets, string, os, crypt, pwd
from django.template.loader import render_to_string
from core.utils import filesystem
from subprocess import (
    STDOUT, check_call, CalledProcessError, Popen, PIPE, DEVNULL
)

# Constants
FASTCP_SYS_GROUP = 'fcp-users'

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

def rand_passwd(length: int = 20) -> str:
    """Generate a random password.

    Generate a random and strong password using secrets module.
    """
    alphabet = string.ascii_letters + string.digits
    return ''.join(secrets.choice(alphabet) for _ in range(length))


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