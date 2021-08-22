from django.urls import path, include
from . import views
from rest_framework import routers


router = routers.DefaultRouter()
router.register('', views.UsersViewSet)

app_name='sshusers'
urlpatterns=[
    path('', include(router.urls)),
]