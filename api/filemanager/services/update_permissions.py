from core.utils.system import run_cmd
from .base_service import BaseService


class UpdatePermissionService(BaseService):
    """Update permission.

    This class is responsible to update permissions on a file or a directory.
    """

    def __init__(self, request):
        self.request = request

    def update_permissions(self, validated_data: dict) -> bool:
        """Update permissions.

        Args:
            validated_data (dict): Validated data from serializer
                (api.filemanager.serializers.PermissionUpdateSerializer)

        Returns:
            bool: True on success and False on failure.
        """
        path = validated_data.get('path')
        permissions = validated_data.get('permissions')
        user = self.request.user

        if path and self.is_allowed(path, user):
            try:
                run_cmd(f'/usr/bin/chmod {permissions} {path}')
                self.fix_ownership(path)
                return True
            except Exception:
                pass
        return False
