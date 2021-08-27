from core.utils import filesystem as cpfs
import os
from pathlib import Path
from django.core.paginator import Paginator, EmptyPage
from .base_service import BaseService


class ListFileService(BaseService):
    """List Files
    
    Scans the filesystem for the provided path and returns a dict containing paginated paths list.    
    """
    def __init__(self, request):
        self.request = request
        
    
    def get_files_list(self, validated_data: dict) -> dict:
        """Gets the list of files
        
        Gets the paginated list of files and directories paths.
        
        Args:
            validated_data (dict): The validated data from serializers.
        
        Returns:
            dict: Containing paginated filesystem paths list.
        """
        path = validated_data.get('path')
        search = validated_data.get('search')
        user = self.request.user
        
        if path:                
            path = Path(path)
            files = []
            for p in path.iterdir():
                try:
                    data = cpfs.get_path_info(p)
                    if not search or search.lower() in data.get('name').lower():
                        if self.is_allowed(p, user):
                            files.append(data)
                except PermissionError:
                    pass
            
            paginator = Paginator(files, 30)
            try:
                page = paginator.page(validated_data.get('page'))
            except EmptyPage:
                page = paginator.page(1)
            
            try:
                next_page = page.next_page_number()
            except EmptyPage:
                next_page = None
            
            try:
                previous_page = page.previous_page_number()
            except EmptyPage:
                previous_page = None
            
            try:
                segments = enumerate([path for path in str(path).split('/') if len(path.strip()) > 0])
            except:
                segments = []
            
            data = {
                'segments': segments,
                'links': {
                    'next': next_page,
                    'previous': previous_page
                },
                'count': len(files),
                'results': page.object_list
            }
            
            return data
        return None