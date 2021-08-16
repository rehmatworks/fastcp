from rest_framework import serializers
from core.models import Website


class WebsiteSerializer(serializers.ModelSerializer):
    class Meta:
        model = Website
        fields = ['label', 'user', 'has_ssl', 'php']
        read_only_fields = ['has_ssl']

    def create(self, validated_data):
        website = Website.objects.create(**validated_data)
        return website