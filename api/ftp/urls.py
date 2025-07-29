from django.urls import path, include
from rest_framework.routers import DefaultRouter
from .views import FTPUserViewSet

app_name = 'ftp'

router = DefaultRouter()
router.register(r'users', FTPUserViewSet, basename='ftpuser')

urlpatterns = [
    path('', include(router.urls)),
]
