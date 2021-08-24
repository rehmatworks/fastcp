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
        
        new_path = os.path.join(path, item_name)
        
        if self.is_allowed(new_path, user) and not os.path.exists(new_path):
            try:
                if item_type == 'file':
                    open(new_path, 'a').close()
                elif item_type == 'directory':
                    os.makedirs(new_path)
                
                self.fix_ownership(new_path)
                return True
            except:
                pass
    
        return False