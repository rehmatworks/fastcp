from core.utils import filesystem
import secrets
import string
import os, crypt
from subprocess import (
    STDOUT, check_call, CalledProcessError
)


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
                           stdout=open(os.devnull, 'wb'), stderr=STDOUT, timeout=300)
            else:
                check_call(cmd, stdout=open(os.devnull, 'wb'),
                           stderr=STDOUT, shell=True, executable='bash', timeout=300)
            return True
        except CalledProcessError:
            return False

def setup_website(website: object):
    """Setup website.
    
    This function is responsible to setup the website when it's created and it
    restarts the services. Ideally, this function should be called soon after
    the website model is created.
    """
    
    # Create initial directories
    filesystem.create_website_dirs(website)
    
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
   
def rand_passwd(length: int = 20) -> str:
    """Generate a random password.

    Generate a random and strong password using secrets module.
    """
    alphabet = string.ascii_letters + string.digits
    return ''.join(secrets.choice(alphabet) for _ in range(length)) 

def setup_user(user: object, password: str=None) -> bool:
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
    user_home = filesystem.get_user_path(user, exact=True)
    user_pass = crypt.crypt(password, 22)
    
    # Create filesystem dirs
    filesystem.create_user_dirs(user)
    
    # Create unix user
    run_cmd(f'useradd -s /bin/bash -g {user.username} -p {user_pass} -d {user_home} {user.username}')
    
    # Fix permissions
    run_cmd(f'chown -R {user.username}:{user.username} {user_home}')