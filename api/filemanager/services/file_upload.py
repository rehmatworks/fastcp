import os
from .base_service import BaseService
import requests


class FileUploadService(BaseService):
    """Upload a file.
    
    Processes an uploaded file and stores the content on the disk.
    """
    def __init__(self, request):
        self.request = request
    
    def upload_file(self, validated_data) -> bool:
        """Process upload.
        
        Args:
            validated_data (dict): Validated data dict from serializer (api.filemanager.serializers.FileUploadSerializer)
        
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

    
    def remote_upload(self, validated_data) -> bool:
        """Process remote upload.
        
        Args:
            validated_data (dict): Validated data dict from serializer (api.filemanager.serializers.RemoteUploadSerializer)
        
        Returns:
            bool: True on success and False otherwise.
        """
        path = validated_data.get('path')
        user = self.request.user
        
        remote_url = validated_data.get('remote_url')
        dest_path = os.path.join(path, os.path.basename(remote_url))
        
        # Check if allowed
        if self.is_allowed(path, user):
            with requests.get(remote_url, stream=True) as res:
                # Check status code
                if res.status_code == 200:
                    with open(dest_path, 'wb') as f:
                        for chunk in res.iter_content(chunk_size=1024):
                            f.write(chunk)
                
                    return True
        return False