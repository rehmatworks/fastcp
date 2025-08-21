from django.urls import path, include
from . import views

app_name = 'filemanager'
urlpatterns = [
    path('files/', views.FileListView.as_view(), name='files'),
    path('file-manipulation/', views.FileObjectView.as_view(), name='file_manipulation'),
    path('generate-archive/', views.GenerateArchiveView.as_view(), name='generate_archive'),
    path('delete-items/', views.DeleteItemsView.as_view(), name='delete_items'),
    path('extract-archive/', views.ExtractArchiveView().as_view(), name='extract_archive'),
    path('upload-files/', views.UploadFileView().as_view(), name='upload_files'),
    path('move-items/', views.MoveItemsView().as_view(), name='move_items'),
    path('rename-item/', views.RenameItem().as_view(), name='rename_item'),
    path('update-permissions/', views.UpdatePermissions().as_view(), name='update_permissions'),
    path('remote-fetch/', views.RemoteUpload().as_view(), name='remote_fetch'),
]