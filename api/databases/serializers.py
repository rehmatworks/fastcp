from core.models import Database
from rest_framework import serializers

# Disallowed names
DISALLOWED_NAMES = ['fastcp', 'root', 'mysql', 'test',
                    'information_schema', 'performance_schema', 'sys', 'ubuntu', 'admin']


class DatabaseSerializer(serializers.ModelSerializer):
    class Meta:
        model = Database
        fields = ['id', 'name', 'username', 'created']
        read_only_fields = ['id', 'created']

    def validate_name(self, value):
        """Ensure that it doesn't use a preserved name."""
        if value in DISALLOWED_NAMES:
            raise serializers.ValidationError(
                f'{value} is not allowed to be used as a database name.')
        return value

    def validate_username(self, value):
        """Ensure that it doesn't use a preserved username."""
        if value in DISALLOWED_NAMES:
            raise serializers.ValidationError(
                f'{value} is not allowed to be used as a username.')
        return value

    def create(self, validated_data):
        request = self.context['request']
        user = request.user

        # Ensure that user doesn't go beyond their quota
        if user.databases.count() >= user.max_dbs:
            if user.max_dbs == 1:
                limit_str = '1 database'
            else:
                limit_str = f'{user.max_dbs} databases'
            raise serializers.ValidationError(
                {'name': [f'The allowed quota limit of {limit_str} has reached.']})

        validated_data['user'] = user
        database = Database.objects.create(**validated_data)
        return database
