from django.urls import path
from . import views

app_name='stats'
urlpatterns=[
    path('common/', views.StatsView.as_view(), name='stats'),
    path('hardware/', views.HardwareinfoView.as_view(), name='hardwareinfo')
]