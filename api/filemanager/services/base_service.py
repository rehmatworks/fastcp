from core.utils.system import (
    get_user_by_path, run_cmd
)

class BaseService(object):
    """Base service.
    
    All file manager services should extend this class. Here, we are going to write reusable code that can be
    used in multiple other places.
    """
    
    def fix_ownership(self, path: str) -> None:
        """Fix ownership.
        
        When an item is created by root or edited by root user, the permissions may get messed up. This function
        will ensure that permissions are correct on the path.
        
        Args:
            path (str): Path string.
        """
        
        user = get_user_by_path(path)
        if user:
            run_cmd(f'/usr/bin/chown -R {user}:{user} {path}')