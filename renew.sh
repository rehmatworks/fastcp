#!/bin/bash
cd /etc/fastcp/ && \
source venv/bin/activate && \
cd fastcp && \
source vars.sh && \
python manage.py runcrons