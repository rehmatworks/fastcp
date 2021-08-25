from django.urls import path, include
from . import views
from rest_framework import routers


router = routers.DefaultRouter()
router.register('', views.UsersViewSet)

app_name='sshusers'
urlpatterns=[
    path('<int:id>/reset-password/', views.ResetPasswordView().as_view(), name='reset_password'),
    path('', include(router.urls)),
]