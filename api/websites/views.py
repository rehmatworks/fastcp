from rest_framework import viewsets
from rest_framework.views import APIView
from rest_framework.response import Response
from rest_framework import status
from core.models import Website
from . import serializers
from core.permissions import IsAdminOrOwner
from rest_framework import permissions
from django.db.models import Q
from .services.get_php_versions import PhpVersionListService


class PhpVersionsView(APIView):
    """Gets the list of supported PHP versions."""
    http_method_names = ['get']
    
    def get(self, request, *args, **kwargs):
        php_versions = PhpVersionListService().get_php_versions()
        return Response({
            'php_versions': php_versions
        })

class WebsiteViewSet(viewsets.ModelViewSet):
    """Website View
    
    Generates CRUD API endpoints for the website model.
    """
    queryset = Website.objects.all().order_by('-created')
    serializer_class = serializers.WebsiteSerializer
    permission_classes = [permissions.IsAuthenticated, IsAdminOrOwner]

    def filter_queryset(self, queryset):
        user = self.request.user
        if not user.is_superuser:
            queryset = queryset.filter(user=user)
        order_by =  self.request.GET.get('order_by')
        if order_by:
            if order_by.lstrip('-') in ['pk', 'label', 'created']:
                queryset = queryset.order_by(order_by)

        search_q = self.request.GET.get('q')
        if search_q:
            queryset = queryset.filter(label__icontains=search_q)
             
        return queryset