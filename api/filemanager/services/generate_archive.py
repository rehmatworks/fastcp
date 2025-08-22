from core.utils import filesystem as cpfs
import os
from django.template.defaultfilters import slugify
from .base_service import BaseService


class GenerateArchiveService(BaseService):
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

            if len(paths) and root_path and self.is_allowed(root_path, user):
                filename = os.path.basename(paths[0])
                archive_name = f'{slugify(filename)}.zip'
                cpfs.create_zip(root_path, archive_name, selected=paths)
                self.fix_ownership(root_path)
                return True
        except Exception:
            pass

        return False
