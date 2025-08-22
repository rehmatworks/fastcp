from django.urls import path
from . import views

app_name='account'
urlpatterns=[
    path('', views.AccountView.as_view(), name='account')
]