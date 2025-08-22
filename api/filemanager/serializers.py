from rest_framework import serializers
import os
from django.conf import settings
from urllib.parse import unquote


class ValidPathSerializer(serializers.Serializer):
    # For security reasons, let's validate the
    # path and ensure that it is allowed. Further
    # more granular validation is done in base_service
    # module
    def validate_path(self, value):
        # URL decode incoming path values so clients that send encoded
        # or double-encoded paths won't fail validation. Decode up to
        # a few times to be defensive.
        if value:
            # Repeatedly unquote up to 3 times to handle double-encoding.
            try:
                for _ in range(3):
                    if (
                        '%25' in value
                        or '%20' in value
                        or '%2F' in value
                        or '%2f' in value
                    ):
                        new = unquote(value)
                        if new == value:
                            break
                        value = new
                    else:
                        break
            except Exception:
                # Fall back to single decode if something goes wrong
                value = unquote(value)

            # normalize any accidental whitespace and path separators
            value = value.strip()
            # Convert to normalized path to avoid issues like trailing slashes
            try:
                value = os.path.normpath(value)
            except Exception:
                pass

            if not value.startswith(settings.FILE_MANAGER_ROOT):
                raise serializers.ValidationError(
                    'The path you are trying to access is invalid.'
                )

        return value


class RenameFileSerializer(ValidPathSerializer):
    path = serializers.CharField(required=False)
    old_name = serializers.CharField()
    new_name = serializers.CharField()


class RemoteUploadSerializer(ValidPathSerializer):
    path = serializers.CharField(required=False)
    remote_url = serializers.URLField()

    def validate(self, data):
        path = data.get('path')
        remote_url = data.get('remote_url')

        if path and remote_url:
            filename = os.path.basename(remote_url)
            dest_path = os.path.join(path, filename)

            # Check if file exists
            if os.path.exists(dest_path):
                raise serializers.ValidationError(
                    {
                        'remote_url': (
                            f'The destination file {dest_path} already exists.'
                        )
                    }
                )
        return data


class PermissionUpdateSerializer(ValidPathSerializer):
    path = serializers.CharField(required=False)
    permissions = serializers.IntegerField()


class FileUploadSerializer(ValidPathSerializer):
    file = serializers.FileField()
    path = serializers.CharField(required=False)


class ReadFileSerializer(ValidPathSerializer):
    """Defines fields required to read a file's content."""
    path = serializers.CharField()


class ExtractArchiveSerializer(ValidPathSerializer):
    """Defines fields required to extract an archive."""
    path = serializers.CharField()
    root_path = serializers.CharField(required=False)


class GenerateArchiveSerializer(ValidPathSerializer):
    """Defines fields required to generate an archive."""
    path = serializers.CharField(required=False)
    paths = serializers.CharField()


class DeleteItemSerializer(serializers.Serializer):
    """Defines fields required to delete items."""
    paths = serializers.CharField()


class FileUpdateSerializer(ValidPathSerializer):
    """Defines fields required to update a file's content."""
    path = serializers.CharField()
    content = serializers.CharField()


class ItemCreateSerializer(ValidPathSerializer):
    """Defines fields required to create a file or a directory."""
    path = serializers.CharField(required=False)
    item_name = serializers.CharField(max_length=100)
    item_type = serializers.ChoiceField(
        choices=(('file', 'File'), ('directory', 'Directory')))


class FileListSerializer(ValidPathSerializer):
    path = serializers.CharField(required=False)
    page = serializers.IntegerField(default=1)
    search = serializers.CharField(required=False)

    def validate_path(self, value):
        # Ensure we apply the base validation (which decodes the value)
        value = super().validate_path(value)
        if value:
            if not os.path.exists(value):
                raise serializers.ValidationError('Path does not exist.')
            elif not os.path.isdir(value):
                raise serializers.ValidationError('Path is not a directory.')
        return value


class MoveItemsSerializer(ValidPathSerializer):
    path = serializers.CharField(required=False)
    paths = serializers.CharField()
    action = serializers.CharField(default='move')

    def validate_action(self, value):
        if value not in ['move', 'copy']:
            raise serializers.ValidationError(
                'Invalid action specified. It should be either copy or move.')
        return value
