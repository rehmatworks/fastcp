import shutil
from distutils.dir_util import copy_tree
from .base_service import BaseService
import os


class MoveDataService(BaseService):
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

        errors = False
        if dest_root and self.is_allowed(dest_root, user):
            paths = validated_data.get('paths').split(',')
            if len(paths):
                for p in paths:
                    try:
                        if validated_data.get('action') == 'move':
                            shutil.move(p, dest_root)
                        else:
                            if os.path.isdir(p):
                                dest_root = os.path.join(dest_root, os.path.basename(p))
                                copy_tree(p, dest_root)
                            else:
                                shutil.copy2(p, dest_root)
                    except Exception:
                        errors = True

            self.fix_ownership(dest_root)

        if errors:
            return False
        else:
            return True
