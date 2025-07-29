# Dockerfile for fastcp Django project

FROM python:3.12-slim

# Install system dependencies for MySQL, PostgreSQL, PHP, and Python packages
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        gcc \
        python3-dev \
        libffi-dev \
        libpq-dev \
        default-libmysqlclient-dev \
        pkg-config \
        php-fpm \
        php-cli \
        libmariadb-dev \
        build-essential \
    && rm -rf /var/lib/apt/lists/*

ENV PYTHONDONTWRITEBYTECODE 1
ENV PYTHONUNBUFFERED 1

WORKDIR /app

COPY requirements.txt ./
RUN pip install --upgrade pip && pip install -r requirements.txt && pip install psycopg2-binary

COPY . .

CMD ["gunicorn", "fastcp.wsgi:application", "--bind", "0.0.0.0:8000"]
