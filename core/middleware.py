from django.http import HttpResponseNotFound
import re

class BlockUnwantedExtensionsMiddleware:
    """Middleware to block unwanted file extensions in URLs."""
    
    def __init__(self, get_response):
        self.get_response = get_response
        # Extensions that should be blocked
        self.blocked_extensions = re.compile(r'\.(php|aspx?|jsp|cgi|pl|exe|sh|bat)$', re.I)
        
    def __call__(self, request):
        # Check if URL ends with unwanted extension
        if self.blocked_extensions.search(request.path_info):
            return HttpResponseNotFound("Not Found", content_type='text/plain')
            
        response = self.get_response(request)
        return response
