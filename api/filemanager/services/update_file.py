from core.utils import filesystem as cpfs
import os
from core.utils.system import (
    get_uid_by_path, set_uid
)


class UpdateFileService(object):
    """Update file.
    
    This class updates a file on the disk using the provided content.
    """
    
    def __init__(self, request):
        self.request = request
    
    def update_file(self, validated_data: dict) -> bool:
        """Update file.
        
        Args:
            validated_data (dict): Serializer's validated data that contains the updated content as well as file path.
        
        Returns:
            bool: True if file update succeeds and False on failure.
        """
        user = self.request.user
        path = validated_data.get('path')
        BASE_PATH = cpfs.get_user_path(user)
        
        if path and os.path.exists(path) and path.startswith(BASE_PATH):
            try:
                # Become user
                uid = get_uid_by_path(path)
                if uid:
                    set_uid(uid)
                    
                data = validated_data.get('content')
                with open(path, 'wb') as f:
                    f.write(data.encode())
                
                # Revert to root
                set_uid(0)
                return True
            except UnicodeDecodeError as e:
                pass
        
        return False