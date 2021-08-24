from core.utils import filesystem as cpfs
import os
from .base_service import BaseService


class UpdateFileService(BaseService):
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
        
        if path and os.path.exists(path) and self.is_allowed(path, user):
            try: 
                data = validated_data.get('content')
                with open(path, 'wb') as f:
                    f.write(data.encode())
                
                self.fix_ownership(path)
                return True
            except UnicodeDecodeError as e:
                pass
        
        return False