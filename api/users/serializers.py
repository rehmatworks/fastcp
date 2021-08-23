from rest_framework import serializers
from core.models import User


class UserSearilizer(serializers.ModelSerializer):
    """User serializer.
    
    This serializer class serializes the user model. The user model deals with the SSH user accounts on the system.
    """
    class Meta:
        model = User
        fields = ['username']
        
    
    def create(self, validated_data):
        """Create user"""
        user = User.objects.create(
            username=validated_data.get('username')
        )
        return user