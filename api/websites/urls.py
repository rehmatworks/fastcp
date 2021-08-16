from rest_framework import routers
from . import views
from django.urls import path, include


router = routers.DefaultRouter()
router.register('', views.WebsiteViewSet)

app_name='websites'
urlpatterns=[
    path('php-versions/', views.PhpVersionsView().as_view(), name='php_versions'),
    path('', include(router.urls))
]