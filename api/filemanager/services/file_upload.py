import os
from .base_service import BaseService
import requests


import logging

class FileUploadService(BaseService):
    """Service for handling file uploads, both local and remote."""

    def __init__(self, request) -> None:
        """
        Initialize the FileUploadService.
        Args:
            request: Django request object containing user info.
        """
        self.request = request

    def upload_file(self, validated_data: dict) -> bool:
        """
        Process a local file upload and store it on disk.
        Args:
            validated_data (dict): Validated data dict from serializer (api.filemanager.serializers.FileUploadSerializer)
        Returns:
            bool: True on success, False otherwise.
        """
        path = validated_data.get('path')
        user = self.request.user
        f = validated_data.get('file')
        dest_path = os.path.join(path, f.name)
        try:
            if self.is_allowed(path, user) and not os.path.exists(dest_path):
                with open(dest_path, 'wb+') as destination:
                    for chunk in f.chunks():
                        destination.write(chunk)
                self.fix_ownership(dest_path)
                return True
            else:
                logging.warning(f"Upload not allowed or file exists: {dest_path}")
        except Exception as e:
            logging.error(f"Error uploading file to {dest_path}: {e}")
        return False

    def remote_upload(self, validated_data: dict) -> bool:
        """
        Download a file from a remote URL and store it on disk.
        Args:
            validated_data (dict): Validated data dict from serializer (api.filemanager.serializers.RemoteUploadSerializer)
        Returns:
            bool: True on success, False otherwise.
        """
        path = validated_data.get('path')
        user = self.request.user
        remote_url = validated_data.get('remote_url')
        dest_path = os.path.join(path, os.path.basename(remote_url))
        try:
            if self.is_allowed(path, user):
                with requests.get(remote_url, stream=True) as res:
                    if res.status_code == 200:
                        with open(dest_path, 'wb') as f:
                            for chunk in res.iter_content(chunk_size=(1024*1024)):
                                f.write(chunk)
                        self.fix_ownership(dest_path)
                        return True
                    else:
                        logging.warning(f"Remote upload failed, status {res.status_code}: {remote_url}")
            else:
                logging.warning(f"Remote upload not allowed: {dest_path}")
        except Exception as e:
            logging.error(f"Error in remote upload to {dest_path}: {e}")
        return False