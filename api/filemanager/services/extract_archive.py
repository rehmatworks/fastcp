from django.conf import settings
import os
from core.utils import filesystem as cpfs
from .base_service import BaseService


class ExtractArchiveService(BaseService):
    """Extract Archive
    
    Extracts an archive by taking the archive path and the destination path from a serializers.
    """
    def __init__(self, request):
        self.request = request
    
    def extract_archive(self, validated_data):
        """Extracts the archive.
        
        Args:
            validated_data (dict): The serializer's validated data.
        
        Returns:
            bool: True on success and False on failure.
        """
        try:
            path = validated_data.get('path')
            root_path = validated_data.get('root_path')
            user = self.request.user
            
            if not root_path and user.is_superuser:
                root_path = settings.FILE_MANAGER_ROOT
                
            if root_path and os.path.exists(root_path) and path and os.path.exists(path):
                cpfs.extract_zip(root_path, archive_path=path)
                self.fix_ownership(root_path)
                return True
                        
        except Exception as e:
            pass
        
        return False