import os
import django

# Configure Django
os.environ.setdefault('DJANGO_SETTINGS_MODULE', 'fastcp.settings')
django.setup()

# The imports below are valid for this script after Django is configured. Silence E402.
from api.filemanager.serializers import FileListSerializer  # noqa: E402
from django.conf import settings  # noqa: E402
from urllib.parse import quote  # noqa: E402


def try_validate(path):
    s = FileListSerializer()
    try:
        v = s.validate_path(path)
        print(f"OK: input={path!r} -> {v!r}")
    except Exception as e:
        print(f"ERR: input={path!r} -> {e}")


if __name__ == '__main__':
    root = settings.FILE_MANAGER_ROOT
    # Make a synthetic subdir path (do not require actual existence for the serializer's decode step)
    # But FileListSerializer.validate_path checks os.path.exists and isdir, so we need a real directory.
    # We'll create a temporary directory under FILE_MANAGER_ROOT if possible; otherwise skip.
    test_sub = os.path.join(root, 'tmp_test_path')
    created = False
    try:
        if not os.path.exists(test_sub):
            # try to create parent dirs if allowed (may fail on permission)
            os.makedirs(test_sub, exist_ok=True)
            created = True
    except Exception as e:
        print('Could not create test directory under FILE_MANAGER_ROOT:', e)
        print('Proceeding with PATH values but FileListSerializer may reject non-existent path.')

    candidates = [
        test_sub,
        quote(test_sub),
        quote(quote(test_sub)),
    ]

    print('FILE_MANAGER_ROOT =', root)
    for p in candidates:
        try_validate(p)

    # cleanup
    if created:
        try:
            os.rmdir(test_sub)
        except Exception:
            pass
