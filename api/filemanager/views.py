from rest_framework.views import APIView
from rest_framework.response import Response
from rest_framework import status
from . import serializers
from rest_framework.parsers import MultiPartParser
from .services.delete_items import DeleteItemsService
from .services.list_files import ListFileService
from .services.extract_archive import ExtractArchiveService
from .services.generate_archive import GenerateArchiveService
from .services.update_file import UpdateFileService
from .services.create_item import CreateItemService
from .services.read_file import ReadFileService
from .services.move_items import MoveDataService
from .services.file_upload import FileUploadService
from .services.rename_item import RenameItemService
from .services.update_permissions import UpdatePermissionService


class UploadFileView(APIView):
    """Upload Files.
    
    This view allows the users to upload files using file manager or using HTTP API.
    """
    http_method_names = ['post']
    parser_classes = [MultiPartParser]
    
    def post(self, request, *args, **kwargss):
        """Handle file upload"""
        s = serializers.FileUploadSerializer(data=request.data)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)
        
        if FileUploadService(request).upload_file(s.validated_data):
            return Response({
                'message': 'File has been successfully uploaded.'
            })
        else:
            return Response({
                'error': 'File cannot be uploaded to the specified location.'
            }, status=status.HTTP_400_BAD_REQUEST)

class RemoteUpload(APIView):
    """Remote Upload.
    
    This view allows the users to fetch files from remote URLs. The remote URLs must host a public file.
    """
    http_method_names = ['post']
    
    def post(self, request, *args, **kwargss):
        """Handle file upload"""
        s = serializers.RemoteUploadSerializer(data=request.data)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)
        
        if FileUploadService(request).remote_upload(s.validated_data):
            return Response({
                'message': 'File has been successfully fetched.'
            })
        else:
            return Response({
                'error': 'File cannot be fetched to the specified location.'
            }, status=status.HTTP_400_BAD_REQUEST)


class MoveItemsView(APIView):
    """Move Items.
    
    This class is responsible to move items from one location to another.
    """
    http_method_names = ['post']
    
    def post(self, request, *args, **kwargs):
        """Handles moving of items."""
        s = serializers.MoveItemsSerializer(data=request.POST)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)

        if MoveDataService(request).move_data(s.validated_data):
            return Response({
                'message': 'Items have been relocated successfully.'
            })
        else:
            return Response({
                'error': 'An error occured while moving the items.'
            }, status=status.HTTP_400_BAD_REQUEST)

class FileObjectView(APIView):
    """File Object View.
    
    This view is responsible for returning a file's content as well as it creates an item and updates the contents of the item on the disk.
    """
    http_method_names = ['get', 'put', 'post']
    
    def get(self, request, *args, **kwargs):
        """Read a file.
        
        This method attempts to read the contents of a file from the disk and returns the content.
        """
        s = serializers.ReadFileSerializer(data=request.GET)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)
        
        content = ReadFileService(request).read_file(s.validated_data)
        if content is not None:
            return Response({
                'content': content
            })
        else:
            return Response({
                'content': 'File not available for editing.'
            }, status=status.HTTP_400_BAD_REQUEST)
    
    def post(self, request, *args, **kwargs):
        """Create Item
        
        This function attempts to create a new file or a directory in the root of selected dir. If no file ID is passed,
        then the root of file manager is selected as the root directory to create the new item in.
        """
        s = serializers.ItemCreateSerializer(data=request.POST)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)
        
        if CreateItemService(request).create_item(s.validated_data):
            return Response({
                'message': 'New item has been created successfully.'
            })
        else:
            return Response({
                'error': 'New item cannot be created with this name.'
            }, status=400)
    
    def put(self, request, *args, **kwargs):
        """Update File
        
        This method attempts to update the contents of a file on the disk.
        """
        s = serializers.FileUpdateSerializer(data=request.POST)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)
        
        if UpdateFileService(request).update_file(s.validated_data):
            return Response({
                'message': 'File has been updated.'
            })
        else:
            return Response({
                'content': 'File cannot be updated.'
            }, status=status.HTTP_400_BAD_REQUEST)


class GenerateArchiveView(APIView):
    """Generate Archive
    
    This view generate a ZIP file for the provided file paths in the provided root path or the file manager root if root
    path was not supplied.
    """
    http_method_names = ['post']
    
    def post(self, request):
        s = serializers.GenerateArchiveSerializer(data=request.POST)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)
        
        if GenerateArchiveService(request).generate_archive(s.validated_data):
            return Response({
                'message': 'Archive has been successfully generated.'
            })
        else:
            return Response({
                'error': 'Archive cannot be generated.'
            }, status=status.HTTP_400_BAD_REQUEST)


class ExtractArchiveView(APIView):
    """Extract Archive
    
    This function attempts to extract the archive in the current directory. If the archive needs to
    be extracted somewhere else, that's not possible yet. In order to achive that, users will first
    need to move the archive file to the desired path and then they will have extract it there.
    """
    http_method_names = ['post']
    
    def post(self, request, *args, **kwargs):
        s = serializers.ExtractArchiveSerializer(data=request.POST)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)

        if ExtractArchiveService(request).extract_archive(s.validated_data):
            return Response({
                'message': 'The archive has been extracted successfully.'
            })
        else:
            return Response({
                'error': 'Archive data cannot be extracted.'
            }, status=status.HTTP_400_BAD_REQUEST)


class DeleteItemsView(APIView):
    """Delete Items.
        
    Delete selected items from the the disk. This action irreversible and this function permanently
    deletes the selected files.
    """
    
    http_method_names = ['post']
    
    def post(self, request, *args, **kwargs):
        s = serializers.DeleteItemSerializer(data=request.POST)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)
        
        if DeleteItemsService(request).delete_items(s.validated_data):
            return Response({
                'message': 'The selected items have been successfully deleted.'
            })
        else:
            return Response({
                'error': 'Selected files cannot be deleted.'
            }, status=status.HTTP_400_BAD_REQUEST)
    
class FileListView(APIView):
    """File View
    
    List files in the provided path.
    """
    http_method_names = ['get']
    
    def get(self, request, *args, **kwargs):
        s = serializers.FileListSerializer(data=request.GET)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)
        
        data = ListFileService(request).get_files_list(s.validated_data)
        if data:
            return Response(data)
        else:
            return Response({
                'message': 'Directory listing cannot be retrieved.'
            }, status=status.HTTP_400_BAD_REQUEST)

class RenameItem(APIView):
    """Rename an item.
    
    Rename a directory or a file.
    """
    http_method_names = ['post']
    
    def post(self, request, *args, **kwargs):
        s = serializers.RenameFileSerializer(data=request.POST)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)

        if RenameItemService(request).rename_item(s.validated_data):
            return Response({'status': True})
        else:
            return Response({
                'message': 'The item cannot be renamed.'
            }, status=status.HTTP_400_BAD_REQUEST)

class UpdatePermissions(APIView):
    """Update permissions.
    
    Update permissions on a file or a directory.
    """
    http_method_names = ['post']
    
    def post(self, request, *args, **kwargs):
        s = serializers.PermissionUpdateSerializer(data=request.POST)
        if not s.is_valid():
            return Response(s.errors, status=status.HTTP_422_UNPROCESSABLE_ENTITY)

        if UpdatePermissionService(request).update_permissions(s.validated_data):
            return Response({'status': True})
        else:
            return Response({
                'message': 'The permissions cannot be updated.'
            }, status=status.HTTP_400_BAD_REQUEST)