from django.urls import path, include
from api.websites.urls import router as websites_router
from api.databases.urls import router as databases_router

app_name='api'
urlpatterns=[
    path('websites/', include('api.websites.urls', namespace='websites')),
    path('databases/', include(databases_router.urls)),
    path('account/', include('api.account.urls', namespace='account')),
    path('stats/', include('api.stats.urls', namespace='stats')),
    path('file-manager/', include('api.filemanager.urls', namespace='filemanager'))
]