from core.models import Database
from rest_framework import serializers
    
class DatabaseSerializer(serializers.ModelSerializer):
    class Meta:
        model = Database
        fields = ['id', 'name', 'username', 'created']
        read_only_fields = ['id', 'created']

    def create(self, validated_data):
        database = Database.objects.create(**validated_data)
        return database