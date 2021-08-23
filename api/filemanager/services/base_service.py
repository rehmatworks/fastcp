from core.utils.system import (
    get_uid_by_path, set_uid
)

class BaseService(object):
    """Base service.
    
    All file manager services should extend this class. Here, we are going to write reusable code that can be
    used in multiple other places.
    """
    
    def ensure_owner(self) -> None:
        """Sets the Unix user id.
        
        Sets the Unix user ID of the intended user to ensure that permissions don't get messed up when edited
        by root.
        """
        
        uid = get_uid_by_path(self.path)
        if uid:
            set_uid(uid)