from core.utils import filesystem as cpfs
import os
from pathlib import Path
from django.core.paginator import Paginator, EmptyPage


class ListFileService(object):
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
        
        # List of protected files and folders (in root dir)
        # For example, in run directory, we are storing socket files
        # And a non-root user should not see this directory in file manager in
        # general circumstances.
        PROTECTED_LIST = ['run', '.profile', '.bashrc', '.bash_logout', '.bash_history', '.local']
        
        BASE_PATH = cpfs.get_user_path(user)
            
        if not path or not os.path.exists(path):
            path = BASE_PATH
        
        # Ensure intended root path
        if not path.startswith(BASE_PATH):
            path = BASE_PATH
                           
        path = Path(path)
        files = []
        for p in path.iterdir():
            try:
                data = cpfs.get_path_info(p)
                if not search or search.lower() in data.get('name').lower():
                    if str(path) == BASE_PATH and data.get('name').lower() in PROTECTED_LIST:
                        continue
                    files.append(data)
            except PermissionError:
                pass
        
        paginator = Paginator(files, 15)
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
            
        data = {
            'links': {
                'next': next_page,
                'previous': previous_page
            },
            'count': len(files),
            'results': page.object_list
        }
        
        return data