from rest_framework.views import APIView
from rest_framework import viewsets
from rest_framework import permissions
from core.models import User
from rest_framework.response import Response
from . import serializers


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
        