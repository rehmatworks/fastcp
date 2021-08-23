from core.utils import filesystem as cpfs
import os
from .base_service import BaseService


class CreateItemService(BaseService):
    """Create an item.
    
    Creates an item (either a file or a directory) based on the information obtained from a serializers.
    """
    
    def __init__(self, request):
        self.request = request
    
    def create_item(self, validated_data):
        """Create item.
        
        Args:
            validated_data (dict): The serializer validated data.
        
        Returns:
            bool: True on success and False on failure.
        """
        path = validated_data.get('path')
        user = self.request.user
        item_type = validated_data.get('item_type')
        item_name = validated_data.get('item_name')
        
        BASE_PATH = cpfs.get_user_path(user)
        
        if path and path.startswith(BASE_PATH):
            root_path = path
        else:
            root_path = BASE_PATH
        
        new_path = os.path.join(root_path, item_name)
        if not os.path.exists(new_path):
            try:
                if item_type == 'file':
                    open(new_path, 'a').close()
                    return True
                elif item_type == 'directory':
                    os.makedirs(new_path)
                    return True
                return True
            except:
                pass
    
        return False