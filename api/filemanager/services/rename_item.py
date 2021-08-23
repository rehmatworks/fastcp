from core.utils import filesystem as cpfs
import os
from core.utils.system import (
    get_uid_by_path, set_uid
)


class RenameItemService(object):
    """Rename item.
    
    This class is responsible to rename a file or a directory.
    """
    
    def __init__(self, request):
        self.request = request
    
    def rename_item(self, validated_data: dict) -> bool:
        """Rename item.
        
        Args:
            validated_data (dict): Validated data from serializer (api.filemanager.serializers.RenameFileSerializer)
        
        Returns:
            bool: True on success and False on failure.
        """
        root_path = validated_data.get('path')
        new_name = validated_data.get('new_name')
        old_name = validated_data.get('old_name')
        user = self.request.user
        
        BASE_PATH = cpfs.get_user_path(user)
            
        if not root_path or not root_path.startswith(BASE_PATH):
            root_path = BASE_PATH
        
        # Become user
        uid = get_uid_by_path(root_path)
        if uid:
            set_uid(uid)
            
        old_path = os.path.join(root_path, old_name)
        new_path = os.path.join(root_path, new_name)
        if os.path.exists(old_path) and not os.path.exists(new_path):
            try:
                os.rename(old_path, new_path)
                
                # Revert to root
                set_uid(0)
                return True
            except:
                pass    
        
        # Revert to root
        set_uid(0)
        return False