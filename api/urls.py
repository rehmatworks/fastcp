from django.urls import path, include
from api.databases.urls import router as databases_router

app_name='api'
urlpatterns=[
    path('websites/', include('api.websites.urls', namespace='websites')),
    path('ssh-users/', include('api.users.urls', namespace='users')),
    path('databases/', include('api.databases.urls', namespace='databases')),
    path('account/', include('api.account.urls', namespace='account')),
    path('stats/', include('api.stats.urls', namespace='stats')),
    path('file-manager/', include('api.filemanager.urls', namespace='filemanager')),
    path('ftp/', include('api.ftp.urls', namespace='ftp'))
]