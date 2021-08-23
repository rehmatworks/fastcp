from core.utils import filesystem as cpfs
import os


class ReadFileService(object):
    """Read a file.
        
    This class attempts to read the contents of a file from the disk and returns the content.
    """
    
    def __init__(self, request):
        self.request = request
    
    def read_file(self, validated_data: dict) -> str:
        """Read file.
        
        Reads the file for the provided path and returns the content.
        
        Args:
            validated_data (dict): Validated data from serializer (api.filemanager.serializers.ReadFileSerializer)
        
        Returns:
            Content string on success and None on failure.
        """
        
        user = self.request.user
        path = validated_data.get('path')
        BASE_PATH = cpfs.get_user_path(user)
        content = None
    
        if path and path.startswith(BASE_PATH) and os.path.exists(path):
            PATH_INFO = cpfs.get_path_info(path)
            
            # Check for file existence, as well as discard
            # files larger than 10MB.
            
            if PATH_INFO.get('size') <= 10000000:
                try:
                    with open(path, 'rb') as f:
                        content = f.read()
                    content = content.decode('utf-8')
                except UnicodeDecodeError as e:
                    pass
                
        return content