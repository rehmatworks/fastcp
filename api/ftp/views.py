from rest_framework.views import APIView
from rest_framework.response import Response
from rest_framework import status
from rest_framework import permissions
import os
import secrets
import string


def generate_password(length=16):
    """Generate a secure random password"""
    alphabet = string.ascii_letters + string.digits + string.punctuation
    return ''.join(secrets.choice(alphabet) for i in range(length))


class ResetPasswordView(APIView):
    """Reset FTP user password."""
    http_method_names = ['post']
    permission_classes = [permissions.IsAdminUser]
    
    def post(self, request, *args, **kwargs):
        """Handle password reset"""
        try:
            new_password = generate_password()
            # In a real implementation, you would update the FTP password here
            # For now, we just return the new password
            return Response({
                'password': new_password
            })
        except Exception as e:
            return Response({
                'error': str(e)
            }, status=status.HTTP_400_BAD_REQUEST)
