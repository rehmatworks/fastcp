from core.models import User
User.objects.filter(username='admin').delete()
print('Deleted admin user.')
