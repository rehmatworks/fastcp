from rest_framework.viewsets import ModelViewSet
from rest_framework.views import APIView
from rest_framework.response import Response
from rest_framework import status
from rest_framework.permissions import IsAuthenticated
from rest_framework.decorators import action
from core.models import FTPUser
from django.shortcuts import get_object_or_404
from .serializers import FTPUserSerializer
import os
import secrets
import string
import subprocess


def generate_password(length=16):
    """Generate a secure random password"""
    alphabet = string.ascii_letters + string.digits + string.punctuation
    return ''.join(secrets.choice(alphabet) for i in range(length))


class FTPUserViewSet(ModelViewSet):
    """FTP User management viewset."""
    permission_classes = [IsAuthenticated]
    serializer_class = FTPUserSerializer

    def get_queryset(self):
        """Return FTP users for the current user only."""
        return FTPUser.objects.filter(user=self.request.user)

    def perform_create(self, serializer):
        """Create a new FTP user."""
        # Generate paths
        username = serializer.validated_data['username']
        home_dir = f'/app/ftp/data/{username}'
        
        # Create FTP user
        ftp_user = serializer.save(
            user=self.request.user,
            home_dir=home_dir
        )
        
        # Create the FTP user in Pure-FTPd
        subprocess.run([
            'docker', 'exec', 'fastcp-ftp',
            'pure-pw', 'useradd', username, '-f', '/etc/pure-ftpd/passwd/pureftpd.passwd',
            '-u', 'ftpuser', '-g', 'ftpuser', '-d', home_dir, '-m'
        ], input=f'{serializer.validated_data["password"]}\n{serializer.validated_data["password"]}\n'.encode(), check=True)
        
        subprocess.run(['docker', 'exec', 'fastcp-ftp', 'pure-pw', 'mkdb', '/etc/pure-ftpd/pureftpd.pdb', '-f', '/etc/pure-ftpd/passwd/pureftpd.passwd'], check=True)
        
        return ftp_user

    def perform_update(self, serializer):
        """Update the FTP user."""
        # Update the FTP user in Pure-FTPd
        username = serializer.instance.username
        if 'password' in serializer.validated_data:
            subprocess.run([
                'docker', 'exec', 'fastcp-ftp',
                'pure-pw', 'passwd', username, '-f', '/etc/pure-ftpd/passwd/pureftpd.passwd',
                '-m'
            ], input=f'{serializer.validated_data["password"]}\n{serializer.validated_data["password"]}\n'.encode(), check=True)
            
            subprocess.run(['docker', 'exec', 'fastcp-ftp', 'pure-pw', 'mkdb', '/etc/pure-ftpd/pureftpd.pdb', '-f', '/etc/pure-ftpd/passwd/pureftpd.passwd'], check=True)

        return super().perform_update(serializer)

    def perform_destroy(self, instance):
        """Delete the FTP user."""
        # Delete from Pure-FTPd
        subprocess.run([
            'docker', 'exec', 'fastcp-ftp',
            'pure-pw', 'userdel', instance.username, '-f', '/etc/pure-ftpd/passwd/pureftpd.passwd',
            '-m'
        ], check=True)
        
        subprocess.run(['docker', 'exec', 'fastcp-ftp', 'pure-pw', 'mkdb', '/etc/pure-ftpd/pureftpd.pdb', '-f', '/etc/pure-ftpd/passwd/pureftpd.passwd'], check=True)
        
        # Delete the home directory
        if os.path.exists(instance.home_dir):
            try:
                subprocess.run(['rm', '-rf', instance.home_dir], check=True)
            except subprocess.CalledProcessError:
                pass
                
        return super().perform_destroy(instance)

    @action(detail=True, methods=['post'])
    def reset_password(self, request, pk=None):
        """Reset FTP user password."""
        instance = self.get_object()
        new_password = generate_password()

        # Update the password in Pure-FTPd
        subprocess.run([
            'docker', 'exec', 'fastcp-ftp',
            'pure-pw', 'passwd', instance.username, '-f', '/etc/pure-ftpd/passwd/pureftpd.passwd',
            '-m'
        ], input=f'{new_password}\n{new_password}\n'.encode(), check=True)
        
        subprocess.run(['docker', 'exec', 'fastcp-ftp', 'pure-pw', 'mkdb', '/etc/pure-ftpd/pureftpd.pdb', '-f', '/etc/pure-ftpd/passwd/pureftpd.passwd'], check=True)

        # Update the model
        instance.password = new_password
        instance.save()

        return Response({'password': new_password})
