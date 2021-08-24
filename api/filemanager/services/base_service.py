from core.utils.system import (
    get_user_by_path, run_cmd
)

class BaseService(object):
    """Base service.
    
    All file manager services should extend this class. Here, we are going to write reusable code that can be
    used in multiple other places.
    
    Attributes:
        PROTECTED_PATHS (list): List of protected paths to ignore during listing and other operations. The are relative
                                to user's home directory.
    """
    PROTECTED_PATHS = ['run', '.profile', '.bashrc', '.bash_logout', '.bash_history', '.local']
    
    def is_protected(self, path: str) -> bool:
        """Checks either path is protected or not.
        
        This method checks and ensures that the provided path is not included in protected paths list.
        
        Args:
            path (str): The path string.
        
        Returns:
            bool: True on success False otherwise.
        """
        try:
            path = str(path)
            dir_name = path.split('/')[4]
            return str(dir_name).lower() in self.PROTECTED_PATHS
        except IndexError:
            return False
    
    def fix_ownership(self, path: str) -> None:
        """Fix ownership.
        
        When an item is created by root or edited by root user, the permissions may get messed up. This function
        will ensure that permissions are correct on the path.
        
        Args:
            path (str): Path string.
        """
        path = str(path)
        user = get_user_by_path(path)
        if user and not self.is_protected(path) and len(path.split('/')) >= 4:
            run_cmd(f'/usr/bin/chown -R {user}:{user} {path}')