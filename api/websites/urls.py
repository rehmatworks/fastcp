from rest_framework import routers
from . import views
from django.urls import path, include


router = routers.DefaultRouter()
router.register('', views.WebsiteViewSet)

app_name='websites'
urlpatterns=[
    path('<int:id>/reset-password/', views.PasswordUpdateView().as_view(), name='update_password'),
    path('<int:id>/change-php/', views.ChangePHPVersion().as_view(), name='change_php'),
    path('<int:id>/add-domain/', views.DomainAddView().as_view(), name='add_domain'),
    path('<int:id>/delete-domain/<int:dom_id>/', views.DeleteDomainView().as_view(), name='del_domain'),
    path('php-versions/', views.PhpVersionsView().as_view(), name='php_versions'),
    path('', include(router.urls))
]