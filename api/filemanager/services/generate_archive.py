from core.utils import filesystem as cpfs
import os
from django.template.defaultfilters import slugify
from core.utils.system import (
    get_uid_by_path, set_uid
)


class GenerateArchiveService(object):
    """Generates an archive.
    
    Generates an archive from the supplied paths in the provided root directory. The paths and root directory
    is retrieved from the serializer validated data.
    """
    
    def __init__(self, request):
        self.request = request
    
    def generate_archive(self, validated_data: dict) -> bool:
        """Generate archive
        
        Args:
            validated_data (dict): Serializer's validated data that contains paths and the optional root path.
        
        Returns:
            bool: Returns True on success and False if an error is occured.
        """
        try:
            paths = validated_data.get('paths').split(',')
            root_path = validated_data.get('path')
            user = self.request.user
            BASE_PATH = cpfs.get_user_path(user)
            
            if not root_path or not root_path.startswith(BASE_PATH):
                root_path = BASE_PATH
                
            if len(paths) and root_path and root_path.startswith(BASE_PATH):
                # Become user    
                uid = get_uid_by_path(root_path)
                if uid:
                    set_uid(uid)
                filename = os.path.basename(paths[0])
                archive_name = f'{slugify(filename)}.zip'
                cpfs.create_zip(root_path, archive_name, selected=paths)
                
                # Revert to root
                set_uid(0)
                return True
        except Exception as e:
            pass
        
        return False