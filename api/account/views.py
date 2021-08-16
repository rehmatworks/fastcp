from rest_framework.views import APIView
from rest_framework.response import Response
from rest_framework import status


class AccountView(APIView):
    """Account View
    
    Returns the account details of an authenticated user.
    """
    http_method_names = ['get']
    def get(self, request, *args, **kw):
        user = request.user
        result = {
            'username': user.username,
            'is_root': user.is_superuser
        }
        response = Response(result, status=status.HTTP_200_OK)
        return response