from core.utils import filesystem as cpfs
import shutil


class MoveDataService(object):
    """Move data.
    
    This class is responsible to move or copy items from one location to another.
    """
    
    def __init__(self, request):
        self.request = request
    
    def move_data(self, validated_data: dict) -> bool:
        """Move data.
        
        Args:
            validated_data (dict): Validated data from serializer (api.filemanager.serializers.MoveItemsSerializer)
        
        Returns:
            bool: True on success and False on failure.
        """
        dest_root = validated_data.get('path')
        user = self.request.user
        
        BASE_PATH = cpfs.get_user_path(user)
            
        if not dest_root or not dest_root.startswith(BASE_PATH):
            dest_root = BASE_PATH
            
        
        errors = False
        if dest_root:
            paths = validated_data.get('paths').split(',')
            if len(paths):
                for p in paths:
                    try:
                        if validated_data.get('action') == 'move':
                            shutil.move(p, dest_root)
                        else:
                            shutil.copy2(p, dest_root)
                    except:
                        errors = True
        if errors:
            return False
        else:
            return True