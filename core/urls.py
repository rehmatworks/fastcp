from django.urls import path
from . import views


app_name = 'sites'
urlpatterns = [
    path('sign-in/', views.sign_in, name='login'),
    path('sign-out/', views.sign_out, name='logout'),
    path('download-file/', views.download_file, name='download')
]