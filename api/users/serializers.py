from rest_framework import serializers
from core.models import User
from core.signals import create_user


# Disallow some system usernames

DISALLOWED_USERNAMES = ['admin', 'superuser', 'administrator', 'www-data', 'mysql', 'ubuntu']


class UserSearilizer(serializers.ModelSerializer):
    """User serializer.

    This serializer class serializes the user model. The user model deals with the SSH user accounts on the system.
    """
    class Meta:
        model = User
        fields = [
            'id', 'username', 'date_joined', 'total_dbs', 'uid', 'is_active',
            'total_sites', 'max_storage', 'storage_used', 'max_dbs', 'max_sites',
        ]
        read_only_fields = [
            'id', 'date_joined', 'total_dbs', 'uid', 'storage_used', 'total_sites',
        ]

    def validate_username(self, value):
        """Ensure that username is valid."""
        if value and value.lower() in DISALLOWED_USERNAMES:
            raise serializers.ValidationError('The provided username is not allowed.')
        return value

    def create(self, validated_data):
        """Create user"""
        request = self.context['request']
        user = User.objects.create(**validated_data)
        create_user.send(sender=user, password=request.POST.get('password'))
        return user
