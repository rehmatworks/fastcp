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
                
            if self.is_allowed(path, user) and self.is_allowed(root_path, user):
                cpfs.extract_zip(root_path, archive_path=path)
                self.fix_ownership(root_path)
                return True
                        
        except Exception as e:
            pass
        
        return False