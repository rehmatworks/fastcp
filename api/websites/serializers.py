from rest_framework import serializers
from core.models import Website, Domain, Database
import validators
from core import signals
from core.models import User
from core.utils import system
from django.db.models import Q


class ChangePhpVersionSerializer(serializers.ModelSerializer):
    class Meta:
        model = Website
        fields = ["php"]


class DomainSerializer(serializers.ModelSerializer):
    class Meta:
        model = Domain
        fields = "__all__"

    def validate_domain(self, value):
        """Ensure that the value is a valid domain"""
        if not validators.domain(value):
            raise serializers.ValidationError(f"{value} is not a valid domain.")

        # A domain should always be lower case
        if value:
            value = value.lower()
        return value


class WebsiteSerializer(serializers.ModelSerializer):
    domains = DomainSerializer(many=True, required=False)

    class Meta:
        model = Website
        fields = [
            "id",
            "label",
            "user",
            "metadata",
            "domains",
            "has_ssl",
            "php",
        ]
        read_only_fields = [
            "id",
            "has_ssl",
            "root_path",
            "domains",
            "metadata",
            "user",
        ]

    def validate_domains(self, value):
        # Validate domains
        domains = list(filter(None, [d.strip() for d in value.strip().split(",")]))
        if len(domains) == 0:
            raise serializers.ValidationError({"domains": ["You have not provided any domains."]})

        for domain in domains:
            # Check if domain is valid
            if not validators.domain(domain):
                raise serializers.ValidationError({"domains": [f"{domain} is not a valid domain."]})

            # Ensure domain is unique
            if Domain.objects.filter(domain=domain).count():
                raise serializers.ValidationError({"domains": [f"{domain} already exists in the database."]})

        return domains

    def create(self, validated_data):
        request = self.context["request"]
        domains = request.POST.get("domains")
        domains = self.validate_domains(domains)
        is_wp = request.POST.get("website_type") == "wordpress"

        user = request.user
        if not user.is_superuser:
            ssh_user = user
        else:
            ssh_user = request.POST.get("ssh_user")

            if ssh_user:
                if ssh_user == "root":
                    raise serializers.ValidationError({"username": "You cannot create any websites for root user."})
                else:
                    ssh_user = User.objects.filter(username=ssh_user).first()

            if not ssh_user:
                raise serializers.ValidationError({
                    "username": "An SSH user should be selected as the owner of this website."
                })

        # Ensure that user doesn't go beyond their quota
        if ssh_user.websites.count() >= ssh_user.max_sites:
            if ssh_user.max_sites == 1:
                limit_str = "1 website"
            else:
                limit_str = f"{ssh_user.max_sites} websites"

            raise serializers.ValidationError({"label": [f"The allowed quota limit of {limit_str} has reached."]})

        validated_data["user"] = ssh_user
        website = Website.objects.create(**validated_data)

        # Create domains
        for domain in domains:
            website.domains.create(domain=domain)

        # Send domains updated signal so vhost file will be created.
        signals.domains_updated.send(sender=website)

        if is_wp:
            website.is_wp = True
            website.save()
            i = ""
            while True:
                dbname = f"wp_db{i}"
                dbuser = f"wp_user{i}"
                if isinstance(i, str):
                    i = 0
                if Database.objects.filter(Q(name=dbname) | Q(username=dbuser)).count() == 0:
                    break
                i += 1

            dbobj = ssh_user.databases.create(name=dbname, username=dbuser)
            dbpassword = system.rand_passwd()
            signals.create_db.send(sender=dbobj, password=dbpassword)
            wp_data = {"dbname": dbname, "dbuser": dbuser, "dbpassword": dbpassword}
            signals.install_wp.send(sender=website, **wp_data)

        return website
