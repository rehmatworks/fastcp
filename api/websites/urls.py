from rest_framework import routers
from . import views
from django.urls import path, include


router = routers.DefaultRouter()
router.register('', views.WebsiteViewSet)

app_name='websites'
urlpatterns=[
    path('<int:id>/reset-password/', views.PasswordUpdateView().as_view(), name='update_password'),
    path('<int:id>/change-php/', views.ChangePHPVersion().as_view(), name='change_php'),
    path('php-versions/', views.PhpVersionsView().as_view(), name='php_versions'),
    path('', include(router.urls))
]