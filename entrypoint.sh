#!/bin/bash
set -e
# Entrypoint for web container: run migrations, collectstatic, then exec CMD
python manage.py migrate --noinput
python manage.py collectstatic --noinput
exec "$@"
