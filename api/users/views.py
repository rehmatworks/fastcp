from rest_framework.views import APIView
from rest_framework import viewsets
from rest_framework import status
from rest_framework import permissions
from core.models import User
from rest_framework.response import Response
from . import serializers
from core.utils.system import change_password


class ResetPasswordView(APIView):
    """Reset SSH user password."""
    http_method_names = ['post']
    
    def post(self, request, *args, **kwargs):
        user = request.user
        user_id = kwargs.get('id')

        if user.is_superuser and user_id != user.id:
            user = User.objects.filter(pk=user_id).first()
        
        if not user:
            return Response({
                'message': 'The requested user account cannot be found.'
            }, status=status.HTTP_404_NOT_FOUND)
        
        # Update password
        password = change_password(user.username)
        
        # Return the new password
        return Response({
            'message': 'Password has been updated.',
            'new_password': password,
            'user': user.username
        })
        

class UsersViewSet(viewsets.ModelViewSet):
    """User View
    
    Generates CRUD API endpoints for the user model. Only non-root or users without superuser privileg
    are returned as only non-root users are allowed to be used when creating websites.
    """
    queryset = User.objects.all().order_by('-pk')
    serializer_class = serializers.UserSearilizer
    permission_classes = [permissions.IsAuthenticated, permissions.IsAdminUser]

    def filter_queryset(self, queryset):
        queryset = queryset.filter(is_superuser=False)
        order_by =  self.request.GET.get('order_by')
        if order_by:
            if order_by.lstrip('-') in ['pk', 'username']:
                queryset = queryset.order_by(order_by)

        search_q = self.request.GET.get('q')
        if search_q:
            queryset = queryset.filter(username__icontains=search_q)
             
        return queryset
        