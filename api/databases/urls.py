from django.urls import path, include
from rest_framework import routers
from . import views


router = routers.DefaultRouter()
router.register("", views.DatabaseViewSet)

app_name = "databases"
urlpatterns = [
    path("<int:id>/reset-password/", views.ResetPasswordView().as_view(), name="reset_sql_password"),
    path("", include(router.urls)),
]
