from core.models import Database
from rest_framework import serializers
    
class DatabaseSerializer(serializers.ModelSerializer):
    class Meta:
        model = Database
        fields = ['id', 'name', 'username', 'created']
        read_only_fields = ['id', 'created']

    def create(self, validated_data):
        request = self.context['request']
        user = request.user
        
         # Ensure that user doesn't go beyond their quota
        if user.databases.count() >= user.max_dbs:
            if user.max_dbs == 1:
                limit_str = '1 database'
            else:
                limit_str = f'{user.max_dbs} databases'
            raise serializers.ValidationError({'name': [f'The allowed quota limit of {limit_str} has reached.']})
        
        validated_data['user'] = user
        database = Database.objects.create(**validated_data)
        return database