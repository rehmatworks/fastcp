from rest_framework import serializers
from core.models import Website


class WebsiteSerializer(serializers.ModelSerializer):
    class Meta:
        model = Website
        fields = ['label', 'user', 'domains', 'has_ssl', 'php']
        read_only_fields = ['has_ssl', 'domains', 'user']
        
    
    def validate_domains(self, value):
        domains = list(filter(None, [domain.strip() for domain in value.strip().split(',')]))
        if len(domains) == 0:
            raise serializers.ValidationError({'domains': ['You have not provided any domains.']})

    def create(self, validated_data):
        self.validate_domains(self.context['request'].POST.get('domains'))
        user = self.context['request'].user
        validated_data['user'] = user
        website = Website.objects.create(**validated_data)
        return website