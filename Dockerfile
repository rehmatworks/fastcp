# Dockerfile for fastcp Django project

FROM python:3.12-slim

# Install system dependencies for MariaDB, PostgreSQL, PHP, and Python packages
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        curl \
        gnupg \
        ca-certificates \
        gcc \
        python3-dev \
        libffi-dev \
        libpq-dev \
        pkg-config \
        php-fpm \
        php-cli \
        libmariadb-dev \
        build-essential \
        # node build deps
        gnupg2 \
        git \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js (LTS) for frontend build
RUN curl -fsSL https://deb.nodesource.com/setup_18.x | bash - \
    && apt-get install -y nodejs \
    && npm --version || true

ENV PYTHONDONTWRITEBYTECODE 1
ENV PYTHONUNBUFFERED 1

WORKDIR /app

COPY requirements.txt ./
RUN pip install --upgrade pip && pip install -r requirements.txt && pip install psycopg2-binary

COPY . .
# Build frontend assets during image build to avoid runtime dependency on npm
RUN if [ -f package.json ]; then \
            npm ci --silent && npm run production --silent; \
        fi

RUN chmod +x /app/entrypoint.sh

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["gunicorn", "fastcp.wsgi:application", "--bind", "0.0.0.0:8000"]
