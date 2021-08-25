from rest_framework import viewsets
from core.models import Database
from . import serializers
from core.permissions import IsAdminOrOwner
from rest_framework import permissions
from django.db.models import Q
from rest_framework.views import APIView
from rest_framework.response import Response
from rest_framework import status
from core.utils.system import change_db_password


class ResetPasswordView(APIView):
    """Reset SSH user password."""
    http_method_names = ['post']
    
    def post(self, request, *args, **kwargs):
        user = request.user
        db_id = kwargs.get('id')

        if user.is_superuser:
            db_obj = Database.objects.filter(pk=db_id).first()
        else:
            db_obj = user.databases.filter(pk=db_id).first()
        
        if not db_obj:
            return Response({
                'message': 'The requested database cannot be found.'
            }, status=status.HTTP_404_NOT_FOUND)
        
        # Update password
        password = change_db_password(db_obj.username)
        
        if password:
            # Return the new password
            return Response({
                'message': 'Password has been updated.',
                'new_password': password
            })
        else:
            return Response({
                'message': 'Password cannot be updated for this user.'
            }, status=status.HTTP_404_NOT_FOUND)

class DatabaseViewSet(viewsets.ModelViewSet):
    """Database View
    
    Generates CRUD API endpoints for the database model. 
    """
    queryset = Database.objects.all().order_by('-created')
    serializer_class = serializers.DatabaseSerializer
    permission_classes = [permissions.IsAuthenticated, IsAdminOrOwner]

    def filter_queryset(self, queryset):
        user = self.request.user
        if not user.is_superuser:
            queryset = queryset.filter(user=user)
        order_by =  self.request.GET.get('order_by')
        if order_by:
            if order_by.lstrip('-') in ['pk', 'name', 'username', 'created']:
                queryset = queryset.order_by(order_by)

        search_q = self.request.GET.get('q')
        if search_q:
            queryset = queryset.filter(Q(name__icontains=search_q) | Q(username__icontains=search_q))
             
        return queryset
    
    def destroy(self, request, *args, **kwargs):
        database = Database.objects.filter(pk=kwargs.get('pk')).first()
        return super(DatabaseViewSet, self).destroy(request, *args, **kwargs)