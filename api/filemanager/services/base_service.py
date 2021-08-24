from core.utils.system import run_cmd
from core.models import User


class BaseService(object):
    """Base service.
    
    All file manager services should extend this class. Here, we are going to write reusable code that can be
    used in multiple other places.
    
    Attributes:
        PROTECTED_PATHS (list): List of protected paths to ignore during listing and other operations. The are relative
                                to user's home directory.
    """
    
    def get_owner_by_path(self, path: str) -> str:
        """Get user by path.
        
        Parses the path and tries to get the user.
        
        Args:
            path (str): Path of the file or a folder.
            
        Returns:
            str: Username if found or null.
        """
        try:
            username = path.split('/')[3]
            return User.objects.filter(username=username).first()
        except IndexError:
            return None
    
    def is_owner(self, path: str, user: object) -> bool:
        """Checks either path is protected or not.
        
        This method checks and ensures that the provided path is not included in protected paths list.
        
        Args:
            path (str): The path string.
            owner (object): The user model.
        
        Returns:
            bool: True on success False otherwise.
        """
        owner = self.get_owner_by_path(path)
        return owner and (user.id == owner.id or user.is_superuser)
        
    def is_allowed(self, path: str, user: object) -> bool:
        """Ensure path is allowed.
        
        For security reasons, only certain paths are allowed. This method checks and ensures that the path
        is allowed.
        
        Args:
            path (str): Path string.
            user (object): Userm odel object.
            
        Returns:
            bool: True if allowed False otherwise.
        """
        path = str(path)
        if self.is_owner(path, user):
            return len(path.split('/')) >= 6
        return False
        
    
    def fix_ownership(self, path: str) -> None:
        """Fix ownership.
        
        When an item is created by root or edited by root user, the permissions may get messed up. This function
        will ensure that permissions are correct on the path.
        
        Args:
            path (str): Path string.
        """
        path = str(path)
        user = self.get_owner_by_path(path)
        if user:
            run_cmd(f'/usr/bin/chown -R {user}:{user} {path}')