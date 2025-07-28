from core.models import User
User.objects.filter(password=None).delete()
print('Deleted users with no password.')
