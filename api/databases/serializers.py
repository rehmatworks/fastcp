from core.models import Database, User
from rest_framework import serializers
from core.signals import create_db


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

        if not user.is_superuser:
            ssh_user = user
        else:
            ssh_user = request.POST.get('ssh_user')
            
            if ssh_user:
                if ssh_user == 'root':
                    raise serializers.ValidationError({'user': 'You cannot create any databases for root user.'})
                else:
                    ssh_user = User.objects.filter(username=ssh_user).first()
            
            if not ssh_user:
                raise serializers.ValidationError({'username': 'An SSH user should be selected as the owner of this website.'})

        # Ensure that user doesn't go beyond their quota
        if ssh_user.databases.count() >= ssh_user.max_dbs:
            if ssh_user.max_dbs == 1:
                limit_str = '1 database'
            else:
                limit_str = f'{ssh_user.max_dbs} databases'
            raise serializers.ValidationError(
                {'name': [f'The allowed quota limit of {limit_str} has reached.']})

        validated_data['user'] = ssh_user
        database = Database.objects.create(**validated_data)
        create_db.send(sender=database, password=request.POST.get('password'))
        return database
