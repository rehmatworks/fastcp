from core.utils import filesystem as cpfs
import os
from .base_service import BaseService


class FileUploadService(BaseService):
    """Upload a file.
    
    Processes an uploaded file and stores the content on the disk.
    """
    def __init__(self, request):
        self.request = request
    
    def upload_file(self, validated_data):
        """Process upload.
        
        Args:
            validated_data (dict): Validated data dict from serializer (api.serializers.FileUploadSerializer)
        
        Returns:
            bool: True on success and False otherwise.
        """
        path = validated_data.get('path')
        user = self.request.user
            
        f = validated_data.get('file')
        dest_path = os.path.join(path, f.name)
        if self.is_allowed(path, user) and not os.path.exists(dest_path):
            with open(dest_path, 'wb+') as destination:
                for chunk in f.chunks():
                    destination.write(chunk)    
            
            self.fix_ownership(dest_path)
            return True
        return False