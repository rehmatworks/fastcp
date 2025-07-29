from django.urls import path
from . import views

app_name='ftp'
urlpatterns=[
    path('reset-password/', views.ResetPasswordView.as_view(), name='reset_password'),
]
