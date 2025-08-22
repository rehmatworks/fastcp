from rest_framework import serializers
from core.models import FTPUser


class FTPUserSerializer(serializers.ModelSerializer):
    """FTP User serializer."""

    class Meta:
        model = FTPUser
        fields = ['id', 'username', 'password', 'website', 'created_at', 'updated_at']
        extra_kwargs = {
            'password': {'write_only': True},
            'website': {'required': True}
        }

    def validate_username(self, value):
        """Validate username is unique."""
        if FTPUser.objects.filter(username=value).exists():
            raise serializers.ValidationError("Username already exists")
        return value

    def validate_website(self, value):
        """Validate website belongs to user."""
        if value.user != self.context['request'].user:
            raise serializers.ValidationError("Website does not belong to user")
        return value
