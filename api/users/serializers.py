from rest_framework import serializers
from core.models import User


class UserSearilizer(serializers.ModelSerializer):
    """User serializer.
    
    This serializer class serializes the user model. The user model deals with the SSH user accounts on the system.
    """
    class Meta:
        model = User
        fields = ['id', 'username', 'date_joined', 'total_dbs', 'uid', 'is_active', 'total_sites', 'max_storage', 'storage_used', 'max_dbs', 'max_sites']
        read_only_fields = ['id', 'date_joined', 'total_dbs', 'uid', 'storage_used', 'total_sites']
        
    
    def create(self, validated_data):
        """Create user"""
        user = User.objects.create(
            username=validated_data.get('username')
        )
        return user