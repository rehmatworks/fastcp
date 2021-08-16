from rest_framework import permissions


class IsAdminOrOwner(permissions.BasePermission):
    message = 'You are not allowed.'

    def has_object_permission(self, request, view, obj):
        return request.user.is_superuser or obj.user.pk == request.user.pk