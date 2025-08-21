from rest_framework import serializers
import os
from django.conf import settings


class ValidPathSerializer(serializers.Serializer):
    # For security reasons, let's validate the
    # path and ensure that it is allowed. Further
    # more granular validation is done in base_service
    # module
    def validate_path(self, value):
        if value and not value.startswith(settings.FILE_MANAGER_ROOT):
            raise serializers.ValidationError(
                'The path you are trying to access is invalid.')
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
                raise serializers.ValidationError({
                    'remote_url': f'The destination file {dest_path} already exists.'})
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
